package test

import (
	"slices"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/api"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func (h *harness) agents() map[string]store.Agent {
	h.t.Helper()
	var list []store.Agent
	h.do("GET", "/api/v1/agents", adminTok, nil, 200, &list)
	out := map[string]store.Agent{}
	for _, a := range list {
		out[a.ID] = a
	}
	return out
}

func TestOfficeStartsWithDefaultTeam(t *testing.T) {
	h := newHarness(t)
	agents := h.agents()
	main := agents["manager"]
	if !main.IsMain() || len(agents) != 6 {
		t.Fatalf("unexpected starting team: %+v", agents)
	}
	if !slices.Equal(main.Team, []string{"researcher", "writer", "engineer", "analyst", "reviewer"}) {
		t.Fatalf("main agent team = %v", main.Team)
	}
	if agents["researcher"].Instructions == "" || len(agents["researcher"].Skills) != 1 {
		t.Fatalf("specialists should come with instructions and their role's skills: %+v", agents["researcher"])
	}
}

func TestCreateAgentFromTemplateJoinsMainTeam(t *testing.T) {
	h := newHarness(t)
	var a store.Agent
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"template_id": "travel-planner"}, 201, &a)
	if a.Name != "Tess" || a.ID != "tess" || a.Role != store.RoleSpecialist || a.Instructions == "" {
		t.Fatalf("unexpected agent: %+v", a)
	}
	if !slices.Contains(h.agents()["manager"].Team, a.ID) {
		t.Fatal("new agent should join the main agent's team")
	}

	// Same template again: a unique id, and it can stay off the team.
	var b store.Agent
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"template_id": "travel-planner", "join_team": false}, 201, &b)
	if b.ID != "tess-2" || slices.Contains(h.agents()["manager"].Team, b.ID) {
		t.Fatalf("second agent: id %s, team %v", b.ID, h.agents()["manager"].Team)
	}
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"template_id": "nope"}, 400, nil)
}

func TestCustomAgentValidationAndUpdate(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"name": ""}, 400, nil)
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"name": "X", "tools": []string{"shell"}}, 400, nil)
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"name": "X", "mcp_servers": []string{"ghost"}}, 400, nil)
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{"name": "X", "effort": "ludicrous"}, 400, nil)

	var a store.Agent
	h.do("POST", "/api/v1/agents", adminTok, map[string]any{
		"name": "Bea Bot", "title": "Bookkeeper", "instructions": "Be exact.", "tools": []string{"files"},
	}, 201, &a)
	if a.ID != "bea-bot" {
		t.Fatalf("id = %s", a.ID)
	}
	var upd store.Agent
	h.do("PUT", "/api/v1/agents/"+a.ID, adminTok, map[string]any{
		"instructions": "Be exact and brief.", "team": []string{"writer", a.ID}, "model": "claude-haiku-5-5",
	}, 200, &upd)
	if upd.Instructions != "Be exact and brief." || upd.Title != "Bookkeeper" || !slices.Equal(upd.Team, []string{"writer"}) {
		t.Fatalf("partial update went wrong: %+v", upd)
	}
	h.do("PUT", "/api/v1/agents/manager", adminTok, map[string]any{"enabled": false}, 400, nil)
	h.do("PUT", "/api/v1/agents/nobody", adminTok, map[string]any{"name": "x"}, 404, nil)
}

func TestSwitchMainAgent(t *testing.T) {
	h := newHarness(t)
	h.do("DELETE", "/api/v1/agents/manager", adminTok, nil, 409, nil) // the main agent can't be removed
	h.do("POST", "/api/v1/agents/writer/main", adminTok, nil, 200, nil)
	agents := h.agents()
	if !agents["writer"].IsMain() || agents["manager"].IsMain() {
		t.Fatal("main agent did not switch")
	}
	team := agents["writer"].Team
	if !slices.Contains(team, "manager") || !slices.Contains(team, "researcher") || slices.Contains(team, "writer") {
		t.Fatalf("new main agent should inherit the team plus the old main: %v", team)
	}
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Draft"}, 201, &asg)
	if asg.Assignee != "writer" {
		t.Fatalf("assignments should go to the new main agent, got %s", asg.Assignee)
	}
}

func TestDeleteAgentCleansUpTeamsAndWork(t *testing.T) {
	h := newHarness(t)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Research", "agent_id": "researcher"}, 201, &asg)
	h.do("DELETE", "/api/v1/agents/researcher", adminTok, nil, 204, nil)
	if slices.Contains(h.agents()["manager"].Team, "researcher") {
		t.Fatal("deleted agent still on a team")
	}
	var got api.TaskDetail
	h.do("GET", "/api/v1/tasks/"+asg.ID, adminTok, nil, 200, &got)
	if got.Assignee != "manager" {
		t.Fatalf("queued work should move to the main agent, got %s", got.Assignee)
	}
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "x", "agent_id": "researcher"}, 400, nil)
}

