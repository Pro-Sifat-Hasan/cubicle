package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/catalog"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// personaMarker separates the main agent's own instructions from the personality
// onboarding appends, so choosing a new passion replaces the old one.
const personaMarker = "\n\nYour personality: "

const (
	maxCustomPassion = 80
	maxStackItems    = 12
	maxStackItem     = 40
)

// ---- packs and profile ----

func (s *Server) handleGetOnboarding(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProfile(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":     p,
		"passions":    catalog.Passions(),
		"professions": catalog.Professions(),
	})
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProfile(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	missions := []string{}
	if prof, ok := catalog.ProfessionByID(p.Profession); ok {
		missions = prof.StarterMissions
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": p, "starter_missions": missions})
}

// ---- custom persona ----

// handleCreatePersona asks the main agent to invent a persona from the passion
// the owner typed. The model runs in the worker, so this only queues the task.
func (s *Server) handleCreatePersona(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Passion string `json:"passion"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Passion = strings.TrimSpace(in.Passion)
	if err := checkLen("passion", in.Passion, maxCustomPassion, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	main, err := s.store.MainAgent(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	t, err := s.store.CreateTask(r.Context(), store.NewTask{
		Kind: store.KindPersona, Title: "Create a persona", Description: in.Passion, Assignee: main.ID,
	})
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.publishTasks(t)
	writeJSON(w, http.StatusAccepted, map[string]string{"task_id": t.ID})
}

// handleGetPersona reports a persona task. The result is model output, so it is
// parsed and clamped here and the app never sees the raw text.
func (s *Server) handleGetPersona(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if t.Kind != store.KindPersona {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	out := map[string]any{"status": t.Status}
	switch t.Status {
	case store.StatusDone:
		p, err := parsePersona(t.Result, t.Description)
		if err != nil {
			out["status"], out["error"] = store.StatusFailed, err.Error()
		} else {
			out["persona"] = p
		}
	case store.StatusFailed, store.StatusCancelled:
		out["error"] = firstNonBlank(t.Error, "the persona could not be created")
	}
	writeJSON(w, http.StatusOK, out)
}

// parsePersona turns the worker's JSON into a clamped custom persona.
func parsePersona(raw, passion string) (store.Persona, error) {
	var p store.Persona
	if err := json.Unmarshal([]byte(extractJSONObject(raw)), &p); err != nil {
		return p, errors.New("the model did not return a usable persona")
	}
	p.ID, p.Title = store.CustomPassion, firstNonBlank(p.Title, passion)
	if err := store.NormalizePersona(&p); err != nil {
		return p, err
	}
	return p, nil
}

func extractJSONObject(s string) string {
	start, end := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if start == -1 || end < start {
		return s
	}
	return s[start : end+1]
}

// fallbackPersona is used when no model can write one (no provider yet, or an
// invalid answer): a neutral voice with an accent derived from the passion text.
func fallbackPersona(passion string) store.Persona {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(passion)))
	hue := h.Sum32() % 360
	p := store.Persona{
		ID: store.CustomPassion, Title: passion, Emoji: "✨",
		SystemPromptStyle: fmt.Sprintf("You are a friendly, capable chief of staff who loves %s. Mention it lightly now and then, "+
			"never at the cost of a clear answer.", passion),
		GreetingTemplates: []string{"Good morning, {name}. {agent} here. Ready when you are."},
		DailyFeedQueries:  []string{"latest news about " + passion},
		AccentColor:       hslToHex(float64(hue), 0.65, 0.6),
		AvatarSet:         []string{"✨", "🧭", "🌟"},
	}
	_ = store.NormalizePersona(&p)
	return p
}

func hslToHex(h, s, l float64) string {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return fmt.Sprintf("#%02X%02X%02X", int((r+m)*255+0.5), int((g+m)*255+0.5), int((b+m)*255+0.5))
}

func firstNonBlank(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---- onboarding ----

type onboardingInput struct {
	AgentName     string         `json:"agent_name"`
	OwnerName     string         `json:"owner_name"`
	Profession    string         `json:"profession"`
	Passion       string         `json:"passion"`        // a passion pack id, or "custom"
	CustomPassion string         `json:"custom_passion"` // what the owner typed, for "custom"
	Persona       *store.Persona `json:"persona"`        // the generated persona, for "custom"; optional
	Stack         []string       `json:"stack"`
}

// handleOnboard applies the owner's choices: it names and voices the main agent,
// hires the profession's team, sets up its routines and remembers the basics.
func (s *Server) handleOnboard(w http.ResponseWriter, r *http.Request) {
	var in onboardingInput
	if !decode(w, r, &in) {
		return
	}
	in.AgentName, in.OwnerName = strings.TrimSpace(in.AgentName), strings.TrimSpace(in.OwnerName)
	in.CustomPassion = strings.TrimSpace(in.CustomPassion)
	for _, c := range []error{
		checkLen("agent_name", in.AgentName, 60, true),
		checkLen("owner_name", in.OwnerName, 60, false),
		checkLen("custom_passion", in.CustomPassion, maxCustomPassion, false),
	} {
		if c != nil {
			writeErr(w, http.StatusBadRequest, c.Error())
			return
		}
	}
	prof, ok := catalog.ProfessionByID(in.Profession)
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown profession")
		return
	}
	persona, err := resolvePersona(in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	stack := cleanStack(in.Stack)

	ctx := r.Context()
	// Two agents with one name would be impossible to tell apart in chats and plans.
	if others, err := s.store.ListAgents(ctx); err == nil {
		for _, a := range others {
			if !a.IsMain() && strings.EqualFold(a.Name, in.AgentName) {
				writeErr(w, http.StatusBadRequest, "another agent is already called "+a.Name+"; pick a different name")
				return
			}
		}
	}
	res, err := s.applyOnboarding(ctx, in, prof, persona, stack)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.configChanged("profile.updated", res.Profile)
	if in.OwnerName != "" && s.OnConfigChanged != nil {
		s.OnConfigChanged() // the owner's name is part of the worker's environment
	}
	s.logActivity(ctx, "", "", fmt.Sprintf("%s joined as your chief of staff with a team of %d.", in.AgentName, len(res.Hired)+1))
	writeJSON(w, http.StatusOK, res)
}

func resolvePersona(in onboardingInput) (store.Persona, error) {
	if in.Passion == store.CustomPassion {
		if in.CustomPassion == "" {
			return store.Persona{}, errors.New("custom_passion is required for a custom passion")
		}
		if in.Persona != nil {
			p := *in.Persona
			p.ID, p.Title = store.CustomPassion, firstNonBlank(p.Title, in.CustomPassion)
			if err := store.NormalizePersona(&p); err == nil {
				return p, nil
			}
		}
		return fallbackPersona(in.CustomPassion), nil
	}
	p, ok := catalog.PassionByID(in.Passion)
	if !ok {
		return store.Persona{}, errors.New("unknown passion")
	}
	return p, nil
}

func cleanStack(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || len(v) > maxStackItem || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
		if len(out) == maxStackItems {
			break
		}
	}
	return out
}

type onboardingResult struct {
	Profile         store.Profile    `json:"profile"`
	Hired           []*store.Agent   `json:"hired"`
	Routines        []*store.Routine `json:"routines"`
	StarterMissions []string         `json:"starter_missions"`
}

func (s *Server) applyOnboarding(ctx context.Context, in onboardingInput, prof catalog.Profession,
	persona store.Persona, stack []string) (*onboardingResult, error) {
	main, err := s.store.MainAgent(ctx)
	if err != nil {
		return nil, err
	}

	// Voice and name for the main agent. Only the main agent takes the persona;
	// specialists stay professional.
	base := main.Instructions
	if i := strings.Index(base, personaMarker); i >= 0 {
		base = base[:i]
	}
	updated := *main
	updated.Name = in.AgentName
	updated.Instructions = strings.TrimSpace(base) + personaMarker + persona.SystemPromptStyle
	if len(persona.AvatarSet) > 0 {
		updated.Avatar = persona.AvatarSet[0]
	}
	if _, err := s.store.UpdateAgent(ctx, updated); err != nil {
		return nil, err
	}

	res := &onboardingResult{Hired: []*store.Agent{}, Routines: []*store.Routine{}, StarterMissions: prof.StarterMissions}
	hired := map[string]string{} // pack teammate id -> agent id
	for _, t := range prof.Team {
		if existing, err := s.store.GetAgent(ctx, t.ID); err == nil {
			hired[t.ID] = existing.ID
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		var skillIDs []string
		for _, sk := range t.Skills {
			id, err := s.installSkillTemplate(ctx, sk)
			if err != nil {
				return nil, err
			}
			skillIDs = append(skillIDs, id)
		}
		a, err := s.store.CreateAgent(ctx, store.Agent{
			ID: t.ID, Name: t.Name, Title: t.Title, Avatar: t.Avatar, Description: t.Description,
			Instructions: t.Instructions, Tools: t.Tools, Skills: skillIDs,
		}, true)
		if err != nil {
			return nil, err
		}
		hired[t.ID] = a.ID
		res.Hired = append(res.Hired, a)
	}

	existing, err := s.store.ListRoutines(ctx)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, rt := range existing {
		have[rt.Title] = true
	}
	for _, pr := range prof.Routines {
		if have[pr.Title] {
			continue
		}
		rt, err := s.store.CreateRoutine(ctx, store.RoutineInput{
			Title: pr.Title, Description: pr.Description, Schedule: pr.Schedule, AgentID: hired[pr.Agent],
		})
		if err != nil {
			return nil, err
		}
		if !pr.Enabled {
			if rt, err = s.store.SetRoutineEnabled(ctx, rt.ID, false); err != nil {
				return nil, err
			}
		}
		res.Routines = append(res.Routines, rt)
	}

	// The basics start the owner's profile, the one record every agent reads and keeps up to date.
	profile := fmt.Sprintf("Works as %s.", prof.Title)
	if len(stack) > 0 {
		profile += " Works with: " + strings.Join(stack, ", ") + "."
	}
	profile += " Into " + strings.ToLower(firstNonBlank(in.CustomPassion, persona.Title)) + "."
	if _, err := s.store.SetProfile(ctx, profile, main.ID); err != nil {
		return nil, err
	}

	if in.OwnerName != "" {
		st, err := s.store.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		st.OwnerName = in.OwnerName
		if err := s.store.PutSettings(ctx, st); err != nil {
			return nil, err
		}
	}

	res.Profile = store.Profile{
		Onboarded: true, AgentName: in.AgentName, Profession: prof.ID, Passion: persona,
		CustomPassion: in.CustomPassion, Stack: stack,
	}
	if in.Passion != store.CustomPassion {
		res.Profile.CustomPassion = ""
	}
	if err := s.store.PutProfile(ctx, res.Profile); err != nil {
		return nil, err
	}
	return res, nil
}
