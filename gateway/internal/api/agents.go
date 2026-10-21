package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/catalog"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// ---- office setup ----

// ensureOffice seeds the starting team on first run. Owners upgrading from a
// release with a hand-edited staff.json get that roster instead of the default.
func (s *Server) ensureOffice(ctx context.Context) error {
	roster, err := legacyRoster(filepath.Join(s.cfg.Home, "staff.json"))
	if err != nil {
		s.log.Warn("ignoring staff.json", "err", err)
	}
	if roster == nil {
		roster = catalog.DefaultRoster()
	}
	seeded, err := s.store.SeedAgents(ctx, roster)
	if err != nil {
		return err
	}
	if !seeded {
		return s.upgradeBuiltIns(ctx)
	}
	// Give the starting team the skills their roles come with.
	skillIDs := map[string]string{}
	for i := range roster {
		t, ok := catalog.AgentTemplateByID(roster[i].TemplateID)
		if !ok || len(t.Skills) == 0 {
			continue
		}
		for _, sid := range t.Skills {
			if _, done := skillIDs[sid]; !done {
				if id, err := s.installSkillTemplate(ctx, sid); err == nil {
					skillIDs[sid] = id
				}
			}
		}
		a := roster[i]
		for _, sid := range t.Skills {
			if id := skillIDs[sid]; id != "" {
				a.Skills = append(a.Skills, id)
			}
		}
		if _, err := s.store.UpdateAgent(ctx, a); err != nil {
			return err
		}
	}
	return s.store.SetFlag(ctx, builtInsFlag)
}

// builtInsFlag marks the one-time upgrade that gives the built-in agents the tools and skills of this release.
const builtInsFlag = "builtins-2026-10"

// defaultIDs maps the starting team's ids to the role templates they were made from.
var defaultIDs = map[string]string{
	"manager": "chief-of-staff", "researcher": "researcher", "writer": "writer", "engineer": "engineer",
	"analyst": "analyst", "reviewer": "reviewer",
}

// upgradeBuiltIns runs once on an existing office: agents made from a role template get the terminal tool
// and the skills their role now comes with. It only adds; anything the owner removed from an agent earlier
// stays removed, and it never runs again, so later choices are kept.
func (s *Server) upgradeBuiltIns(ctx context.Context) error {
	if done, err := s.store.Flag(ctx, builtInsFlag); err != nil || done {
		return err
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return err
	}
	for _, a := range agents {
		tid := a.TemplateID
		if tid == "" {
			tid = defaultIDs[a.ID]
		}
		t, ok := catalog.AgentTemplateByID(tid)
		if !ok {
			continue
		}
		changed := false
		if slices.Contains(t.Tools, catalog.ToolTerminal) && !slices.Contains(a.Tools, catalog.ToolTerminal) {
			a.Tools = append(a.Tools, catalog.ToolTerminal)
			changed = true
		}
		for _, sid := range t.Skills {
			id, err := s.installSkillTemplate(ctx, sid)
			if err != nil {
				return err
			}
			if !slices.Contains(a.Skills, id) {
				a.Skills = append(a.Skills, id)
				changed = true
			}
		}
		if changed {
			if _, err := s.store.UpdateAgent(ctx, *a); err != nil {
				return err
			}
		}
	}
	return s.store.SetFlag(ctx, builtInsFlag)
}

// legacyRoster reads the staff.json format of earlier releases.
func legacyRoster(path string) ([]store.Agent, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var list []struct {
		ID, Name, Title, Role, Skills, Persona, Avatar, Model string
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var out []store.Agent
	mainIdx := -1
	for _, e := range list {
		a := store.Agent{ID: e.ID, Name: e.Name, Title: e.Title, Avatar: e.Avatar, Model: e.Model, Role: store.RoleSpecialist,
			Description: e.Skills, Instructions: e.Persona, Enabled: true,
			Tools: []string{catalog.ToolFiles, catalog.ToolWeb, catalog.ToolMemory}}
		if e.Role == store.RoleMain && mainIdx < 0 {
			a.Role, mainIdx = store.RoleMain, len(out)
			a.Tools = append(a.Tools, catalog.ToolAssignments)
		}
		out = append(out, a)
	}
	if mainIdx < 0 || len(out) < 2 {
		return nil, errors.New("staff.json needs one manager and at least one specialist")
	}
	for i, a := range out {
		if i != mainIdx {
			out[mainIdx].Team = append(out[mainIdx].Team, a.ID)
		}
	}
	return out, nil
}

// installSkillTemplate adds a catalog skill once and returns its id.
func (s *Server) installSkillTemplate(ctx context.Context, templateID string) (string, error) {
	t, ok := catalog.SkillTemplateByID(templateID)
	if !ok {
		return "", fmt.Errorf("unknown skill template %q", templateID)
	}
	list, err := s.store.ListSkills(ctx)
	if err != nil {
		return "", err
	}
	for _, sk := range list {
		if sk.TemplateID == templateID {
			return sk.ID, nil
		}
	}
	sk, err := s.store.CreateSkill(ctx, t.Skill())
	if err != nil {
		return "", err
	}
	return sk.ID, nil
}

// configChanged tells the worker (and the app) that agents or extensions changed.
// The worker reloads its roster without restarting, so work in progress continues.
func (s *Server) configChanged(event string, data any) {
	s.revision.Add(1)
	s.hub.Publish(event, data)
	s.hub.Wake()
}

// ---- owner API: catalog ----

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":       catalog.Tools(),
		"agents":      catalog.AgentTemplates(),
		"mcp_servers": catalog.MCPTemplates(),
		"skills":      catalog.SkillTemplates(),
		"plugins":     catalog.PluginTemplates(),
	})
}

