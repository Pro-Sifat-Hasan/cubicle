package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/providers"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

var efforts = []string{"low", "medium", "high", "xhigh", "max"}

// ---- settings ----

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in store.Settings
	if !decode(w, r, &in) {
		return
	}
	in.Model = strings.TrimSpace(in.Model)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.OwnerName = strings.TrimSpace(in.OwnerName)
	if err := validateSettings(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.PutSettings(r.Context(), in); err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("settings.updated", in)
	s.logActivity(r.Context(), "", "", "Office settings updated: "+s.describeProvider(r.Context(), in)+".")
	s.checkBudget(r.Context())
	if s.OnConfigChanged != nil {
		s.OnConfigChanged()
	}
	writeJSON(w, http.StatusOK, in)
}

func validateSettings(in *store.Settings) error {
	if in.Provider == "" {
		in.Provider = providers.Auto
	}
	if in.Provider != providers.Auto {
		p, ok := providers.Get(in.Provider)
		if !ok {
			return fmt.Errorf("unknown provider %q", in.Provider)
		}
		if !p.BaseURLEditable {
			in.BaseURL = ""
		}
	}
	if in.Effort == "" {
		in.Effort = "medium"
	}
	if !slices.Contains(efforts, in.Effort) {
		return fmt.Errorf("effort must be one of %s", strings.Join(efforts, ", "))
	}
	if err := checkLen("model", in.Model, 200, false); err != nil {
		return err
	}
	if strings.ContainsAny(in.Model, " \t\r\n") {
		return fmt.Errorf("model must not contain whitespace")
	}
	if err := checkLen("owner_name", in.OwnerName, 80, false); err != nil {
		return err
	}
	if in.OwnerName == "" {
		in.OwnerName = "the owner"
	}
	if in.BaseURL != "" {
		u, err := url.Parse(in.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("base_url must be an http(s) URL")
		}
	}
	if in.DailyTokenBudget != nil && *in.DailyTokenBudget < 0 {
		return fmt.Errorf("daily_token_budget must be 0 (unlimited) or positive")
	}
	return nil
}

// ---- providers & models ----

