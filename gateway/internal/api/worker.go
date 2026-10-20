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

func (s *Server) handleSyncEmployees(w http.ResponseWriter, r *http.Request) {
	var list []store.Employee
	if !decode(w, r, &list) {
		return
	}
	if len(list) == 0 || len(list) > 50 {
		writeErr(w, http.StatusBadRequest, "roster must contain 1-50 employees")
		return
	}
	for _, e := range list {
		if err := checkLen("id", e.ID, 64, true); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.store.SyncEmployees(r.Context(), list); err != nil {
		s.storeErr(w, err)
		return
	}
	emps, _ := s.store.ListEmployees(r.Context())
	s.hub.Publish("employees.synced", emps)
	s.logActivity(r.Context(), "", "", "Your staff clocked in.")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEmployeeStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status        string `json:"status"`
		CurrentTaskID string `json:"current_task_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	switch in.Status {
	case "idle", "working", "waiting", "offline":
	default:
		writeErr(w, http.StatusBadRequest, "status must be idle, working, waiting or offline")
		return
	}
	e, err := s.store.SetEmployeeStatus(r.Context(), r.PathValue("id"), in.Status, in.CurrentTaskID)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("employee.updated", e)
	writeJSON(w, http.StatusOK, e)
}

// ClaimResponse bundles everything an employee needs to work on a task.
type ClaimResponse struct {
	Task              *store.Task        `json:"task"`
	Parent            *store.Task        `json:"parent,omitempty"`
	DependencyResults []*store.Task      `json:"dependency_results"`
	HumanAnswers      []*store.InboxItem `json:"human_answers"`
}

// handleClaim long-polls for up to 25s for a runnable task.
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EmployeeIDs []string `json:"employee_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.EmployeeIDs) == 0 || len(in.EmployeeIDs) > 50 {
		writeErr(w, http.StatusBadRequest, "employee_ids must contain 1-50 ids")
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
		t, err := s.store.ClaimTask(r.Context(), in.EmployeeIDs)
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
	resp := ClaimResponse{Task: t, DependencyResults: []*store.Task{}, HumanAnswers: []*store.InboxItem{}}
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
		if ok, _ := s.store.EmployeeExists(r.Context(), a); !ok {
			writeErr(w, http.StatusBadRequest, "unknown assignee: "+a)
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
	s.publishTasks(changed...)
	for _, t := range changed {
		if t.Kind == store.KindAssignment {
			s.hub.Publish("assignment.done", t)
			s.logActivity(r.Context(), "", t.ID, "Done: "+t.Title)
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
	s.publishTasks(changed...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EmployeeID string `json:"employee_id"`
		Question   string `json:"question"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("question", in.Question, 4000, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.Ask(r.Context(), r.PathValue("id"), in.EmployeeID, in.Question)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("inbox.new", item)
	if t, err := s.store.GetTask(r.Context(), item.TaskID); err == nil {
		s.publishTasks(t)
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