// ---- owner API: agents ----

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListAgents(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

type agentInput struct {
	TemplateID   string    `json:"template_id"`
	JoinTeam     *bool     `json:"join_team"`
	Name         *string   `json:"name"`
	Title        *string   `json:"title"`
	Avatar       *string   `json:"avatar"`
	Description  *string   `json:"description"`
	Instructions *string   `json:"instructions"`
	Model        *string   `json:"model"`
	Effort       *string   `json:"effort"`
	Tools        *[]string `json:"tools"`
	MCPServers   *[]string `json:"mcp_servers"`
	Skills       *[]string `json:"skills"`
	Team         *[]string `json:"team"`
	Enabled      *bool     `json:"enabled"`
}

// apply overlays the fields that were sent onto a.
func (in agentInput) apply(a *store.Agent) {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&a.Name, in.Name)
	set(&a.Title, in.Title)
	set(&a.Avatar, in.Avatar)
	set(&a.Description, in.Description)
	set(&a.Instructions, in.Instructions)
	set(&a.Model, in.Model)
	set(&a.Effort, in.Effort)
	for _, l := range []struct {
		dst *[]string
		v   *[]string
	}{{&a.Tools, in.Tools}, {&a.MCPServers, in.MCPServers}, {&a.Skills, in.Skills}, {&a.Team, in.Team}} {
		if l.v != nil {
			*l.dst = *l.v
		}
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
}

// handleCreateAgent adds a custom agent, or one from a role template in one click.
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	a := store.Agent{Tools: []string{catalog.ToolFiles, catalog.ToolWeb, catalog.ToolMemory}}
	var templateSkills []string
	if in.TemplateID != "" {
		t, ok := catalog.AgentTemplateByID(in.TemplateID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "unknown agent template")
			return
		}
		a, templateSkills = t.Agent(), t.Skills
		a.ID = ""
	}
	in.apply(&a)
	if in.Skills == nil {
		for _, sid := range templateSkills {
			id, err := s.installSkillTemplate(ctx, sid)
			if err != nil {
				s.storeErr(w, err)
				return
			}
			a.Skills = append(a.Skills, id)
		}
	}
	if err := s.checkAgentRefs(ctx, &a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := s.store.CreateAgent(ctx, a, in.JoinTeam == nil || *in.JoinTeam)
	if err != nil {
		s.agentErr(w, err)
		return
	}
	s.configChanged("agents.updated", created)
	s.logActivity(ctx, created.ID, "", fmt.Sprintf("%s joined your staff as %s.", created.Name, orDefault(created.Title, "an agent")))
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	a, err := s.store.GetAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	in.apply(a)
	if err := s.checkAgentRefs(ctx, a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.store.UpdateAgent(ctx, *a)
	if err != nil {
		s.agentErr(w, err)
		return
	}
	s.configChanged("agents.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := s.store.GetAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if err := s.store.DeleteAgent(ctx, a.ID); err != nil {
		s.agentErr(w, err)
		return
	}
	s.configChanged("agents.updated", map[string]string{"id": a.ID})
	s.logActivity(ctx, "", "", a.Name+" left your staff.")
	w.WriteHeader(http.StatusNoContent)
}

// handleSetMain makes an agent the main agent: it receives assignments and
// automations and plans them with its team.
func (s *Server) handleSetMain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.SetMainAgent(ctx, r.PathValue("id")); err != nil {
		s.agentErr(w, err)
		return
	}
	a, err := s.store.GetAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.configChanged("agents.updated", a)
	s.logActivity(ctx, a.ID, "", a.Name+" is now your main agent.")
	writeJSON(w, http.StatusOK, a)
}