type providerView struct {
	providers.Provider
	Configured bool `json:"configured"` // key stored (or not needed)
	Active     bool `json:"active"`     // what the staff are using right now
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.ListSecretNames(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	active := resolveProvider(st, names)
	out := []providerView{}
	for _, p := range providers.All() {
		out = append(out, providerView{
			Provider:   p,
			Configured: p.SecretName == "" || slices.Contains(names, p.SecretName),
			Active:     p.ID == active.ID,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleListModels asks the provider (through the worker and the provider's
// official SDK) which models the stored key can use. This doubles as a
// connection test. Falls back to the catalog's suggestions.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	p, ok := providers.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown provider")
		return
	}
	resp := map[string]any{"models": p.SuggestedModels, "live": false}
	if s.RunWorker == nil || p.Kind == providers.KindDemo {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	st, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	baseURL := p.BaseURL
	if p.BaseURLEditable && st.Provider == p.ID && st.BaseURL != "" {
		baseURL = st.BaseURL
	}
	if q := strings.TrimSpace(r.URL.Query().Get("base_url")); q != "" && p.BaseURLEditable {
		baseURL = q
	}
	env := append(s.secretEnv(r.Context()), providerEnv(p, baseURL)...)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	var res struct {
		Models []string `json:"models"`
		Error  string   `json:"error"`
	}
	err = s.RunWorker(ctx, []string{"models", "--json"}, env, &res)
	switch {
	case err != nil:
		resp["error"] = err.Error()
	case res.Error != "":
		resp["error"] = res.Error
	case len(res.Models) > 0:
		resp["models"], resp["live"] = res.Models, true
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- memories ----

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListMemories(r.Context(), limit)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleOwnerAddMemory(w http.ResponseWriter, r *http.Request) {
	s.addMemory(w, r, false)
}

func (s *Server) handleWorkerAddMemory(w http.ResponseWriter, r *http.Request) {
	s.addMemory(w, r, true)
}

func (s *Server) addMemory(w http.ResponseWriter, r *http.Request, fromWorker bool) {
	var in struct {
		Content    string `json:"content"`
		EmployeeID string `json:"employee_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if err := checkLen("content", in.Content, 1000, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !fromWorker {
		in.EmployeeID = "" // the owner wrote it
	}
	m, err := s.store.AddMemory(r.Context(), in.Content, in.EmployeeID)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("memory.added", m)
	if fromWorker {
		s.logActivity(r.Context(), in.EmployeeID, "", "Remembered: "+truncate(m.Content, 200))
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleOwnerSetProfile(w http.ResponseWriter, r *http.Request) {
	s.setProfile(w, r, false)
}

func (s *Server) handleWorkerSetProfile(w http.ResponseWriter, r *http.Request) {
	s.setProfile(w, r, true)
}

// setProfile replaces the one record that describes the owner in full. Empty content clears it.
func (s *Server) setProfile(w http.ResponseWriter, r *http.Request, fromWorker bool) {
	var in struct {
		Content    string `json:"content"`
		EmployeeID string `json:"employee_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if err := checkLen("content", in.Content, 3000, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !fromWorker {
		in.EmployeeID = ""
	}
	m, err := s.store.SetProfile(r.Context(), in.Content, in.EmployeeID)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("memory.added", m)
	if fromWorker && m != nil {
		s.logActivity(r.Context(), in.EmployeeID, "", "Updated the profile of the owner")
	}
	if m == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMemory(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("memory.deleted", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// ---- worker environment ----

// resolveProvider turns the owner's choice into a concrete provider.
func resolveProvider(st store.Settings, secretNames []string) providers.Provider {
	if p, ok := providers.Get(st.Provider); ok {
		return p
	}
	if slices.Contains(secretNames, "ANTHROPIC_API_KEY") {
		p, _ := providers.Get("anthropic")
		return p
	}
	p, _ := providers.Get("demo")
	return p
}

func providerEnv(p providers.Provider, baseURL string) []string {
	return []string{
		"CUBICLE_LLM_PROVIDER=" + p.ID,
		"CUBICLE_LLM_KIND=" + p.Kind,
		"CUBICLE_LLM_BASE_URL=" + baseURL,
		"CUBICLE_LLM_KEY_ENV=" + p.SecretName,
	}
}

// WorkerEnv is the full environment for the agent worker: decrypted secrets
// plus the resolved provider, model and office settings.
func (s *Server) WorkerEnv() []string {
	ctx, cancel := contextWithTimeout()
	defer cancel()
	env := s.secretEnv(ctx)
	names, _ := s.store.ListSecretNames(ctx)
	st, err := s.store.GetSettings(ctx)
	if err != nil {
		st = store.DefaultSettings()
	}
	p := resolveProvider(st, names)
	baseURL := p.BaseURL
	if p.BaseURLEditable && st.BaseURL != "" {
		baseURL = st.BaseURL
	}
	model := st.Model
	if model == "" || st.Provider == providers.Auto {
		model = p.DefaultModel
	}
	env = append(env, providerEnv(p, baseURL)...)
	return append(env,
		"CUBICLE_MODEL="+model,
		"CUBICLE_EFFORT="+st.Effort,
		"CUBICLE_OWNER_NAME="+st.OwnerName,
		"CUBICLE_WEB_ACCESS="+strconv.FormatBool(st.WebAccess),
		"CUBICLE_WORKSPACE="+s.workspaceDir(),
	)
}

func (s *Server) workspaceDir() string {
	if s.WorkspaceDir != "" {
		return s.WorkspaceDir
	}
	return filepath.Join(s.cfg.Home, "workspace")
}

func (s *Server) describeProvider(ctx context.Context, st store.Settings) string {
	names, _ := s.store.ListSecretNames(ctx)
	p := resolveProvider(st, names)
	model := st.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return p.Name
	}
	return p.Name + " · " + model
}

// dailyBudget is the owner's setting when present, otherwise the gateway config.
func (s *Server) dailyBudget(ctx context.Context) int64 {
	if st, err := s.store.GetSettings(ctx); err == nil && st.DailyTokenBudget != nil {
		return *st.DailyTokenBudget
	}
	return s.cfg.DailyTokenBudget
}
