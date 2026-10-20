package api

import (
	"fmt"
	"net/http"
	"strconv"
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
		"daily_token_budget": s.cfg.DailyTokenBudget,
	})
}

func (s *Server) handleListEmployees(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListEmployees(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.TaskFilter{Status: q.Get("status")}
	if q.Has("parent_id") {
		p := q.Get("parent_id")
		f.ParentID = &p
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
	t, err := s.store.CreateTask(r.Context(), store.NewTask{
		Kind: store.KindAssignment, Title: in.Title, Description: in.Description, Assignee: s.managerID(r.Context()),
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
		Answer string `json:"answer"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("answer", in.Answer, 20000, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.Answer(r.Context(), r.PathValue("id"), in.Answer)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("inbox.answered", item)
	if t, err := s.store.GetTask(r.Context(), item.TaskID); err == nil {
		s.publishTasks(t)
	}
	s.logActivity(r.Context(), item.EmployeeID, item.TaskID, "You answered a question.")
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

func (s *Server) handleCreateRoutine(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Schedule    string `json:"schedule"`
	}
	if !decode(w, r, &in) {
		return
	}
	for _, c := range []error{
		checkLen("title", in.Title, 300, true),
		checkLen("description", in.Description, 20000, false),
		checkLen("schedule", in.Schedule, 64, true),
	} {
		if c != nil {
			writeErr(w, http.StatusBadRequest, c.Error())
			return
		}
	}
	rt, err := s.store.CreateRoutine(r.Context(), in.Title, in.Description, in.Schedule)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Publish("routine.updated", rt)
	s.logActivity(r.Context(), "", "", fmt.Sprintf("New routine %q (%s), first run %s.", rt.Title, rt.Schedule, rt.NextRunAt.Local().Format(time.RFC1123)))
	writeJSON(w, http.StatusCreated, rt)
}

func (s *Server) handleRoutineEnable(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rt, err := s.store.SetRoutineEnabled(r.Context(), r.PathValue("id"), enabled)
		if err != nil {
			s.storeErr(w, err)
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
	list, err := s.store.ListActivity(r.Context(), limit)
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
	if s.OnSecretsChanged != nil {
		s.OnSecretsChanged()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.DeleteSecret(r.Context(), name); err != nil {
		s.storeErr(w, err)
		return
	}
	if s.OnSecretsChanged != nil {
		s.OnSecretsChanged()
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

// SecretEnv decrypts all secrets into KEY=VALUE pairs for the worker process.
func (s *Server) SecretEnv() []string {
	ctx, cancel := contextWithTimeout()
	defer cancel()
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
