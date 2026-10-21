package test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/catalog"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

type connectResult struct {
	Server   store.MCPServer `json:"server"`
	Attached []string        `json:"attached"`
}

func TestConnectingAnAppStoresTheKeyAddsTheServerAndGivesItToAgents(t *testing.T) {
	h := newHarness(t)
	var res connectResult
	h.do("POST", "/api/v1/integrations/github/connect", adminTok, map[string]any{
		"values": map[string]string{"token": "github_pat_secret"}, "agent_ids": []string{"engineer", "manager"}}, 200, &res)
	if res.Server.TemplateID != "github" || !res.Server.Enabled || res.Server.AuthSecret != "GITHUB_PERSONAL_ACCESS_TOKEN" ||
		!slices.Equal(res.Attached, []string{"engineer", "manager"}) {
		t.Fatalf("unexpected result: %+v", res)
	}
	var names []string
	h.do("GET", "/api/v1/secrets", adminTok, nil, 200, &names)
	if !slices.Contains(names, "GITHUB_PERSONAL_ACCESS_TOKEN") {
		t.Fatalf("the token was not stored: %v", names)
	}
	if !slices.Contains(h.agents()["engineer"].MCPServers, res.Server.ID) {
		t.Fatal("the developer should have the app")
	}

	// Connecting again (no new token needed) updates the same server, and does not attach twice.
	var again connectResult
	h.do("POST", "/api/v1/integrations/github/connect", adminTok, map[string]any{"values": map[string]string{}, "agent_ids": []string{"engineer"}}, 200, &again)
	if again.Server.ID != res.Server.ID || len(again.Attached) != 0 {
		t.Fatalf("reconnecting should reuse the server: %+v", again)
	}
	var servers []store.MCPServer
	h.do("GET", "/api/v1/mcp-servers", adminTok, nil, 200, &servers)
	if len(servers) != 1 {
		t.Fatalf("expected one server, got %d", len(servers))
	}
}

func TestConnectFillsPlaceholdersAndRefusesBadInput(t *testing.T) {
	h := newHarness(t)
	var res connectResult
	h.do("POST", "/api/v1/integrations/slack/connect", adminTok, map[string]any{
		"values": map[string]string{"token": "xoxb-1", "team": "T0123"}}, 200, &res)
	if res.Server.Env["SLACK_TEAM_ID"] != "T0123" || !slices.Contains(res.Server.SecretEnv, "SLACK_BOT_TOKEN") {
		t.Fatalf("slack: %+v", res.Server)
	}
	h.do("POST", "/api/v1/integrations/git/connect", adminTok, map[string]any{"values": map[string]string{"repo": `C:\Projects\my app`}}, 200, &res)
	if !slices.Equal(res.Server.Args, []string{"mcp-server-git", "--repository", `C:\Projects\my app`}) {
		t.Fatalf("a folder with a space must stay one argument: %v", res.Server.Args)
	}

	bad := map[string]map[string]any{
		"/api/v1/integrations/slack/connect":      {"values": map[string]string{"token": "x"}},                                // team missing
		"/api/v1/integrations/github/connect":     {"values": map[string]string{}},                                            // no token yet
		"/api/v1/integrations/linear/connect":     {"values": map[string]string{"token": "k"}, "agent_ids": []string{"nope"}}, // unknown agent
		"/api/v1/integrations/filesystem/connect": {"values": map[string]string{"folder": "--allow-everything"}},              // an option, not a folder
		"/api/v1/integrations/sqlite/connect":     {"values": map[string]string{"db": "a\nb"}},
		"/api/v1/integrations/notion/connect":     {"values": map[string]string{"token": strings.Repeat("x", 9000)}},
	}
	for path, body := range bad {
		h.do("POST", path, adminTok, body, 400, nil)
	}
	h.do("POST", "/api/v1/integrations/unknown/connect", adminTok, map[string]any{}, 404, nil)
}

func TestEveryAppPresetIsConsistent(t *testing.T) {
	agentIDs := map[string]bool{"manager": true}
	for _, a := range catalog.AgentTemplates() {
		agentIDs[a.ID] = true
	}
	for _, p := range catalog.Professions() {
		for _, m := range p.Team {
			agentIDs[m.ID] = true
		}
	}
	for _, tpl := range catalog.MCPTemplates() {
		if tpl.Category == "" || tpl.Icon == "" || tpl.Description == "" {
			t.Errorf("%s needs a category, icon and description", tpl.ID)
		}
		secrets := map[string]bool{}
		for _, f := range tpl.Fields {
			if f.Kind == catalog.FieldSecret {
				secrets[f.Secret] = true
			}
			if f.Key == "" || f.Label == "" {
				t.Errorf("%s has an incomplete field", tpl.ID)
			}
		}
		// Every secret the server reads must be something the owner can fill in.
		for _, name := range append(append([]string{}, tpl.SecretEnv...), tpl.AuthSecret) {
			if name != "" && !secrets[name] {
				t.Errorf("%s needs secret %s but has no field for it", tpl.ID, name)
			}
		}
		// Every {placeholder} must have a field.
		text := strings.Join(tpl.Args, " ") + tpl.URL
		for _, v := range tpl.Env {
			text += " " + v
		}
		for _, f := range tpl.Fields {
			text = strings.ReplaceAll(text, "{"+f.Key+"}", "")
		}
		if strings.ContainsAny(text, "{}") {
			t.Errorf("%s has a placeholder without a field: %s", tpl.ID, text)
		}
		for _, a := range tpl.Agents {
			if !agentIDs[a] {
				t.Errorf("%s recommends unknown agent %q", tpl.ID, a)
			}
		}
	}
}
