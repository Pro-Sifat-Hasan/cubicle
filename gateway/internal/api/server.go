// Package api exposes two surfaces:
//
//	/api/v1/*      the owner's API (desktop app, CLI, scripts) â€” admin token
//	/internal/v1/* the agent worker's API (claim work, report results) â€” worker token
//
// The tokens are distinct so a compromised worker cannot read secrets it was not
// given, change routines, or impersonate the owner.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/config"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/events"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/secrets"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	hub     *events.Hub
	secrets *secrets.Box
	log     *slog.Logger

	// OnConfigChanged is invoked after secrets or settings change, so the
	// supervisor can restart the worker with the new configuration.
	OnConfigChanged func()

	// ListModels runs the worker in "models" mode with the given environment and
	// returns the model ids the provider offers. Nil when no worker is available.
	ListModels func(ctx context.Context, env []string) ([]string, error)

	// WorkspaceDir is where employees save files, one folder per assignment.
	WorkspaceDir string

	breakerMu      sync.Mutex
	breakerOpen    bool
	breakerResetAt time.Time
	LongPollWait   time.Duration
}

func New(cfg *config.Config, st *store.Store, hub *events.Hub, box *secrets.Box, log *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, hub: hub, secrets: box, log: log, LongPollWait: 25 * time.Second}
}

func (s *Server) Handler() http.Handler {
	owner := http.NewServeMux()
	owner.HandleFunc("GET /api/v1/status", s.handleStatus)
	owner.HandleFunc("GET /api/v1/employees", s.handleListEmployees)
	owner.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	owner.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)
	owner.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleCancelTask)
	owner.HandleFunc("POST /api/v1/assignments", s.handleCreateAssignment)
	owner.HandleFunc("GET /api/v1/inbox", s.handleListInbox)
	owner.HandleFunc("POST /api/v1/inbox/{id}/answer", s.handleAnswer)
	owner.HandleFunc("GET /api/v1/routines", s.handleListRoutines)
	owner.HandleFunc("POST /api/v1/routines", s.handleCreateRoutine)
	owner.HandleFunc("POST /api/v1/routines/{id}/enable", s.handleRoutineEnable(true))
	owner.HandleFunc("POST /api/v1/routines/{id}/disable", s.handleRoutineEnable(false))
	owner.HandleFunc("DELETE /api/v1/routines/{id}", s.handleDeleteRoutine)
	owner.HandleFunc("GET /api/v1/activity", s.handleListActivity)
	owner.HandleFunc("GET /api/v1/secrets", s.handleListSecrets)
	owner.HandleFunc("PUT /api/v1/secrets/{name}", s.handlePutSecret)
	owner.HandleFunc("DELETE /api/v1/secrets/{name}", s.handleDeleteSecret)
	owner.HandleFunc("POST /api/v1/breaker/reset", s.handleBreakerReset)
	owner.HandleFunc("GET /api/v1/events", s.handleEvents)
	owner.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	owner.HandleFunc("PUT /api/v1/settings", s.handlePutSettings)
	owner.HandleFunc("GET /api/v1/providers", s.handleListProviders)
	owner.HandleFunc("GET /api/v1/providers/{id}/models", s.handleListModels)
	owner.HandleFunc("GET /api/v1/memories", s.handleListMemories)
	owner.HandleFunc("POST /api/v1/memories", s.handleOwnerAddMemory)
	owner.HandleFunc("DELETE /api/v1/memories/{id}", s.handleDeleteMemory)

	worker := http.NewServeMux()
	worker.HandleFunc("PUT /internal/v1/employees", s.handleSyncEmployees)
	worker.HandleFunc("PATCH /internal/v1/employees/{id}", s.handleEmployeeStatus)
	worker.HandleFunc("POST /internal/v1/tasks/claim", s.handleClaim)
	worker.HandleFunc("GET /internal/v1/tasks/{id}", s.handleGetTask)
	worker.HandleFunc("POST /internal/v1/tasks/{id}/delegate", s.handleDelegate)
	worker.HandleFunc("POST /internal/v1/tasks/{id}/complete", s.handleComplete)
	worker.HandleFunc("POST /internal/v1/tasks/{id}/fail", s.handleFail)
	worker.HandleFunc("POST /internal/v1/tasks/{id}/ask", s.handleAsk)
	worker.HandleFunc("POST /internal/v1/usage", s.handleUsage)
	worker.HandleFunc("POST /internal/v1/activity", s.handleAddActivity)
	worker.HandleFunc("GET /internal/v1/memories", s.handleListMemories)
	worker.HandleFunc("POST /internal/v1/memories", s.handleWorkerAddMemory)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
	})
	root.Handle("/api/", requireToken(s.cfg.AdminToken, owner))
	root.Handle("/internal/", requireToken(s.cfg.WorkerToken, worker))

	rl := newRateLimiter(s.cfg.RateLimitPerMinute)
	return recoverer(s.log, securityHeaders(rl.middleware(root)))
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (s *Server) storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		s.log.Error("store error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) publishTasks(tasks ...*store.Task) {
	for _, t := range tasks {
		s.hub.Publish("task.updated", t)
	}
	s.hub.Wake()
}

