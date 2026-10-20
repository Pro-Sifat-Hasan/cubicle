package test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/api"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/config"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/events"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/secrets"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

const (
	adminTok  = "admin-token-0123456789abcdef0123456789abcdef"
	workerTok = "worker-token-0123456789abcdef0123456789abcdef"
)

type harness struct {
	t   *testing.T
	srv *httptest.Server
	gw  *api.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secrets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Home: dir, AdminToken: adminTok, WorkerToken: workerTok, DailyTokenBudget: 1000}
	s := api.New(cfg, st, events.NewHub(), box, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.LongPollWait = 50 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, gw: s}
}

func (h *harness) do(method, path, token string, body any, wantStatus int, out any) {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		h.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, wantStatus, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			h.t.Fatalf("decode %s: %v: %s", path, err, raw)
		}
	}
}

func TestAuthIsEnforcedAndSeparated(t *testing.T) {
	h := newHarness(t)
	h.do("GET", "/healthz", "", nil, 200, nil)
	h.do("GET", "/api/v1/status", "", nil, 401, nil)
	h.do("GET", "/api/v1/status", workerTok, nil, 401, nil) // worker token can't use owner API
	h.do("POST", "/internal/v1/tasks/claim", adminTok, map[string]any{"employee_ids": []string{"x"}}, 401, nil)
	h.do("GET", "/api/v1/status", adminTok, nil, 200, nil)
}

func TestFullAssignmentLifecycle(t *testing.T) {
	h := newHarness(t)
	roster := []map[string]string{
		{"id": "manager", "name": "Max", "title": "Chief of Staff", "role": "manager"},
		{"id": "researcher", "name": "Rhea", "title": "Researcher", "role": "specialist"},
		{"id": "writer", "name": "Quill", "title": "Writer", "role": "specialist"},
	}
	h.do("PUT", "/internal/v1/employees", workerTok, roster, 204, nil)

	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Weekly report"}, 201, &asg)
	if asg.Assignee != "manager" || asg.Status != store.StatusQueued {
		t.Fatalf("unexpected assignment: %+v", asg)
	}

	// Manager claims and delegates.
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 200, &claim)
	if claim.Task.ID != asg.ID {
		t.Fatalf("claimed wrong task %s", claim.Task.ID)
	}
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/delegate", workerTok, map[string]any{
		"subtasks": []map[string]any{
			{"title": "Research", "assignee": "researcher"},
			{"title": "Write", "assignee": "writer", "depends_on": []int{0}},
		},
		"synthesis": map[string]string{"title": "Wrap up", "assignee": "manager"},
	}, 201, nil)

	// Writer must wait for researcher.
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"writer"}}, 204, nil)

	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"researcher"}}, 200, &claim)
	research := claim.Task.ID
	// Researcher asks the human, task parks, human answers, task re-queues.
	var item store.InboxItem
	h.do("POST", "/internal/v1/tasks/"+research+"/ask", workerTok, map[string]string{"employee_id": "researcher", "question": "Which week?"}, 201, &item)
	h.do("POST", "/api/v1/inbox/"+item.ID+"/answer", adminTok, map[string]string{"answer": "Last week"}, 200, nil)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"researcher"}}, 200, &claim)
	if len(claim.HumanAnswers) != 1 || claim.HumanAnswers[0].Answer != "Last week" {
		t.Fatalf("expected human answer in claim, got %+v", claim.HumanAnswers)
	}
	h.do("POST", "/internal/v1/tasks/"+research+"/complete", workerTok, map[string]string{"result": "facts"}, 204, nil)

	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"writer"}}, 200, &claim)
	if len(claim.DependencyResults) != 1 || claim.DependencyResults[0].Result != "facts" {
		t.Fatalf("writer should see research result, got %+v", claim.DependencyResults)
	}
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/complete", workerTok, map[string]string{"result": "draft"}, 204, nil)

	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 200, &claim)
	if claim.Task.Kind != store.KindSynthesis {
		t.Fatalf("expected synthesis, got %s", claim.Task.Kind)
	}
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/complete", workerTok, map[string]string{"result": "final report"}, 204, nil)

	var got api.TaskDetail
	h.do("GET", "/api/v1/tasks/"+asg.ID, adminTok, nil, 200, &got)
	if got.Status != store.StatusDone || got.Result != "final report" || len(got.Children) != 3 {
		t.Fatalf("assignment not finished correctly: %+v", got)
	}
}

