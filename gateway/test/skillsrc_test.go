package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/skillsrc"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func TestSkillSourceParsing(t *testing.T) {
	good := map[string]skillsrc.Source{
		"acme/skills":                                        {Owner: "acme", Repo: "skills"},
		"acme/skills/refactor":                               {Owner: "acme", Repo: "skills", Skill: "refactor"},
		"acme/skills@refactor":                               {Owner: "acme", Repo: "skills", Skill: "refactor"},
		"npx skills add acme/skills":                         {Owner: "acme", Repo: "skills"},
		"npx skills@latest add acme/skills --skill debug":    {Owner: "acme", Repo: "skills", Skill: "debug"},
		"npx skills add https://github.com/acme/skills -s x": {Owner: "acme", Repo: "skills", Skill: "x"},
		"https://skills.sh/acme/skills/refactor":             {Owner: "acme", Repo: "skills", Skill: "refactor"},
		"https://www.skills.sh/acme/skills":                  {Owner: "acme", Repo: "skills"},
		"https://github.com/acme/skills.git":                 {Owner: "acme", Repo: "skills"},
		"https://github.com/acme/skills/tree/main/tools/x":   {Owner: "acme", Repo: "skills", Ref: "main", Dir: "tools/x"},
		"https://github.com/acme/skills/blob/v1/a/SKILL.md":  {Owner: "acme", Repo: "skills", Ref: "v1", Dir: "a"},
	}
	for in, want := range good {
		got, err := skillsrc.Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "   ", "hello", "https://evil.example/acme/skills", "https://skills.sh/site/open.feishu.cn/lark-doc",
		"https://skills.sh/topic/testing", "https://skills.sh/", "http://169.254.169.254/latest", "../etc/passwd",
		"acme/skills/refactor/extra/parts", "file:///etc/passwd", "-rf /",
	} {
		if got, err := skillsrc.Parse(in); err == nil {
			t.Errorf("Parse(%q) should fail, got %+v", in, got)
		}
	}
}

// fakeGitHub serves one repository: two skills in folders, one without front matter, and a stray file.
func fakeGitHub(t *testing.T) *skillsrc.Client {
	t.Helper()
	files := map[string]string{
		"skills/refactor/SKILL.md": "---\nname: refactor\ndescription: >\n  Use when cleaning up code\n  without changing behaviour.\n---\n# Refactor\nDo it in small steps.\n",
		"skills/debug/SKILL.md":    "---\nname: \"debug\"\ndescription: Find and fix bugs\n---\nReproduce first.\n",
		"skills/nofront/SKILL.md":  "No front matter here.\n",
		"README.md":                "# Skills\n",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/skills/git/trees/", func(w http.ResponseWriter, r *http.Request) {
		var entries []string
		for p := range files {
			entries = append(entries, `{"path":"`+p+`","type":"blob"}`)
		}
		_, _ = w.Write([]byte(`{"truncated":false,"tree":[` + strings.Join(entries, ",") + `]}`))
	})
	mux.HandleFunc("/acme/skills/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/acme/skills/")
		_, p, _ := strings.Cut(rest, "/") // drop the ref
		if body, ok := files[p]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &skillsrc.Client{HTTP: srv.Client(), APIBase: srv.URL, RawBase: srv.URL}
}

func TestDiscoverReadsSkillsFromARepository(t *testing.T) {
	c := fakeGitHub(t)
	found, err := c.Discover(context.Background(), skillsrc.Source{Owner: "acme", Repo: "skills"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]skillsrc.Found{}
	for _, f := range found {
		names[f.Name] = f
	}
	// The skill without a description is not usable, so it is left out.
	if len(found) != 2 || names["refactor"].Description != "Use when cleaning up code without changing behaviour." ||
		!strings.Contains(names["refactor"].Content, "small steps") || strings.Contains(names["refactor"].Content, "---") ||
		names["debug"].Description != "Find and fix bugs" {
		t.Fatalf("unexpected skills: %+v", found)
	}

	one, err := c.Discover(context.Background(), skillsrc.Source{Owner: "acme", Repo: "skills", Skill: "DEBUG"})
	if err != nil || len(one) != 1 || one[0].Name != "debug" {
		t.Fatalf("asking for one skill: %+v, %v", one, err)
	}
	if _, err := c.Discover(context.Background(), skillsrc.Source{Owner: "acme", Repo: "skills", Skill: "missing"}); err == nil {
		t.Fatal("an unknown skill should be an error")
	}
	if _, err := c.Discover(context.Background(), skillsrc.Source{Owner: "acme", Repo: "other"}); err == nil {
		t.Fatal("an unknown repository should be an error")
	}
}

func TestInstallSkillsFromASource(t *testing.T) {
	h := newHarness(t)
	h.gw.SkillSource = fakeGitHub(t)

	var found struct {
		Skills []struct {
			Name, Path string
			Installed  bool
		}
	}
	h.do("POST", "/api/v1/skills/discover", adminTok, map[string]string{"source": "npx skills add acme/skills"}, 200, &found)
	if len(found.Skills) != 2 || found.Skills[0].Installed {
		t.Fatalf("discover: %+v", found)
	}
	h.do("POST", "/api/v1/skills/discover", adminTok, map[string]string{"source": "https://evil.example/x/y"}, 400, nil)
	h.do("POST", "/api/v1/skills/discover", adminTok, map[string]string{"source": "acme/missing"}, 502, nil)

	var done struct {
		Installed []store.Skill
		Skipped   []string
	}
	h.do("POST", "/api/v1/skills/install", adminTok, map[string]any{"source": "acme/skills", "paths": []string{"skills/debug/SKILL.md"}}, 200, &done)
	if len(done.Installed) != 1 || done.Installed[0].Name != "debug" || !done.Installed[0].Enabled {
		t.Fatalf("install one: %+v", done)
	}
	// Installing again skips what is already there; installing everything adds the rest.
	h.do("POST", "/api/v1/skills/install", adminTok, map[string]any{"source": "acme/skills"}, 200, &done)
	if len(done.Installed) != 1 || done.Installed[0].Name != "refactor" || len(done.Skipped) != 1 {
		t.Fatalf("install all: %+v", done)
	}
	h.do("POST", "/api/v1/skills/discover", adminTok, map[string]string{"source": "acme/skills"}, 200, &found)
	if !found.Skills[0].Installed || !found.Skills[1].Installed {
		t.Fatalf("both should now show as installed: %+v", found)
	}
	// Imported skills belong to no agent until the owner attaches them.
	for _, a := range h.agents() {
		for _, id := range a.Skills {
			if id == done.Installed[0].ID {
				t.Fatalf("%s got an imported skill without being asked", a.ID)
			}
		}
	}
}
