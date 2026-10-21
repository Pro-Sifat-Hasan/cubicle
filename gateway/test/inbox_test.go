package test

import (
	"strings"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/api"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// startChatReply sends a chat message and has the worker claim the reply task.
func startChatReply(h *harness, agent string) (conversationID string, claim api.ClaimResponse) {
	var conv store.Conversation
	h.do("POST", "/api/v1/conversations", adminTok, map[string]string{"agent_id": agent}, 201, &conv)
	h.do("POST", "/api/v1/conversations/"+conv.ID+"/messages", adminTok, map[string]string{"content": "Please email the report"}, 201, nil)
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{agent}, "lane": "chat"}, 200, &claim)
	return conv.ID, claim
}

func TestApprovalInAChatPausesTheReplyAndResumesWithTheDecision(t *testing.T) {
	h := newHarness(t)
	convID, claim := startChatReply(h, "manager")

	var item store.InboxItem
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/ask", workerTok, map[string]string{
		"employee_id": "manager", "kind": "approval", "question": "Send the report to bob@example.com?"}, 201, &item)
	if item.Kind != store.InboxApproval || item.ConversationID != convID || item.TaskTitle != "" {
		t.Fatalf("approval should point at its chat: %+v", item)
	}

	var conv store.Conversation
	h.do("GET", "/api/v1/conversations/"+convID, adminTok, nil, 200, &conv)
	if !conv.Waiting || conv.Busy {
		t.Fatalf("chat should be waiting for the owner, not busy: %+v", conv)
	}

	// An approval needs a decision; a plain answer is rejected.
	h.do("POST", "/api/v1/inbox/"+item.ID+"/answer", adminTok, map[string]string{"answer": "yes"}, 400, nil)
	h.do("POST", "/api/v1/inbox/"+item.ID+"/answer", adminTok, map[string]string{"decision": "approve", "answer": "Use the short version"}, 200, &item)
	if !strings.HasPrefix(item.Answer, "Approved.") || !strings.Contains(item.Answer, "Use the short version") {
		t.Fatalf("unexpected answer text %q", item.Answer)
	}

	// The reply task resumes with the decision in front of the agent.
	var again api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}, "lane": "chat"}, 200, &again)
	if again.Task.ID != claim.Task.ID || len(again.HumanAnswers) != 1 || again.HumanAnswers[0].Kind != store.InboxApproval {
		t.Fatalf("expected the same reply task with the decision, got %+v", again)
	}
}

func TestQuestionsNeedAnAnswerAndDeclinesAreExplicit(t *testing.T) {
	h := newHarness(t)
	var asg store.Task
	h.do("POST", "/api/v1/assignments", adminTok, map[string]string{"title": "Plan the launch"}, 201, &asg)
	var claim api.ClaimResponse
	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, &claim)

	var q, a store.InboxItem
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/ask", workerTok, map[string]string{"employee_id": "manager", "question": "Which date?"}, 201, &q)
	if q.Kind != store.InboxQuestion || q.TaskTitle != "Plan the launch" || q.ConversationID != "" {
		t.Fatalf("a question from an assignment should name it: %+v", q)
	}
	h.do("POST", "/api/v1/inbox/"+q.ID+"/answer", adminTok, map[string]string{"answer": "  "}, 400, nil)
	h.do("POST", "/api/v1/inbox/"+q.ID+"/answer", adminTok, map[string]string{"answer": "1 March"}, 200, nil)

	h.do("POST", "/internal/v1/tasks/claim", workerTok, map[string]any{"agent_ids": []string{"manager"}}, 200, &claim)
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/ask", workerTok, map[string]string{
		"employee_id": "manager", "kind": "approval", "question": "Delete the old draft?"}, 201, &a)
	h.do("POST", "/api/v1/inbox/"+a.ID+"/answer", adminTok, map[string]string{"decision": "maybe"}, 400, nil)
	h.do("POST", "/api/v1/inbox/"+a.ID+"/answer", adminTok, map[string]string{"decision": "decline"}, 200, &a)
	if !strings.HasPrefix(a.Answer, "Declined.") {
		t.Fatalf("decline should be unmistakable, got %q", a.Answer)
	}
	h.do("POST", "/internal/v1/tasks/"+asg.ID+"/ask", workerTok, map[string]string{"employee_id": "manager", "kind": "bogus", "question": "x"}, 400, nil)
}

func TestAssignmentsStartedFromAChatAreShownWithTheChat(t *testing.T) {
	h := newHarness(t)
	convID, claim := startChatReply(h, "manager")
	var started store.Task
	h.do("POST", "/internal/v1/assignments", workerTok, map[string]string{
		"title": "Competitor research", "agent_id": "manager", "source_task_id": claim.Task.ID}, 201, &started)

	var detail struct {
		Started []store.StartedAssignment `json:"started"`
	}
	h.do("GET", "/api/v1/conversations/"+convID, adminTok, nil, 200, &detail)
	if len(detail.Started) != 1 || detail.Started[0].SourceTaskID != claim.Task.ID || detail.Started[0].Assignment.ID != started.ID {
		t.Fatalf("chat should list the assignment it started: %+v", detail.Started)
	}

	var trail []store.Activity
	h.do("GET", "/api/v1/activity?task_id="+claim.Task.ID, adminTok, nil, 200, &trail)
	found := false
	for _, a := range trail {
		found = found || strings.Contains(a.Message, "Started the assignment")
	}
	if !found {
		t.Fatalf("the reply's trail should mention the assignment: %+v", trail)
	}
}

func TestDeletingAChatClosesItsOpenRequests(t *testing.T) {
	h := newHarness(t)
	convID, claim := startChatReply(h, "manager")
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/ask", workerTok, map[string]string{"employee_id": "manager", "question": "Which one?"}, 201, nil)
	h.do("DELETE", "/api/v1/conversations/"+convID, adminTok, nil, 204, nil)

	var open []store.InboxItem
	h.do("GET", "/api/v1/inbox?status=open", adminTok, nil, 200, &open)
	if len(open) != 0 {
		t.Fatalf("requests of a deleted chat must not linger: %+v", open)
	}
}

func TestChatActivityGroupsByReplyTask(t *testing.T) {
	h := newHarness(t)
	convID, claim := startChatReply(h, "manager")
	h.do("POST", "/internal/v1/activity", workerTok, map[string]string{"employee_id": "manager", "task_id": claim.Task.ID, "message": "Tool: Searching the web…"}, 204, nil)
	h.do("POST", "/internal/v1/activity", workerTok, map[string]string{"employee_id": "manager", "task_id": "tsk_other", "message": "Elsewhere"}, 204, nil)
	var got []store.Activity
	h.do("GET", "/api/v1/activity?conversation_id="+convID, adminTok, nil, 200, &got)
	if len(got) != 1 || got[0].TaskID != claim.Task.ID {
		t.Fatalf("want only this chat's reply trail, got %+v", got)
	}
}
