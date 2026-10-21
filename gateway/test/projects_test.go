package test

import (
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func TestProjectsAreSavedUpdatedAndValidated(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	var p store.Project
	h.do("PUT", "/internal/v1/projects", workerTok, map[string]string{"name": "AbroadMate", "path": dir, "notes": "SaaS, Next.js"}, 200, &p)
	if p.Name != "AbroadMate" || p.Path == "" || p.Notes != "SaaS, Next.js" {
		t.Fatalf("project not saved: %+v", p)
	}

	// Same name in another letter case updates it; an empty path keeps the stored one.
	var again store.Project
	h.do("PUT", "/api/v1/projects", adminTok, map[string]string{"name": "abroadmate", "notes": "SaaS, Next.js, UI in src/components"}, 200, &again)
	if again.ID != p.ID || again.Path != p.Path || again.Notes != "SaaS, Next.js, UI in src/components" {
		t.Fatalf("project not updated in place: %+v", again)
	}

	var list []store.Project
	h.do("GET", "/internal/v1/projects", workerTok, nil, 200, &list)
	if len(list) != 1 {
		t.Fatalf("want one project, got %d", len(list))
	}

	h.do("PUT", "/internal/v1/projects", workerTok, map[string]string{"name": "Bad", "path": "relative/dir"}, 400, nil)
	h.do("PUT", "/internal/v1/projects", workerTok, map[string]string{"name": "Missing", "path": dir + "/nope"}, 400, nil)
	h.do("PUT", "/internal/v1/projects", workerTok, map[string]string{"name": ""}, 400, nil)

	h.do("DELETE", "/api/v1/projects/"+p.ID, adminTok, nil, 204, nil)
	h.do("DELETE", "/api/v1/projects/"+p.ID, adminTok, nil, 404, nil)
}
