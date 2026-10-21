package test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/api"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/catalog"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

type onboardingPacks struct {
	Profile     store.Profile        `json:"profile"`
	Passions    []store.Persona      `json:"passions"`
	Professions []catalog.Profession `json:"professions"`
}

func TestOnboardingListsPacks(t *testing.T) {
	h := newHarness(t)
	var got onboardingPacks
	h.do("GET", "/api/v1/onboarding", adminTok, nil, 200, &got)
	if got.Profile.Onboarded {
		t.Fatal("a new office must not be onboarded")
	}
	if len(got.Passions) < 10 {
		t.Fatalf("want at least 10 passions, got %d", len(got.Passions))
	}
	last := got.Professions[len(got.Professions)-1]
	if last.ID != catalog.OtherProfession {
		t.Fatalf("\"other\" should come last, got %q", last.ID)
	}
	for _, p := range got.Passions {
		if p.SystemPromptStyle == "" || len(p.GreetingTemplates) == 0 || len(p.DailyFeedQueries) == 0 ||
			len(p.AvatarSet) == 0 || !strings.HasPrefix(p.AccentColor, "#") {
			t.Errorf("passion %q is incomplete: %+v", p.ID, p)
		}
	}
}

func TestOnboardingBuildsTheStaff(t *testing.T) {
	h := newHarness(t)
	var res struct {
		Profile         store.Profile   `json:"profile"`
		Hired           []store.Agent   `json:"hired"`
		Routines        []store.Routine `json:"routines"`
		StarterMissions []string        `json:"starter_missions"`
	}
	body := map[string]any{
		"agent_name": "Nova", "owner_name": "Sam", "profession": "software-engineer", "passion": "gaming",
		"stack": []string{"Go", "Python", "go", " "},
	}
	h.do("POST", "/api/v1/onboarding", adminTok, body, 200, &res)

	if !res.Profile.Onboarded || res.Profile.AgentName != "Nova" || res.Profile.Passion.ID != "gaming" {
		t.Fatalf("unexpected profile: %+v", res.Profile)
	}
	if len(res.Profile.Stack) != 2 {
		t.Fatalf("stack should be de-duplicated, got %v", res.Profile.Stack)
	}
	if len(res.Hired) != 4 || len(res.StarterMissions) == 0 {
		t.Fatalf("expected 4 hires and starter missions, got %d / %v", len(res.Hired), res.StarterMissions)
	}

	var main store.Agent
	h.do("GET", "/api/v1/agents/manager", adminTok, nil, 200, &main)
	if main.Name != "Nova" || !strings.Contains(main.Instructions, "gamer") || main.Avatar != "🎮" {
		t.Fatalf("main agent was not given the persona: %+v", main)
	}
	for _, a := range res.Hired {
		if !contains(main.Team, a.ID) {
			t.Errorf("%s is not on the main agent's team", a.ID)
		}
	}

	enabled := 0
	for _, r := range res.Routines {
		if r.Enabled {
			enabled++
		}
	}
	if len(res.Routines) != 2 || enabled != 1 {
		t.Fatalf("expected 2 routines with 1 enabled, got %d with %d enabled", len(res.Routines), enabled)
	}

	var mems []store.Memory
	h.do("GET", "/api/v1/memories", adminTok, nil, 200, &mems)
	var joined string
	for _, m := range mems {
		joined += m.Content + "\n"
	}
	if !strings.Contains(joined, "Software Engineer") || !strings.Contains(joined, "Go, Python") {
		t.Fatalf("profession and stack should be remembered, got:\n%s", joined)
	}

	var st store.Settings
	h.do("GET", "/api/v1/settings", adminTok, nil, 200, &st)
	if st.OwnerName != "Sam" {
		t.Fatalf("owner name = %q", st.OwnerName)
	}

	// Choosing again swaps the persona and hires nobody twice.
	body["passion"], body["agent_name"] = "music", "Echo"
	h.do("POST", "/api/v1/onboarding", adminTok, body, 200, &res)
	if len(res.Hired) != 0 || len(res.Routines) != 0 {
		t.Fatalf("second run should not hire or schedule again: %d hires, %d routines", len(res.Hired), len(res.Routines))
	}
	h.do("GET", "/api/v1/agents/manager", adminTok, nil, 200, &main)
	if strings.Count(main.Instructions, "Your personality:") != 1 || strings.Contains(main.Instructions, "gamer") {
		t.Fatalf("persona was not replaced:\n%s", main.Instructions)
	}
	var routines []store.Routine
	h.do("GET", "/api/v1/routines", adminTok, nil, 200, &routines)
	if len(routines) != 2 {
		t.Fatalf("routines duplicated: %d", len(routines))
	}

	var prof struct {
		Profile         store.Profile `json:"profile"`
		StarterMissions []string      `json:"starter_missions"`
	}
	h.do("GET", "/api/v1/profile", adminTok, nil, 200, &prof)
	if prof.Profile.AgentName != "Echo" || len(prof.StarterMissions) == 0 {
		t.Fatalf("unexpected profile: %+v", prof)
	}
}

func TestOnboardingOtherKeepsTheDefaultTeam(t *testing.T) {
	h := newHarness(t)
	var res struct{ Hired []store.Agent }
	h.do("POST", "/api/v1/onboarding", adminTok, map[string]any{"agent_name": "Max", "profession": "other", "passion": "space"}, 200, &res)
	if len(res.Hired) != 0 {
		t.Fatalf("\"other\" should hire nobody, hired %d", len(res.Hired))
	}
}

