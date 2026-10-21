package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/api"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func TestWorkLogSummarisesTheDayAndStartsAReport(t *testing.T) {
	h := newHarness(t)

	// An assignment is planned and finished, a tool is used, tokens are spent, something is remembered.
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Plan the launch"}, 201, &asg)
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, &claim)
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/steps", workerTok, map[string]any{"id": "t1", "agent_id": "manager", "kind": "tool", "name": "Write", "status": "done"}, 204, nil)
	h.do("POST", "/internal/v1/usage", workerTok, map[string]any{"task_id": asg.ID, "employee_id": "manager", "input_tokens": 100, "output_tokens": 40}, 204, nil)
	h.do("POST", "/internal/v1/memories", workerTok, map[string]string{"content": "Sam launches on 3 March", "employee_id": "manager"}, 201, nil)
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/complete", workerTok, map[string]string{"result": "The launch plan is ready."}, 204, nil)

	// A chat, and a file saved today in a work folder.
	var conv store.Conversation
	h.do("POST", "/api/v1/conversations", adminTok, map[string]string{"agent_id": "writer", "title": "Newsletter ideas"}, 201, &conv)
	h.do("POST", "/api/v1/conversations/"+conv.ID+"/messages", adminTok, map[string]string{"content": "Ideas please"}, 201, nil)
	h.gw.WorkspaceDir = t.TempDir()
	folder := filepath.Join(h.gw.WorkspaceDir, asg.ID)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "plan.md"), []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}

	var log api.WorkLogResponse
	h.do("GET", "/api/v1/work-log?period=today", adminTok, nil, 200, &log)
	if len(log.Assignments) != 1 || log.Assignments[0].Status != "done" || log.Assignments[0].Result != "The launch plan is ready." {
		t.Fatalf("the finished assignment should be logged with its outcome: %+v", log.Assignments)
	}
	if len(log.Chats) != 1 || log.Chats[0].Title != "Newsletter ideas" || log.Chats[0].Messages != 1 {
		t.Fatalf("the chat should be logged: %+v", log.Chats)
	}
	var manager *store.LogAgent
	for i := range log.Agents {
		if log.Agents[i].ID == "manager" {
			manager = &log.Agents[i]
		}
	}
	if manager == nil || manager.ToolCalls != 1 || manager.InputTokens != 100 || manager.OutputTokens != 40 || manager.TasksDone < 1 {
		t.Fatalf("the main agent's work should be counted: %+v", log.Agents)
	}
	if len(log.Memories) != 1 || len(log.Files) != 1 || !strings.HasSuffix(log.Files[0], "plan.md") {
		t.Fatalf("memory and files should be logged: %+v %+v", log.Memories, log.Files)
	}
	for _, want := range []string{"# Work log:", "Plan the launch", "Outcome: The launch plan is ready.", "Newsletter ideas", "Sam launches on 3 March", "plan.md"} {
		if !strings.Contains(log.Markdown, want) {
			t.Fatalf("the written log should mention %q:\n%s", want, log.Markdown)
		}
	}

	// Yesterday had nothing; agents read the same log through the worker API.
	h.do("GET", "/api/v1/work-log?period=yesterday", adminTok, nil, 200, &log)
	if len(log.Assignments) != 0 || len(log.Chats) != 0 {
		t.Fatalf("yesterday should be empty: %+v", log)
	}
	h.do("GET", "/internal/v1/work-log?period=week", workerTok, nil, 200, &log)
	if len(log.Assignments) != 1 {
		t.Fatalf("the week includes today: %+v", log.Assignments)
	}
	h.do("GET", "/api/v1/work-log?period=forever", adminTok, nil, 400, nil)

	// A report is an assignment for the main agent with the log inside it.
	var report store.Task
	h.do("POST", "/api/v1/reports", adminTok, map[string]string{"period": "week"}, 201, &report)
	if !strings.HasPrefix(report.Title, "Weekly report: Week of ") || report.Assignee != "manager" ||
		!strings.Contains(report.Description, "Plan the launch") || !strings.Contains(report.Description, "Do not invent work") {
		t.Fatalf("unexpected report assignment: %+v", report)
	}
	h.do("POST", "/api/v1/reports", adminTok, map[string]string{"period": "decade"}, 400, nil)
}

func TestPeriodRanges(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 30, 0, 0, time.Local) // a Wednesday
	cases := map[string][2]string{
		"today":     {"2026-10-21", "2026-10-22"},
		"yesterday": {"2026-10-20", "2026-10-21"},
		"week":      {"2026-10-19", "2026-10-26"},
		"last_week": {"2026-10-12", "2026-10-19"},
		"month":     {"2026-10-01", "2026-11-01"},
	}
	for period, want := range cases {
		from, to, label, err := store.PeriodRange(now, period)
		if err != nil || from.Format("2006-01-02") != want[0] || to.Format("2006-01-02") != want[1] || label == "" {
			t.Fatalf("%s: got %v %v %q %v", period, from, to, label, err)
		}
	}
	// Sunday belongs to the week that began on the Monday before.
	sunday := time.Date(2026, 10, 25, 9, 0, 0, 0, time.Local)
	if from, _, _, _ := store.PeriodRange(sunday, "week"); from.Format("2006-01-02") != "2026-10-19" {
		t.Fatalf("a Sunday is the end of its week, got %v", from)
	}
}