func TestSubtaskFailureFailsAssignment(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/internal/v1/employees", workerTok, []map[string]string{
		{"id": "manager", "name": "Max", "title": "Boss", "role": "manager"},
		{"id": "dev", "name": "Dev", "title": "Engineer", "role": "specialist"},
	}, 204, nil)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Ship it"}, 201, &asg)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 200, nil)
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/delegate", workerTok, map[string]any{
		"subtasks":  []map[string]any{{"title": "Code", "assignee": "dev"}},
		"synthesis": map[string]string{"title": "Wrap", "assignee": "manager"},
	}, 201, nil)
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"dev"}}, 200, &claim)
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/fail", workerTok, map[string]string{"error": "boom"}, 204, nil)

	var got api.TaskDetail
	h.do("GET", "/api/v1/tasks/"+asg.ID, adminTok, nil, 200, &got)
	if got.Status != store.StatusFailed {
		t.Fatalf("assignment status %s, want failed", got.Status)
	}
	for _, c := range got.Children {
		if c.Kind == store.KindSynthesis && c.Status != store.StatusCancelled {
			t.Fatalf("synthesis should be cancelled, got %s", c.Status)
		}
	}
}

func TestDelegateRejectsUnknownAssignee(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/internal/v1/employees", workerTok, []map[string]string{{"id": "manager", "name": "Max", "title": "Boss", "role": "manager"}}, 204, nil)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "x"}, 201, &asg)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 200, nil)
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/delegate", workerTok, map[string]any{
		"subtasks":  []map[string]any{{"title": "Hack", "assignee": "ghost"}},
		"synthesis": map[string]string{"title": "Wrap", "assignee": "manager"},
	}, 400, nil)
}

func TestCircuitBreakerBlocksClaims(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/internal/v1/usage", workerTok, map[string]any{"input_tokens": 800, "output_tokens": 300}, 204, nil)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 503, nil)
	h.do("POST", "/api/v1/breaker/reset", adminTok, nil, 204, nil)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"employee_ids": []string{"manager"}}, 204, nil)
}

func TestSecretsAreWriteOnlyAndEncrypted(t *testing.T) {
	h := newHarness(t)
	h.do("PUT", "/api/v1/secrets/bad-name", adminTok, map[string]string{"value": "x"}, 400, nil)
	h.do("PUT", "/api/v1/secrets/ANTHROPIC_API_KEY", adminTok, map[string]string{"value": "sk-test"}, 204, nil)
	var names []string
	h.do("GET", "/api/v1/secrets", adminTok, nil, 200, &names)
	if len(names) != 1 || names[0] != "ANTHROPIC_API_KEY" {
		t.Fatalf("names = %v", names)
	}
	env := h.gw.WorkerEnv()
	for _, want := range []string{"ANTHROPIC_API_KEY=sk-test", "CUBICLE_LLM_KIND=anthropic"} {
		if !slices.Contains(env, want) {
			t.Fatalf("worker env missing %q: %v", want, env)
		}
	}
}

func TestRoutinesValidateSchedules(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/v1/routines", adminTok, map[string]string{"title": "Inbox zero", "schedule": "weekdays@09:00"}, 201, nil)
	h.do("POST", "/api/v1/routines", adminTok, map[string]string{"title": "Too often", "schedule": "every:1m"}, 400, nil)
	h.do("POST", "/api/v1/routines", adminTok, map[string]string{"title": "Nonsense", "schedule": "sometimes"}, 400, nil)
	var list []store.Routine
	h.do("GET", "/api/v1/routines", adminTok, nil, 200, &list)
	if len(list) != 1 {
		t.Fatalf("routines = %d", len(list))
	}
}
