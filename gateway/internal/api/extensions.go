package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/catalog"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// ---- MCP servers ----

func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMCPServers(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type mcpInput struct {
	store.MCPServer
	TemplateID string `json:"template_id"`
}

func (s *Server) handleCreateMCP(w http.ResponseWriter, r *http.Request) {
	var in mcpInput
	if !decode(w, r, &in) {
		return
	}
	m := in.MCPServer
	if in.TemplateID != "" {
		t, ok := catalog.MCPTemplateByID(in.TemplateID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "unknown MCP template")
			return
		}
		// Template defaults, with whatever the owner filled in on top.
		base := t.Server()
		if m.Name != "" {
			base.Name = m.Name
		}
		if m.Args != nil {
			base.Args = m.Args
		}
		if m.URL != "" {
			base.URL = m.URL
		}
		for k, v := range m.Env {
			base.Env[k] = v
		}
		m = base
	}
	m.ID, m.PluginID = "", ""
	created, err := s.store.CreateMCPServer(r.Context(), m)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.configChanged("mcp.updated", created)
	s.logActivity(r.Context(), "", "", "MCP server added: "+created.Name+".")
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleUpdateMCP(w http.ResponseWriter, r *http.Request) {
	var in store.MCPServer
	if !decode(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	updated, err := s.store.UpdateMCPServer(r.Context(), in)
	if err != nil {
		s.agentErr(w, err)
		return
	}
	s.configChanged("mcp.updated", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteMCP(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMCPServer(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.configChanged("mcp.updated", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// MCPTool is one tool an MCP server offers.
type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleTestMCP connects to the server through the worker (with the server's
// secrets) and lists its tools. It doubles as a "does it work?" check.
func (s *Server) handleTestMCP(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMCPServer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if s.RunWorker == nil {
		writeJSON(w, http.StatusOK, map[string]any{"tools": []MCPTool{}, "error": "The agent worker is not available, so the server cannot be tested."})
		return
	}
	spec, _ := json.Marshal(m)
	env := append(s.secretEnv(r.Context()), "CUBICLE_MCP_SERVER="+string(spec))
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var res struct {
		Tools []MCPTool `json:"tools"`
		Error string    `json:"error,omitempty"`
	}
	if err := s.RunWorker(ctx, []string{"mcp-tools", "--json"}, env, &res); err != nil {
		res.Error = err.Error()
	}
	if res.Tools == nil {
		res.Tools = []MCPTool{}
	}
	writeJSON(w, http.StatusOK, res)
}

// ---- skills ----

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSkills(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.store.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	var in struct {
		store.Skill
		TemplateID string `json:"template_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	var sk *store.Skill
	var err error
	if in.TemplateID != "" {
		var id string
		if id, err = s.installSkillTemplate(r.Context(), in.TemplateID); err == nil {
			sk, err = s.store.GetSkill(r.Context(), id)
		}
	} else {
		in.Skill.ID, in.Skill.PluginID, in.Skill.TemplateID = "", "", ""
		sk, err = s.store.CreateSkill(r.Context(), in.Skill)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.configChanged("skills.updated", sk)
	writeJSON(w, http.StatusCreated, sk)
}

func (s *Server) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	var in store.Skill
	if !decode(w, r, &in) {
		return
	}
	in.ID = r.PathValue("id")
	sk, err := s.store.UpdateSkill(r.Context(), in)
	if err != nil {
		s.agentErr(w, err)
		return
	}
	s.configChanged("skills.updated", sk)
	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSkill(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.configChanged("skills.updated", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// ---- plugins ----

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListPlugins(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleInstallPlugin installs a catalog pack ({catalog_id}) or an imported
// bundle ({bundle}), e.g. one read from a Claude Code plugin folder by the app.
func (s *Server) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CatalogID string              `json:"catalog_id"`
		Bundle    *store.PluginBundle `json:"bundle"`
	}
	if !decode(w, r, &in) {
		return
	}
	var bundle store.PluginBundle
	source := "import"
	switch {
	case in.CatalogID != "":
		b, ok := catalog.PluginBundle(in.CatalogID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "unknown plugin")
			return
		}
		bundle, source = b, "catalog"
	case in.Bundle != nil:
		bundle = *in.Bundle
		for i := range bundle.Agents {
			// Imported agents get the standard tools unless the bundle says otherwise.
			if bundle.Agents[i].Tools == nil {
				bundle.Agents[i].Tools = []string{catalog.ToolFiles, catalog.ToolWeb, catalog.ToolMemory}
			}
			bundle.Agents[i].TemplateID = ""
			if err := s.checkToolIDs(bundle.Agents[i].Tools); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	default:
		writeErr(w, http.StatusBadRequest, "send catalog_id or bundle")
		return
	}
	p, err := s.store.InstallPlugin(r.Context(), bundle, source, in.CatalogID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.configChanged("plugins.updated", p)
	s.logActivity(r.Context(), "", "", "Plugin installed: "+p.Name+".")
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUninstallPlugin(w http.ResponseWriter, r *http.Request) {
	if err := s.store.UninstallPlugin(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.configChanged("plugins.updated", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkToolIDs(ids []string) error {
	a := store.Agent{Tools: ids}
	return s.checkAgentRefs(context.Background(), &a)
}
