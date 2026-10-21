package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/secrets"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.TaskCounts(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	used, _ := s.store.TokensSince(r.Context(), s.startOfBudgetWindow())
	inbox, _ := s.store.ListInbox(r.Context(), "open")
	writeJSON(w, http.StatusOK, map[string]any{
		"version":            version.Version,
		"tasks":              counts,
		"open_questions":     len(inbox),
		"breaker_open":       s.BreakerOpen(),
		"tokens_today":       used,
		"daily_token_budget": s.dailyBudget(r.Context()),
	})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TaskFilter{Status: q.Get("status")}
	if q.Has("parent_id") {
		p := q.Get("parent_id")
		f.ParentID = &p
		f.ExcludeChat = p == "" // top-level work, not chat replies
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	list, err := s.store.ListTasks(r.Context(), f)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type TaskDetail struct {
	*store.Task
	Children []*store.Task      `json:"children"`
	Inbox    []*store.InboxItem `json:"inbox"`
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	children, err := s.store.ListTasks(r.Context(), store.TaskFilter{ParentID: &id, Limit: 500})
	if err != nil {
		s.storeErr(w, err)
		return
	}
	inbox, err := s.store.InboxForTask(r.Context(), id)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TaskDetail{Task: t, Children: children, Inbox: inbox})
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	changed, err := s.store.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.publishTasks(changed...)
	s.logActivity(r.Context(), "", r.PathValue("id"), "You cancelled a task.")
	writeJSON(w, http.StatusOK, changed)
}