// checkAgentRefs rejects references to tools, servers, skills or agents that do not exist.
func (s *Server) checkAgentRefs(ctx context.Context, a *store.Agent) error {
	known := map[string]bool{}
	for _, t := range catalog.Tools() {
		known[t.ID] = true
	}
	for _, t := range a.Tools {
		if !known[t] {
			return fmt.Errorf("unknown tool %q", t)
		}
	}
	servers, err := s.store.ListMCPServers(ctx)
	if err != nil {
		return err
	}
	if id := firstMissing(a.MCPServers, servers, func(m *store.MCPServer) string { return m.ID }); id != "" {
		return fmt.Errorf("unknown MCP server %q", id)
	}
	skills, err := s.store.ListSkills(ctx)
	if err != nil {
		return err
	}
	if id := firstMissing(a.Skills, skills, func(sk *store.Skill) string { return sk.ID }); id != "" {
		return fmt.Errorf("unknown skill %q", id)
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return err
	}
	if id := firstMissing(a.Team, agents, func(x *store.Agent) string { return x.ID }); id != "" {
		return fmt.Errorf("unknown team member %q", id)
	}
	return nil
}

func firstMissing[T any](ids []string, items []T, key func(T) string) string {
	for _, id := range ids {
		if !slices.ContainsFunc(items, func(it T) bool { return key(it) == id }) {
			return id
		}
	}
	return ""
}

func (s *Server) agentErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.storeErr(w, err)
		return
	}
	if errors.Is(err, store.ErrLastMain) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// ---- worker API: roster ----

// WorkerAgent is an agent's complete working configuration for the worker:
// its MCP servers and skills are resolved, and disabled items are left out.
type WorkerAgent struct {
	*store.Agent
	MCP       []*store.MCPServer `json:"mcp"`
	SkillDocs []*store.Skill     `json:"skill_docs"`
}

// handleWorkerAgents returns the roster. With ?after=<revision> it long-polls
// until the configuration changes, so the worker reloads within moments.
func (s *Server) handleWorkerAgents(w http.ResponseWriter, r *http.Request) {
	if after := r.URL.Query().Get("after"); after != "" {
		rev, _ := strconv.ParseInt(after, 10, 64)
		ctx, cancel := context.WithTimeout(r.Context(), s.LongPollWait)
		defer cancel()
		for s.revision.Load() == rev {
			select {
			case <-s.hub.WaitC():
			case <-ctx.Done():
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	}
	rev := s.revision.Load()
	roster, err := s.workerRoster(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "agents": roster})
}

func (s *Server) workerRoster(ctx context.Context) ([]WorkerAgent, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	servers, err := s.store.ListMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	skills, err := s.store.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, a := range agents {
		active[a.ID] = a.Enabled
	}
	out := []WorkerAgent{}
	for _, a := range agents {
		if !a.Enabled {
			continue
		}
		wa := WorkerAgent{Agent: a, MCP: []*store.MCPServer{}, SkillDocs: []*store.Skill{}}
		a.Team = slices.DeleteFunc(slices.Clone(a.Team), func(id string) bool { return !active[id] })
		for _, m := range servers {
			if m.Enabled && slices.Contains(a.MCPServers, m.ID) {
				wa.MCP = append(wa.MCP, m)
			}
		}
		for _, sk := range skills {
			if sk.Enabled && slices.Contains(a.Skills, sk.ID) {
				wa.SkillDocs = append(wa.SkillDocs, sk)
			}
		}
		out = append(out, wa)
	}
	return out, nil
}

// handleWorkerStart is called once when a worker starts. Work that was in
// progress when the previous worker stopped is put back in the queue.
func (s *Server) handleWorkerStart(w http.ResponseWriter, r *http.Request) {
	requeued, err := s.store.RequeueInterrupted(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if len(requeued) > 0 {
		s.publishTasks(requeued...)
		s.logActivity(r.Context(), "", "", fmt.Sprintf("Resumed %d interrupted task(s).", len(requeued)))
	}
	s.logActivity(r.Context(), "", "", "Your staff clocked in.")
	writeJSON(w, http.StatusOK, map[string]int{"requeued": len(requeued)})
}

func (s *Server) handleAgentStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status        string `json:"status"`
		CurrentTaskID string `json:"current_task_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	switch in.Status {
	case "idle", "working", "waiting", "offline":
	default:
		writeErr(w, http.StatusBadRequest, "status must be idle, working, waiting or offline")
		return
	}
	a, err := s.store.SetAgentStatus(r.Context(), r.PathValue("id"), in.Status, in.CurrentTaskID)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("agent.status", a)
	writeJSON(w, http.StatusOK, a)
}