func TestTurningAgentOffHandsOverQueuedWork(t *testing.T) {
	h := newHarness(t)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Draft", "agent_id": "writer"}, 201, &asg)
	h.do("PUT", "/api/v1/agents/writer", adminTok, map[string]any{"enabled": false}, 200, nil)
	var got api.TaskDetail
	h.do("GET", "/api/v1/tasks/"+asg.ID, adminTok, nil, 200, &got)
	if got.Assignee != "manager" {
		t.Fatalf("queued work of a disabled agent should move to the main agent, got %s", got.Assignee)
	}
}

func TestWorkerRosterResolvesExtensionsAndLongPolls(t *testing.T) {
	h := newHarness(t)
	var srv store.MCPServer
	h.do("POST", "/api/v1/mcp-servers", adminTok, map[string]any{"template_id": "time"}, 201, &srv)
	var off store.MCPServer
	h.do("POST", "/api/v1/mcp-servers", adminTok, map[string]any{
		"name": "Local", "transport": "http", "url": "http://127.0.0.1:9000/mcp",
	}, 201, &off)
	off.Enabled = false
	h.do("PUT", "/api/v1/mcp-servers/"+off.ID, adminTok, off, 200, nil)
	h.do("PUT", "/api/v1/agents/writer", adminTok, map[string]any{"mcp_servers": []string{srv.ID, off.ID}}, 200, nil)
	h.do("PUT", "/api/v1/agents/analyst", adminTok, map[string]any{"enabled": false}, 200, nil)

	var roster struct {
		Revision int64             `json:"revision"`
		Agents   []api.WorkerAgent `json:"agents"`
	}
	h.do("GET", "/internal/v1/agents", workerTok, nil, 200, &roster)
	byID := map[string]api.WorkerAgent{}
	for _, a := range roster.Agents {
		byID[a.ID] = a
	}
	if _, ok := byID["analyst"]; ok {
		t.Fatal("disabled agents must not reach the worker")
	}
	if slices.Contains(byID["manager"].Team, "analyst") {
		t.Fatal("disabled agents must be left out of teams")
	}
	if w := byID["writer"]; len(w.MCP) != 1 || w.MCP[0].Command != "uvx" || len(w.SkillDocs) != 1 || w.SkillDocs[0].Content == "" {
		t.Fatalf("writer should get only enabled servers and full skill docs: %+v", w)
	}
	// Nothing changed since this revision: the long-poll times out with 204.
	h.do("GET", "/internal/v1/agents?after="+itoa(roster.Revision), workerTok, nil, 204, nil)
	h.do("PUT", "/api/v1/agents/writer", adminTok, map[string]any{"title": "Senior Writer"}, 200, nil)
	h.do("GET", "/internal/v1/agents?after="+itoa(roster.Revision), workerTok, nil, 200, nil)
}

func TestMCPServerValidation(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []map[string]any{
		{"name": "x", "transport": "stdio"},
		{"name": "x", "transport": "http", "url": "file:///etc"},
		{"name": "x", "transport": "carrier-pigeon"},
		{"name": "x", "transport": "stdio", "command": "npx", "env": map[string]string{"BAD NAME": "1"}},
		{"name": "x", "transport": "stdio", "command": "npx", "secret_env": []string{"no-dashes"}},
		{"template_id": "nope"},
	} {
		h.do("POST", "/api/v1/mcp-servers", adminTok, bad, 400, nil)
	}
	var fs store.MCPServer
	h.do("POST", "/api/v1/mcp-servers", adminTok, map[string]any{
		"template_id": "filesystem", "args": []string{"-y", "@modelcontextprotocol/server-filesystem", "D:\\Notes"},
	}, 201, &fs)
	if fs.ID != "filesystem" || fs.Args[2] != "D:\\Notes" || fs.TemplateID != "filesystem" {
		t.Fatalf("template override not applied: %+v", fs)
	}
	// Without a worker the test endpoint explains instead of failing.
	var res struct {
		Error string `json:"error"`
	}
	h.do("POST", "/api/v1/mcp-servers/"+fs.ID+"/test", adminTok, nil, 200, &res)
	if res.Error == "" {
		t.Fatal("expected an explanation when no worker is available")
	}
	h.do("PUT", "/api/v1/agents/engineer", adminTok, map[string]any{"mcp_servers": []string{fs.ID}}, 200, nil)
	h.do("DELETE", "/api/v1/mcp-servers/"+fs.ID, adminTok, nil, 204, nil)
	if len(h.agents()["engineer"].MCPServers) != 0 {
		t.Fatal("deleting a server must detach it from agents")
	}
}

