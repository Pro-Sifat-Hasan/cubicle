package test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func envValue(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

func TestSettingsDefaultsToDemoWithoutKeys(t *testing.T) {
	h := newHarness(t)
	var st store.Settings
	h.do("GET", "/api/v1/settings", adminTok, nil, 200, &st)
	if st.Provider != "auto" || st.Effort != "medium" {
		t.Fatalf("unexpected defaults: %+v", st)
	}
	env := h.gw.WorkerEnv()
	if envValue(env, "CUBICLE_LLM_KIND") != "demo" {
		t.Fatalf("auto without keys should resolve to demo, env=%v", env)
	}
}

func TestSettingsSelectProviderAndModel(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/api/v1/settings", adminTok, map[string]any{
		"provider": "openai", "model": "gpt-5-mini", "effort": "high", "owner_name": "Sam", "web_access": false,
	}, 200, nil)

	env := h.gw.WorkerEnv()
	want := map[string]string{
		"CUBICLE_LLM_PROVIDER": "openai",
		"CUBICLE_LLM_KIND":     "openai_compatible",
		"CUBICLE_LLM_BASE_URL": "https://api.openai.com/v1",
		"CUBICLE_LLM_KEY_ENV":  "OPENAI_API_KEY",
		"CUBICLE_MODEL":        "gpt-5-mini",
		"CUBICLE_EFFORT":       "high",
		"CUBICLE_OWNER_NAME":   "Sam",
		"CUBICLE_WEB_ACCESS":   "false",
	}
	for k, v := range want {
		if got := envValue(env, k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestSettingsCustomBaseURLOnlyWhereEditable(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/api/v1/settings", adminTok, map[string]any{"provider": "openai", "base_url": "http://evil.example"}, 200, nil)
	if got := envValue(h.gw.WorkerEnv(), "CUBICLE_LLM_BASE_URL"); got != "https://api.openai.com/v1" {
		t.Fatalf("non-editable base URL was overridden: %q", got)
	}
	h.do("PUT", "/api/v1/settings", adminTok, map[string]any{"provider": "ollama", "base_url": "http://192.168.1.20:11434/v1"}, 200, nil)
	if got := envValue(h.gw.WorkerEnv(), "CUBICLE_LLM_BASE_URL"); got != "http://192.168.1.20:11434/v1" {
		t.Fatalf("editable base URL not applied: %q", got)
	}
}

func TestSettingsValidation(t *testing.T) {
	h := newHarness(t)
	bad := []map[string]any{
		{"provider": "skynet"},
		{"provider": "anthropic", "effort": "ludicrous"},
		{"provider": "anthropic", "model": "two words"},
		{"provider": "custom", "base_url": "file:///etc/passwd"},
		{"provider": "anthropic", "daily_token_budget": -5},
		{"provider": "anthropic", "unknown_field": true},
	}
	for _, body := range bad {
		h.do("PUT", "/api/v1/settings", adminTok, body, 400, nil)
	}
}

func TestSettingsBudgetOverridesConfig(t *testing.T) {
	h := newHarness(t) // config budget is 1000
	h.do("PUT", "/api/v1/settings", adminTok, map[string]any{"provider": "demo", "daily_token_budget": 0}, 200, nil)
	h.do("POST", "/internal/v1/usage", workerTok, map[string]any{"input_tokens": 5000, "output_tokens": 0}, 204, nil)
	// Unlimited via settings: claims are still allowed (204 = no work, not 503).
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 204, nil)
}

func TestProvidersReportConfiguredAndActive(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/api/v1/secrets/ANTHROPIC_API_KEY", adminTok, map[string]string{"value": "sk-test"}, 204, nil)
	var list []struct {
		ID         string `json:"id"`
		Configured bool   `json:"configured"`
		Active     bool   `json:"active"`
	}
	h.do("GET", "/api/v1/providers", adminTok, nil, 200, &list)
	byID := map[string]bool{}
	var active []string
	for _, p := range list {
		byID[p.ID] = p.Configured
		if p.Active {
			active = append(active, p.ID)
		}
	}
	if !byID["anthropic"] || byID["openai"] || !byID["ollama"] || !byID["claude_code"] {
		t.Fatalf("configured flags wrong: %v", byID)
	}
	if !slices.Equal(active, []string{"anthropic"}) {
		t.Fatalf("active = %v, want [anthropic]", active)
	}
}

func TestListModelsFallsBackToSuggestions(t *testing.T) {
	h := newHarness(t)
	var res struct {
		Models []string `json:"models"`
		Live   bool     `json:"live"`
	}
	h.do("GET", "/api/v1/providers/anthropic/models", adminTok, nil, 200, &res)
	if res.Live || !slices.Contains(res.Models, "claude-opus-5-5") {
		t.Fatalf("unexpected models response: %+v", res)
	}
	h.do("GET", "/api/v1/providers/nope/models", adminTok, nil, 404, nil)
}

func TestMemories(t *testing.T) {
	h := newHarness(t)
	var m store.Memory
	h.do("POST", "/api/v1/memories", adminTok, map[string]string{"content": "Prefers British spelling"}, 201, &m)
	h.do("POST", "/internal/v1/memories", workerTok, map[string]string{"content": "Main client is Acme", "employee_id": "writer"}, 201, nil)
	h.do("POST", "/internal/v1/memories", workerTok, map[string]string{"content": "Main client is Acme", "employee_id": "writer"}, 201, nil) // duplicate ignored
	h.do("POST", "/api/v1/memories", adminTok, map[string]string{"content": "   "}, 400, nil)

	var list []store.Memory
	h.do("GET", "/internal/v1/memories", workerTok, nil, 200, &list)
	if len(list) != 2 {
		t.Fatalf("want 2 memories, got %d", len(list))
	}
	h.do("DELETE", "/api/v1/memories/"+m.ID, adminTok, nil, 204, nil)
	h.do("DELETE", "/api/v1/memories/"+m.ID, adminTok, nil, 404, nil)
	h.do("DELETE", "/internal/v1/memories/"+m.ID, workerTok, nil, 404, nil) // workers have no delete route
}
