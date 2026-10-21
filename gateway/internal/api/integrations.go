package api

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/catalog"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

const maxFieldChars = 500

// cleanFieldValue checks a plain value the owner typed for an integration. Values become command arguments,
// so a leading dash (an option) and control characters are refused.
func cleanFieldValue(f catalog.Field, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		if f.Optional {
			return "", nil
		}
		return "", fmt.Errorf("%s is required", f.Label)
	}
	if len(v) > maxFieldChars {
		return "", fmt.Errorf("%s is too long", f.Label)
	}
	if strings.HasPrefix(v, "-") || strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%s has characters that are not allowed", f.Label)
	}
	return v, nil
}

// handleConnectIntegration connects an app in one step: it stores the owner's keys encrypted, adds (or updates)
// the MCP server from the app's preset, and gives it to the chosen agents.
func (s *Server) handleConnectIntegration(w http.ResponseWriter, r *http.Request) {
	t, ok := catalog.MCPTemplateByID(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown app")
		return
	}
	var in struct {
		Values   map[string]string `json:"values"`
		AgentIDs []string          `json:"agent_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	stored, err := s.store.ListSecretNames(ctx)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	plain := map[string]string{}
	newSecrets := map[string]string{}
	for _, f := range t.Fields {
		raw := in.Values[f.Key]
		if f.Kind == catalog.FieldSecret {
			raw = strings.TrimSpace(raw)
			switch {
			case raw != "":
				if len(raw) > 8192 {
					writeErr(w, http.StatusBadRequest, f.Label+" is too long")
					return
				}
				newSecrets[f.Secret] = raw
			case !slices.Contains(stored, f.Secret) && !f.Optional:
				writeErr(w, http.StatusBadRequest, f.Label+" is required")
				return
			}
			continue
		}
		v, err := cleanFieldValue(f, raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		plain[f.Key] = v
	}

	var agents []*store.Agent
	for _, id := range in.AgentIDs {
		a, err := s.store.GetAgent(ctx, id)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "unknown agent "+id)
			return
		}
		agents = append(agents, a)
	}

	for name, value := range newSecrets {
		sealed, err := s.secrets.Seal(name, []byte(value))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "encryption failed")
			return
		}
		if err := s.store.PutSecret(ctx, name, sealed); err != nil {
			s.storeErr(w, err)
			return
		}
	}

	server := t.Fill(plain)
	existing, err := s.store.ListMCPServers(ctx)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	var saved *store.MCPServer
	for _, e := range existing {
		if e.TemplateID == t.ID && e.PluginID == "" {
			server.ID, server.Enabled = e.ID, true
			saved, err = s.store.UpdateMCPServer(ctx, server)
			break
		}
	}
	if saved == nil && err == nil {
		server.ID = ""
		saved, err = s.store.CreateMCPServer(ctx, server)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	attached := []string{}
	for _, a := range agents {
		if slices.Contains(a.MCPServers, saved.ID) {
			continue
		}
		a.MCPServers = append(a.MCPServers, saved.ID)
		if _, err := s.store.UpdateAgent(ctx, *a); err != nil {
			s.agentErr(w, err)
			return
		}
		attached = append(attached, a.ID)
	}

	s.configChanged("mcp.updated", saved)
	s.logActivity(ctx, "", "", t.Name+" connected.")
	if t.Messages {
		s.ensureMessageCheck(ctx, agents)
	}
	if len(newSecrets) > 0 && s.OnConfigChanged != nil {
		s.OnConfigChanged() // the worker restarts to pick up the new key
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": saved, "attached": attached})
}