func TestSkills(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/v1/skills", adminTok, map[string]any{"name": "x", "content": "y"}, 400, nil) // no description
	var sk store.Skill
	h.do("POST", "/api/v1/skills", adminTok, map[string]any{
		"name": "Invoice Style", "description": "Use when writing invoices.", "content": "# Invoices\nAlways add VAT.",
	}, 201, &sk)
	if sk.Name != "invoice-style" {
		t.Fatalf("skill names are normalised to SKILL.md style, got %q", sk.Name)
	}
	// Installing a template twice reuses the existing skill.
	var a, b store.Skill
	h.do("POST", "/api/v1/skills", adminTok, map[string]any{"template_id": "meeting-notes"}, 201, &a)
	h.do("POST", "/api/v1/skills", adminTok, map[string]any{"template_id": "meeting-notes"}, 201, &b)
	if a.ID != b.ID {
		t.Fatal("skill template installed twice")
	}
	h.do("PUT", "/api/v1/agents/writer", adminTok, map[string]any{"skills": []string{sk.ID, a.ID}}, 200, nil)
	h.do("DELETE", "/api/v1/skills/"+sk.ID, adminTok, nil, 204, nil)
	if got := h.agents()["writer"].Skills; !slices.Equal(got, []string{a.ID}) {
		t.Fatalf("writer skills = %v", got)
	}
}

func TestPluginInstallAndUninstall(t *testing.T) {
	h := newHarness(t)
	var p store.Plugin
	h.do("POST", "/api/v1/plugins", adminTok, map[string]any{"catalog_id": "research-desk"}, 201, &p)
	if len(p.Agents) != 1 || len(p.MCPServers) != 1 || len(p.Skills) != 1 {
		t.Fatalf("plugin contents: %+v", p)
	}
	agent := h.agents()[p.Agents[0]]
	if !slices.Equal(agent.MCPServers, p.MCPServers) || agent.PluginID != p.ID {
		t.Fatalf("plugin agent should use the plugin's server: %+v", agent)
	}
	if !slices.Contains(h.agents()["manager"].Team, agent.ID) {
		t.Fatal("plugin agents join the main agent's team")
	}

	// An imported bundle (e.g. a Claude Code plugin folder) with bundle-local references.
	var imp store.Plugin
	h.do("POST", "/api/v1/plugins", adminTok, map[string]any{"bundle": map[string]any{
		"name":        "My pack",
		"skills":      []map[string]any{{"id": "tone", "name": "tone", "description": "Use for tone.", "content": "Be warm."}},
		"mcp_servers": []map[string]any{{"id": "docs", "name": "Docs", "transport": "http", "url": "https://example.com/mcp"}},
		"agents":      []map[string]any{{"name": "Pat", "instructions": "Help.", "skills": []string{"tone"}, "mcp_servers": []string{"docs"}}},
	}}, 201, &imp)
	pat := h.agents()[imp.Agents[0]]
	if !slices.Equal(pat.Skills, imp.Skills) || !slices.Equal(pat.MCPServers, imp.MCPServers) {
		t.Fatalf("bundle references not remapped: %+v vs %+v", pat, imp)
	}

	h.do("DELETE", "/api/v1/plugins/"+p.ID, adminTok, nil, 204, nil)
	agents := h.agents()
	if _, ok := agents[agent.ID]; ok || slices.Contains(agents["manager"].Team, agent.ID) {
		t.Fatal("uninstalling must remove the plugin's agents")
	}
	var servers []store.MCPServer
	h.do("GET", "/api/v1/mcp-servers", adminTok, nil, 200, &servers)
	if len(servers) != 1 || servers[0].PluginID != imp.ID {
		t.Fatalf("only the imported plugin's server should remain: %+v", servers)
	}
}

