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

func TestOwnerProfileIsOneRecordThatIsReplaced(t *testing.T) {
	h := newHarness(t)
	var first store.Memory
	h.do("PUT", "/internal/v1/memories/profile", workerTok, map[string]string{"content": "Sam is an AI engineer.", "employee_id": "manager"}, 200, &first)
	h.do("POST", "/internal/v1/memories", workerTok, map[string]string{"content": "Sam prefers tea."}, 201, nil)
	h.do("PUT", "/api/v1/memories/profile", adminTok, map[string]string{"content": "Sam is an AI engineer in London."}, 200, nil)

	var list []store.Memory
	h.do("GET", "/api/v1/memories", adminTok, nil, 200, &list)
	profiles := 0
	for _, m := range list {
		if m.Kind == store.KindProfile {
			profiles++
			if m.Content != "Sam is an AI engineer in London." || m.EmployeeID != "" {
				t.Fatalf("profile not replaced by the owner: %+v", m)
			}
		}
	}
	if profiles != 1 || len(list) != 2 || list[0].Kind != store.KindProfile {
		t.Fatalf("want one profile listed first plus one fact: %+v", list)
	}

	h.do("PUT", "/api/v1/memories/profile", adminTok, map[string]string{"content": ""}, 204, nil)
	h.do("GET", "/api/v1/memories", adminTok, nil, 200, &list)
	if len(list) != 1 || list[0].Kind != store.KindFact {
		t.Fatalf("clearing the profile should leave the facts: %+v", list)
	}
}
