package test

import (
	"strings"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func TestStepsAreStoredUpdatedAndListedWithTheirChat(t *testing.T) {
	h := newHarness(t)
	convID, claim := startChatReply(h, "manager")
	path := "/internal/v1/tasks/" + claim.Task.ID + "/steps"

	h.do("POST", path, workerTok, map[string]any{"id": "s1", "agent_id": "manager", "kind": "thinking", "output": "Planning the page"}, 204, nil)
	h.do("POST", path, workerTok, map[string]any{"id": "s2", "agent_id": "engineer", "kind": "tool", "name": "Write",
		"input": `{"file_path":"index.html"}`, "status": "running"}, 204, nil)
	// The same id again: the call finished, so it gains its output instead of adding a row.
	h.do("POST", path, workerTok, map[string]any{"id": "s2", "agent_id": "engineer", "kind": "tool", "name": "Write",
		"input": `{"file_path":"index.html"}`, "output": "File created", "status": "done"}, 204, nil)

	var steps []store.Step
	h.do("GET", "/api/v1/steps?conversation_id="+convID, adminTok, nil, 200, &steps)
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].ID != "s1" || steps[1].ID != "s2" || steps[1].Output != "File created" || steps[1].Status != "done" ||
		steps[1].AgentID != "engineer" || steps[1].ConversationID != convID {
		t.Fatalf("unexpected steps: %+v", steps)
	}

	h.do("GET", "/api/v1/steps?task_id="+claim.Task.ID, adminTok, nil, 200, &steps)
	if len(steps) != 2 {
		t.Fatalf("task listing should match, got %d", len(steps))
	}
}

func TestStepsAreValidatedTruncatedAndFinishedWithTheTask(t *testing.T) {
	h := newHarness(t)
	_, claim := startChatReply(h, "manager")
	path := "/internal/v1/tasks/" + claim.Task.ID + "/steps"

	h.do("POST", path, workerTok, map[string]any{"id": "a", "kind": "dance"}, 400, nil)
	h.do("POST", path, workerTok, map[string]any{"id": "", "kind": "tool"}, 400, nil)
	h.do("POST", path, workerTok, map[string]any{"id": "a", "kind": "tool", "status": "later"}, 400, nil)
	h.do("POST", "/internal/v1/tasks/nope/steps", workerTok, map[string]any{"id": "a", "kind": "tool"}, 404, nil)
	h.do("POST", path, adminTok, map[string]any{"id": "a", "kind": "tool"}, 401, nil)
	h.do("GET", "/api/v1/steps", adminTok, nil, 400, nil)

	h.do("POST", path, workerTok, map[string]any{"id": "big", "kind": "tool", "name": "run_command", "status": "running",
		"output": strings.Repeat("é", 20000)}, 204, nil)
	var steps []store.Step
	h.do("GET", "/api/v1/steps?task_id="+claim.Task.ID, adminTok, nil, 200, &steps)
	if len(steps) != 1 || len(steps[0].Output) > 12010 || !strings.HasSuffix(steps[0].Output, "…") {
		t.Fatalf("output should be cut to its limit: %d bytes", len(steps[0].Output))
	}

	// Finishing the reply closes calls that never reported back, so nothing spins forever.
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/complete", workerTok, map[string]string{"result": "done"}, 204, nil)
	h.do("GET", "/api/v1/steps?task_id="+claim.Task.ID, adminTok, nil, 200, &steps)
	if steps[0].Status != "done" {
		t.Fatalf("running step should be finished with its task, got %q", steps[0].Status)
	}
}

func TestStepsStopGrowingAtTheLimit(t *testing.T) {
	h := newHarness(t)
	_, claim := startChatReply(h, "manager")
	path := "/internal/v1/tasks/" + claim.Task.ID + "/steps"
	for i := 0; i < 405; i++ {
		h.do("POST", path, workerTok, map[string]any{"id": "s" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('A'+i/26)), "kind": "note", "output": "x"}, 204, nil)
	}
	var steps []store.Step
	h.do("GET", "/api/v1/steps?task_id="+claim.Task.ID, adminTok, nil, 200, &steps)
	if len(steps) > 400 {
		t.Fatalf("a task keeps at most 400 steps, got %d", len(steps))
	}
}