func (s *Server) handleCreateAssignment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AgentID     string `json:"agent_id"` // empty = the main agent, who plans it with the team
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("title", in.Title, 300, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := checkLen("description", in.Description, 20000, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	assignee := s.managerID(r.Context())
	if in.AgentID != "" {
		if ok, _ := s.store.AgentIsActive(r.Context(), in.AgentID); !ok {
			writeErr(w, http.StatusBadRequest, "unknown or disabled agent")
			return
		}
		assignee = in.AgentID
	}
	t, err := s.store.CreateTask(r.Context(), store.NewTask{
		Kind: store.KindAssignment, Title: in.Title, Description: in.Description, Assignee: assignee,
	})
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.publishTasks(t)
	s.logActivity(r.Context(), "", t.ID, "New assignment: "+t.Title)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleListInbox(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListInbox(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answer   string `json:"answer"`   // the reply; for an approval, an optional note
		Decision string `json:"decision"` // approve or decline; required for an approval
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("answer", in.Answer, 20000, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cur, err := s.store.GetInboxItem(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	text := in.Answer
	if cur.Kind == store.InboxApproval {
		if in.Decision != store.DecisionApprove && in.Decision != store.DecisionDecline {
			writeErr(w, http.StatusBadRequest, "decision must be approve or decline")
			return
		}
		text = store.ApprovalAnswer(in.Decision, in.Answer)
	} else if strings.TrimSpace(in.Answer) == "" {
		writeErr(w, http.StatusBadRequest, "answer is required")
		return
	}
	item, err := s.store.Answer(r.Context(), cur.ID, text)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("inbox.answered", item)
	if t, err := s.store.GetTask(r.Context(), item.TaskID); err == nil {
		s.publishTasks(t)
		if t.ConversationID != "" {
			s.publishConversation(r, t.ConversationID)
		}
	}
	if item.Kind == store.InboxApproval {
		s.logActivity(r.Context(), item.EmployeeID, item.TaskID, "You answered an approval request: "+in.Decision+"d.")
	} else {
		s.logActivity(r.Context(), item.EmployeeID, item.TaskID, "You answered a question.")
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleListRoutines(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRoutines(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// decodeRoutine reads and validates an automation body.
func decodeRoutine(w http.ResponseWriter, r *http.Request) (store.RoutineInput, bool) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Schedule    string `json:"schedule"`
		AgentID     string `json:"agent_id"`
	}
	if !decode(w, r, &in) {
		return store.RoutineInput{}, false
	}
	for _, c := range []error{
		checkLen("title", in.Title, 300, true),
		checkLen("description", in.Description, 20000, false),
		checkLen("schedule", in.Schedule, 200, true),
		checkLen("agent_id", in.AgentID, 64, false),
	} {
		if c != nil {
			writeErr(w, http.StatusBadRequest, c.Error())
			return store.RoutineInput{}, false
		}
	}
	return store.RoutineInput{Title: in.Title, Description: in.Description, Schedule: in.Schedule, AgentID: in.AgentID}, true
}

func (s *Server) handleCreateRoutine(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeRoutine(w, r)
	if !ok {
		return
	}
	rt, err := s.store.CreateRoutine(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Publish("routine.updated", rt)
	s.logActivity(r.Context(), "", "", fmt.Sprintf("New automation %q (%s), first run %s.", rt.Title, rt.Schedule, rt.NextRunAt.Local().Format(time.RFC1123)))
	writeJSON(w, http.StatusCreated, rt)
}

func (s *Server) handleUpdateRoutine(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeRoutine(w, r)
	if !ok {
		return
	}
	rt, err := s.store.UpdateRoutine(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.agentErr(w, err)
		return
	}
	s.hub.Publish("routine.updated", rt)
	writeJSON(w, http.StatusOK, rt)
}

// handleRunRoutine runs an automation now, without changing its schedule.
func (s *Server) handleRunRoutine(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.RunRoutineNow(r.Context(), r.PathValue("id"), s.managerID(r.Context()))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.publishTasks(t)
	s.logActivity(r.Context(), "", t.ID, "Automation started now: "+t.Title)
	if rt, err := s.store.GetRoutine(r.Context(), r.PathValue("id")); err == nil {
		s.hub.Publish("routine.updated", rt)
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleRoutineRuns(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.GetRoutine(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.TasksForRoutine(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleRoutineEnable(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt, err := s.store.SetRoutineEnabled(r.Context(), r.PathValue("id"), enabled)
		if err != nil {
			s.agentErr(w, err)
			return
		}
		s.hub.Publish("routine.updated", rt)
		writeJSON(w, http.StatusOK, rt)
	}
}

func (s *Server) handleDeleteRoutine(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRoutine(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("routine.deleted", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var list []*store.Activity
	var err error
	if id := r.URL.Query().Get("task_id"); id != "" {
		list, err = s.store.ListTaskActivity(r.Context(), id, limit) // one assignment's trail, oldest first
	} else if id := r.URL.Query().Get("conversation_id"); id != "" {
		list, err = s.store.ListConversationActivity(r.Context(), id, limit) // every reply of one chat, oldest first
	} else {
		list, err = s.store.ListActivity(r.Context(), limit)
	}
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// Secrets are write-only through the API: names can be listed, values never returned.
func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	names, err := s.store.ListSecretNames(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) handlePutSecret(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !secrets.NamePattern.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "secret name must look like an environment variable, e.g. ANTHROPIC_API_KEY")
		return
	}
	var in struct {
		Value string `json:"value"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("value", in.Value, 8192, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sealed, err := s.secrets.Seal(name, []byte(in.Value))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encryption failed")
		return
	}
	if err := s.store.PutSecret(r.Context(), name, sealed); err != nil {
		s.storeErr(w, err)
		return
	}
	s.logActivity(r.Context(), "", "", "Secret "+name+" updated.")
	if s.OnConfigChanged != nil {
		s.OnConfigChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.DeleteSecret(r.Context(), name); err != nil {
		s.storeErr(w, err)
		return
	}
	if s.OnConfigChanged != nil {
		s.OnConfigChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBreakerReset(w http.ResponseWriter, r *http.Request) {
	s.breakerMu.Lock()
	s.breakerOpen = false
	s.breakerResetAt = time.Now()
	s.breakerMu.Unlock()
	s.hub.Publish("breaker.reset", nil)
	s.hub.Wake()
	s.logActivity(r.Context(), "", "", "You reset the circuit breaker. Work resumes.")
	w.WriteHeader(http.StatusNoContent)
}

// handleEvents streams office events as Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, unsub := s.hub.Subscribe()
	defer unsub()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
		}
		flusher.Flush()
	}
}

// secretEnv decrypts all secrets into KEY=VALUE pairs for the worker process.
func (s *Server) secretEnv(ctx context.Context) []string {
	names, err := s.store.ListSecretNames(ctx)
	if err != nil {
		return nil
	}
	var env []string
	for _, n := range names {
		sealed, err := s.store.GetSecret(ctx, n)
		if err != nil {
			continue
		}
		plain, err := s.secrets.OpenSealed(n, sealed)
		if err != nil {
			s.log.Warn("could not decrypt secret", "name", n)
			continue
		}
		env = append(env, n+"="+string(plain))
	}
	return env
}