func (s *Server) logActivity(ctx context.Context, employeeID, taskID, msg string) {
	a, err := s.store.AddActivity(ctx, employeeID, taskID, msg)
	if err == nil {
		s.hub.Publish("activity", a)
	}
}

func checkLen(field, v string, maxLen int, required bool) error {
	if required && v == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(v) > maxLen {
		return fmt.Errorf("%s exceeds %d characters", field, maxLen)
	}
	return nil
}

// ---- circuit breaker ----

func (s *Server) startOfBudgetWindow() time.Time {
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	if s.breakerResetAt.After(day) {
		return s.breakerResetAt
	}
	return day
}

func (s *Server) BreakerOpen() bool {
	s.breakerMu.Lock()
	defer s.breakerMu.Unlock()
	return s.breakerOpen
}

func (s *Server) checkBudget(ctx context.Context) {
	budget := s.dailyBudget(ctx)
	if budget <= 0 {
		return
	}
	used, err := s.store.TokensSince(ctx, s.startOfBudgetWindow())
	if err != nil {
		return
	}
	s.breakerMu.Lock()
	tripped := !s.breakerOpen && used >= budget
	if tripped {
		s.breakerOpen = true
	}
	s.breakerMu.Unlock()
	if tripped {
		s.log.Warn("circuit breaker tripped", "tokens_used", used, "budget", budget)
		s.hub.Publish("breaker.tripped", map[string]int64{"tokens_used": used, "budget": budget})
		s.logActivity(ctx, "", "", fmt.Sprintf("Circuit breaker tripped: %d tokens used today (budget %d). Work is paused.", used, budget))
	}
}

// ResetBreakerAtMidnight closes the breaker when a new budget day starts.
func (s *Server) RunBreakerClock(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	day := time.Now().YearDay()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if d := time.Now().YearDay(); d != day {
				day = d
				s.breakerMu.Lock()
				s.breakerOpen = false
				s.breakerMu.Unlock()
				s.hub.Publish("breaker.reset", nil)
				s.hub.Wake()
			}
		}
	}
}

// RunScheduler fires due routines every 30 seconds.
func (s *Server) RunScheduler(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		s.fireRoutines(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) fireRoutines(ctx context.Context) {
	tasks, err := s.store.FireDueRoutines(ctx, s.managerID(ctx))
	if err != nil {
		s.log.Error("fire routines", "err", err)
		return
	}
	for _, t := range tasks {
		s.logActivity(ctx, "", t.ID, "Routine started: "+t.Title)
	}
	if len(tasks) > 0 {
		s.publishTasks(tasks...)
	}
}

// managerID finds the employee with the manager role; the worker reports the
// roster, so this follows whatever the owner configured.
func (s *Server) managerID(ctx context.Context) string {
	emps, err := s.store.ListEmployees(ctx)
	if err == nil {
		for _, e := range emps {
			if e.Role == "manager" {
				return e.ID
			}
		}
	}
	return "manager"
}