func TestOnboardingValidation(t *testing.T) {
	h := newHarness(t)
	ok := func(m map[string]any) map[string]any {
		b := map[string]any{"agent_name": "Nova", "profession": "other", "passion": "gaming"}
		for k, v := range m {
			b[k] = v
		}
		return b
	}
	for name, body := range map[string]map[string]any{
		"no name":             ok(map[string]any{"agent_name": " "}),
		"long name":           ok(map[string]any{"agent_name": strings.Repeat("x", 61)}),
		"unknown profession":  ok(map[string]any{"profession": "wizard"}),
		"unknown passion":     ok(map[string]any{"passion": "skydiving"}),
		"custom without text": ok(map[string]any{"passion": "custom"}),
		"long custom":         ok(map[string]any{"passion": "custom", "custom_passion": strings.Repeat("x", 81)}),
		"unknown field":       ok(map[string]any{"admin": true}),
	} {
		t.Run(name, func(t *testing.T) { h.do("POST", "/api/v1/onboarding", adminTok, body, 400, nil) })
	}
	var p store.Profile
	h.do("GET", "/api/v1/profile", adminTok, nil, 200, &struct {
		Profile *store.Profile `json:"profile"`
	}{&p})
	if p.Onboarded {
		t.Fatal("a rejected request must not complete onboarding")
	}
}

func TestCustomPassionClampsModelOutput(t *testing.T) {
	h := newHarness(t)
	evil := map[string]any{
		"system_prompt_style": strings.Repeat("be bold. ", 400),
		"accent_color":        "red; background:url(x)",
		"greeting_templates":  []string{"Hi {name}", "Hi {name}", "", strings.Repeat("g", 500)},
		"avatar_set":          []string{"🦊", "🦊", "🐺", "1", "2", "3", "4", "5", "6", "7"},
	}
	var res struct{ Profile store.Profile }
	h.do("POST", "/api/v1/onboarding", adminTok, map[string]any{
		"agent_name": "Rex", "profession": "other", "passion": "custom", "custom_passion": "urban foxes", "persona": evil,
	}, 200, &res)
	p := res.Profile.Passion
	if p.ID != "custom" || res.Profile.CustomPassion != "urban foxes" || p.Title != "urban foxes" {
		t.Fatalf("unexpected custom persona: %+v", p)
	}
	if len(p.SystemPromptStyle) > 800 || p.AccentColor != store.DefaultAccent || len(p.GreetingTemplates) != 2 ||
		len(p.GreetingTemplates[1]) > 140 || len(p.AvatarSet) != 8 {
		t.Fatalf("model output was not clamped: %+v", p)
	}

	// No generated persona at all (no model yet): a safe fallback is built.
	h.do("POST", "/api/v1/onboarding", adminTok, map[string]any{
		"agent_name": "Rex", "profession": "other", "passion": "custom", "custom_passion": "bonsai",
	}, 200, &res)
	if p := res.Profile.Passion; !strings.Contains(p.SystemPromptStyle, "bonsai") || len(p.AccentColor) != 7 {
		t.Fatalf("fallback persona is wrong: %+v", p)
	}
}

func TestPersonaTaskRoundTrip(t *testing.T) {
	h := newHarness(t)
	var created struct {
		TaskID string `json:"task_id"`
	}
	h.do("POST", "/api/v1/persona", adminTok, map[string]string{"passion": " "}, 400, nil)
	h.do("POST", "/api/v1/persona", adminTok, map[string]string{"passion": "urban foxes"}, 202, &created)

	var st struct {
		Status  string         `json:"status"`
		Persona *store.Persona `json:"persona"`
	}
	h.do("GET", "/api/v1/persona/"+created.TaskID, adminTok, nil, 200, &st)
	if st.Status != store.StatusQueued || st.Persona != nil {
		t.Fatalf("expected a queued task, got %+v", st)
	}

	// Persona work never shows up in the owner's task list.
	var tasks []store.Task
	h.do("GET", "/api/v1/tasks?parent_id=", adminTok, nil, 200, &tasks)
	for _, tk := range tasks {
		if tk.Kind == store.KindPersona {
			t.Fatal("persona task leaked into the task list")
		}
	}

	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, &claim)
	if claim.Task.Kind != store.KindPersona || claim.Task.Description != "urban foxes" {
		t.Fatalf("worker claimed %+v", claim.Task)
	}
	result, _ := json.Marshal(map[string]any{
		"title": "Urban Foxes", "system_prompt_style": "Sly, nimble and curious.", "accent_color": "#D2691E",
		"greeting_templates": []string{"Hi {name}!"}, "daily_feed_queries": []string{"urban fox sightings"},
		"avatar_set": []string{"🦊"},
	})
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/complete", workerTok, map[string]string{"result": "```json\n" + string(result) + "\n```"}, 204, nil)

	h.do("GET", "/api/v1/persona/"+created.TaskID, adminTok, nil, 200, &st)
	if st.Status != store.StatusDone || st.Persona == nil || st.Persona.ID != "custom" || st.Persona.AccentColor != "#D2691E" {
		t.Fatalf("unexpected persona: %+v", st)
	}

	// A task that is not a persona task is not exposed here.
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "x"}, 201, &asg)
	h.do("GET", "/api/v1/persona/"+asg.ID, adminTok, nil, 404, nil)
}

func TestPersonaTaskGarbageResultFails(t *testing.T) {
	h := newHarness(t)
	var created struct {
		TaskID string `json:"task_id"`
	}
	h.do("POST", "/api/v1/persona", adminTok, map[string]string{"passion": "chess"}, 202, &created)
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, &claim)
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/complete", workerTok, map[string]string{"result": "I love chess!"}, 204, nil)
	var st struct{ Status, Error string }
	h.do("GET", "/api/v1/persona/"+created.TaskID, adminTok, nil, 200, &st)
	if st.Status != store.StatusFailed || st.Error == "" {
		t.Fatalf("garbage should read as failed, got %+v", st)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