func TestChatWithAgent(t *testing.T) {
	h := newHarness(t)
	var c store.Conversation
	h.do("POST", "/api/v1/conversations", adminTok, map[string]string{"agent_id": "ghost"}, 400, nil)
	h.do("POST", "/api/v1/conversations", adminTok, map[string]string{"agent_id": "writer"}, 201, &c)
	h.do("POST", "/api/v1/conversations/"+c.ID+"/messages", adminTok, map[string]string{"content": "Hi Quill"}, 201, nil)

	// A chat is claimed on the chat lane only, so long tasks never block it.
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"writer"}, "lane": "work"}, 204, nil)
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"writer"}, "lane": "chat"}, 200, &claim)
	if claim.Task.Kind != store.KindChat || claim.Task.Description != "Hi Quill" || len(claim.History) != 0 {
		t.Fatalf("unexpected chat claim: %+v", claim)
	}
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/progress", workerTok, map[string]string{"status": "Thinking…"}, 204, nil)
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/complete", workerTok, map[string]string{"result": "Hello!"}, 204, nil)

	h.do("POST", "/api/v1/conversations/"+c.ID+"/messages", adminTok, map[string]string{"content": "Write a haiku"}, 201, nil)
	h.do("POST", "/api/v1/conversations/"+c.ID+"/messages", adminTok, map[string]string{"content": "About tea"}, 201, nil)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"writer"}, "lane": "chat"}, 200, &claim)
	if len(claim.History) != 2 || claim.History[1].Content != "Hello!" || claim.Task.Description != "Write a haiku" {
		t.Fatalf("history should hold the earlier exchange only: %+v", claim.History)
	}
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/fail", workerTok, map[string]string{"error": "model offline"}, 204, nil)

	var detail api.ConversationDetail
	h.do("GET", "/api/v1/conversations/"+c.ID, adminTok, nil, 200, &detail)
	if len(detail.Messages) != 5 || detail.Messages[4].Status != "failed" || !detail.Busy || detail.Title != "Hi Quill" {
		t.Fatalf("unexpected conversation: %+v", detail)
	}
	var top []store.Task
	h.do("GET", "/api/v1/tasks?parent_id=", adminTok, nil, 200, &top)
	if len(top) != 0 {
		t.Fatalf("chat replies must not appear as assignments: %+v", top)
	}
	h.do("DELETE", "/api/v1/conversations/"+c.ID, adminTok, nil, 204, nil)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"writer"}, "lane": "chat"}, 204, nil)
}

func TestWorkerStartRequeuesInterruptedWork(t *testing.T) {
	h := newHarness(t)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Long job"}, 201, &asg)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, nil)
	// The worker restarts mid-task (e.g. settings were saved).
	var res struct {
		Requeued int `json:"requeued"`
	}
	h.do("POST", "/internal/v1/worker/start", workerTok, nil, 200, &res)
	if res.Requeued != 1 {
		t.Fatalf("requeued = %d", res.Requeued)
	}
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, &claim)
	if claim.Task.ID != asg.ID {
		t.Fatal("interrupted task was not picked up again")
	}
}

func TestMainAgentWithoutTeamGetsFallbackFromWorkerAssignment(t *testing.T) {
	h := newHarness(t)
	var asg store.Task
	h.do("POST", "/internal/v1/assignments", workerTok, map[string]string{"title": "From chat", "agent_id": "writer"}, 201, &asg)
	if asg.Assignee != "manager" || asg.Kind != store.KindAssignment {
		t.Fatalf("chat-started assignments go to the main agent: %+v", asg)
	}
}

func TestDelegateRejectsDisabledAssignee(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/api/v1/agents/engineer", adminTok, map[string]any{"enabled": false}, 200, nil)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "x"}, 201, &asg)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, nil)
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/delegate", workerTok, map[string]any{
		"subtasks":  []map[string]any{{"title": "Code", "assignee": "engineer"}},
		"synthesis": map[string]string{"title": "Wrap", "assignee": "manager"},
	}, 400, nil)
}

func TestCatalog(t *testing.T) {
	h := newHarness(t)
	var cat struct {
		Tools      []map[string]any `json:"tools"`
		Agents     []map[string]any `json:"agents"`
		MCPServers []map[string]any `json:"mcp_servers"`
		Skills     []map[string]any `json:"skills"`
		Plugins    []map[string]any `json:"plugins"`
	}
	h.do("GET", "/api/v1/catalog", adminTok, nil, 200, &cat)
	if len(cat.Tools) < 4 || len(cat.Agents) < 15 || len(cat.MCPServers) < 5 || len(cat.Skills) < 5 || len(cat.Plugins) < 3 {
		t.Fatalf("catalog too small: %d %d %d %d %d", len(cat.Tools), len(cat.Agents), len(cat.MCPServers), len(cat.Skills), len(cat.Plugins))
	}
	// Every plugin pack must install cleanly.
	for _, p := range cat.Plugins {
		h.do("POST", "/api/v1/plugins", adminTok, map[string]any{"catalog_id": p["id"]}, 201, nil)
	}
}
