package api

import (
	"context"
	"net/http"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// ClaimResponse bundles everything an employee needs to work on a task.
type ClaimResponse struct {
	Task              *store.Task        `json:"task"`
	Parent            *store.Task        `json:"parent,omitempty"`
	DependencyResults []*store.Task      `json:"dependency_results"`
	HumanAnswers      []*store.InboxItem `json:"human_answers"`
	// History is the conversation so far, for chat tasks (oldest first).
	History []*store.Message `json:"history"`
	// TitlePending is true while the chat has no title yet: the agent's model names it from the first message.
	TitlePending bool `json:"title_pending"`
}

// handleClaim long-polls for up to 25s for a runnable task.
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentIDs []string `json:"agent_ids"`
		Lane     string   `json:"lane"` // work | chat | empty for both
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.AgentIDs) == 0 || len(in.AgentIDs) > 50 {
		writeErr(w, http.StatusBadRequest, "agent_ids must contain 1-50 ids")
		return
	}
	if in.Lane != store.LaneAny && in.Lane != store.LaneWork && in.Lane != store.LaneChat {
		writeErr(w, http.StatusBadRequest, "lane must be work or chat")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.LongPollWait)
	defer cancel()
	for {
		if s.BreakerOpen() {
			writeErr(w, http.StatusServiceUnavailable, "circuit breaker open: daily token budget reached")
			return
		}
		wait := s.hub.WaitC() // grab before claiming so no wake-up is missed
		t, err := s.store.ClaimTask(r.Context(), in.AgentIDs, in.Lane)
		if err != nil {
			s.storeErr(w, err)
			return
		}
		if t != nil {
			s.hub.Publish("task.updated", t)
			writeJSON(w, http.StatusOK, s.buildClaim(r.Context(), t))
			return
		}
		select {
		case <-wait:
		case <-ctx.Done():
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
}

func (s *Server) buildClaim(ctx context.Context, t *store.Task) ClaimResponse {
	resp := ClaimResponse{Task: t, DependencyResults: []*store.Task{}, HumanAnswers: []*store.InboxItem{}, History: []*store.Message{}}
	if t.ConversationID != "" {
		resp.TitlePending = s.store.TitlePending(ctx, t.ConversationID)
		if msgs, err := s.store.Messages(ctx, t.ConversationID, 60); err == nil {
			for _, m := range msgs {
				if m.TaskID == t.ID && m.Role == store.MessageOwner {
					break // later messages are newer than this request
				}
				resp.History = append(resp.History, m)
			}
		}
	}
	if t.ParentID != "" {
		resp.Parent, _ = s.store.GetTask(ctx, t.ParentID)
	}
	if deps, err := s.store.GetTasks(ctx, t.DependsOn); err == nil {
		resp.DependencyResults = deps
	}
	if items, err := s.store.InboxForTask(ctx, t.ID); err == nil {
		for _, it := range items {
			if it.Status == "answered" {
				resp.HumanAnswers = append(resp.HumanAnswers, it)
			}
		}
	}
	return resp
}

func (s *Server) handleDelegate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Subtasks []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Assignee    string `json:"assignee"`
			DependsOn   []int  `json:"depends_on"`
		} `json:"subtasks"`
		Synthesis struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Assignee    string `json:"assignee"`
		} `json:"synthesis"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Subtasks) == 0 || len(in.Subtasks) > 20 {
		writeErr(w, http.StatusBadRequest, "plan must contain 1-20 subtasks")
		return
	}
	d := store.Delegation{Synthesis: store.NewTask{
		Title: in.Synthesis.Title, Description: in.Synthesis.Description, Assignee: in.Synthesis.Assignee,
	}}
	assignees := map[string]bool{in.Synthesis.Assignee: true}
	for _, st := range in.Subtasks {
		if err := checkLen("subtask title", st.Title, 300, true); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		assignees[st.Assignee] = true
		d.Subtasks = append(d.Subtasks, store.DelegatedSubtask{
			Title: st.Title, Description: st.Description, Assignee: st.Assignee, DependsOn: st.DependsOn,
		})
	}
	for a := range assignees {
		if ok, _ := s.store.AgentIsActive(r.Context(), a); !ok {
			writeErr(w, http.StatusBadRequest, "unknown or disabled assignee: "+a)
			return
		}
	}
	created, err := s.store.Delegate(r.Context(), r.PathValue("id"), d)
	if err != nil {
		if err == store.ErrNotFound || err == store.ErrConflict {
			s.storeErr(w, err)
		} else {
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if parent, err := s.store.GetTask(r.Context(), r.PathValue("id")); err == nil {
		created = append(created, parent)
	}
	s.publishTasks(created...)
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleComplete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Result string `json:"result"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("result", in.Result, 200000, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	changed, err := s.store.Complete(r.Context(), r.PathValue("id"), in.Result)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	_ = s.store.FinishSteps(r.Context(), r.PathValue("id"))
	s.publishTasks(changed...)
	for _, t := range changed {
		switch t.Kind {
		case store.KindAssignment:
			s.hub.Publish("assignment.done", t)
			s.notifyOwner("done", "Done: "+t.Title, "Your staff finished “"+t.Title+"”. Open Cubicle to read the result.")
			s.logActivity(r.Context(), "", t.ID, "Done: "+t.Title)
		case store.KindChat:
			s.publishConversation(r, t.ConversationID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Error string `json:"error"`
	}
	if !decode(w, r, &in) {
		return
	}
	changed, err := s.store.Fail(r.Context(), r.PathValue("id"), truncate(in.Error, 4000))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	_ = s.store.FinishSteps(r.Context(), r.PathValue("id"))
	s.publishTasks(changed...)
	for _, t := range changed {
		if t.Kind == store.KindChat {
			s.publishConversation(r, t.ConversationID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EmployeeID string `json:"employee_id"`
		Kind       string `json:"kind"` // question (default) or approval
		Question   string `json:"question"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("question", in.Question, 4000, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Kind != "" && in.Kind != store.InboxQuestion && in.Kind != store.InboxApproval {
		writeErr(w, http.StatusBadRequest, "kind must be question or approval")
		return
	}
	item, err := s.store.Ask(r.Context(), r.PathValue("id"), in.EmployeeID, in.Kind, in.Question)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if item, err = s.store.GetInboxItem(r.Context(), item.ID); err != nil {
		s.storeErr(w, err)
		return
	}
	_ = s.store.FinishSteps(r.Context(), item.TaskID)
	s.hub.Publish("inbox.new", item)
	title := "Your staff have a question"
	if item.Kind == store.InboxApproval {
		title = "Your staff need your approval"
	}
	s.notifyOwner("waiting", title+": open Cubicle to answer", item.Question)
	if t, err := s.store.GetTask(r.Context(), item.TaskID); err == nil {
		s.publishTasks(t)
		if t.ConversationID != "" {
			s.publishConversation(r, t.ConversationID) // the chat now shows "waiting for you"
		}
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TaskID       string `json:"task_id"`
		EmployeeID   string `json:"employee_id"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.InputTokens < 0 || in.OutputTokens < 0 {
		writeErr(w, http.StatusBadRequest, "token counts must be non-negative")
		return
	}
	if err := s.store.AddUsage(r.Context(), in.TaskID, in.EmployeeID, in.InputTokens, in.OutputTokens); err != nil {
		s.storeErr(w, err)
		return
	}
	s.checkBudget(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAddActivity(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EmployeeID string `json:"employee_id"`
		TaskID     string `json:"task_id"`
		Message    string `json:"message"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("message", in.Message, 2000, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logActivity(r.Context(), in.EmployeeID, in.TaskID, in.Message)
	w.WriteHeader(http.StatusNoContent)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "â€¦"
}
