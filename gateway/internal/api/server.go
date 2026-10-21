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
	"sync/atomic"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/config"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/events"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/secrets"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/skillsrc"
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

	// RunWorker runs a one-off worker command (e.g. "models --json") with the given
	// environment and decodes the JSON object it prints last into out. Nil when no
	// worker is available.
	RunWorker func(ctx context.Context, args, env []string, out any) error

	// SkillSource reads skills from GitHub repositories (skills.sh links). Tests replace it.
	SkillSource *skillsrc.Client

	// WorkspaceDir is where employees save files, one folder per assignment.
	WorkspaceDir string

	// revision counts configuration changes (agents, MCP servers, skills,
	// plugins); the worker long-polls it to reload without restarting.
	revision atomic.Int64

	breakerMu      sync.Mutex
	breakerOpen    bool
	breakerResetAt time.Time
	LongPollWait   time.Duration
}

func New(cfg *config.Config, st *store.Store, hub *events.Hub, box *secrets.Box, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, hub: hub, secrets: box, log: log, LongPollWait: 25 * time.Second, SkillSource: skillsrc.New()}
	s.revision.Store(time.Now().UnixMilli())
	if err := s.ensureOffice(context.Background()); err != nil {
		log.Error("could not set up the starting team", "err", err)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	owner := http.NewServeMux()
	owner.HandleFunc("GET /api/v1/status", s.handleStatus)
	owner.HandleFunc("GET /api/v1/catalog", s.handleCatalog)
	owner.HandleFunc("GET /api/v1/agents", s.handleListAgents)
	owner.HandleFunc("POST /api/v1/agents", s.handleCreateAgent)
	owner.HandleFunc("GET /api/v1/agents/{id}", s.handleGetAgent)
	owner.HandleFunc("PUT /api/v1/agents/{id}", s.handleUpdateAgent)
	owner.HandleFunc("DELETE /api/v1/agents/{id}", s.handleDeleteAgent)
	owner.HandleFunc("POST /api/v1/agents/{id}/main", s.handleSetMain)
	owner.HandleFunc("GET /api/v1/mcp-servers", s.handleListMCP)
	owner.HandleFunc("POST /api/v1/mcp-servers", s.handleCreateMCP)
	owner.HandleFunc("PUT /api/v1/mcp-servers/{id}", s.handleUpdateMCP)
	owner.HandleFunc("DELETE /api/v1/mcp-servers/{id}", s.handleDeleteMCP)
	owner.HandleFunc("POST /api/v1/mcp-servers/{id}/test", s.handleTestMCP)
	owner.HandleFunc("GET /api/v1/skills", s.handleListSkills)
	owner.HandleFunc("POST /api/v1/skills", s.handleCreateSkill)
	owner.HandleFunc("GET /api/v1/skills/{id}", s.handleGetSkill)
	owner.HandleFunc("PUT /api/v1/skills/{id}", s.handleUpdateSkill)
	owner.HandleFunc("POST /api/v1/skills/discover", s.handleDiscoverSkills)
	owner.HandleFunc("POST /api/v1/integrations/{id}/connect", s.handleConnectIntegration)
	owner.HandleFunc("POST /api/v1/skills/install", s.handleInstallSkills)
	owner.HandleFunc("DELETE /api/v1/skills/{id}", s.handleDeleteSkill)
	owner.HandleFunc("GET /api/v1/plugins", s.handleListPlugins)
	owner.HandleFunc("POST /api/v1/plugins", s.handleInstallPlugin)
	owner.HandleFunc("DELETE /api/v1/plugins/{id}", s.handleUninstallPlugin)
	owner.HandleFunc("GET /api/v1/conversations", s.handleListConversations)
	owner.HandleFunc("POST /api/v1/conversations", s.handleCreateConversation)
	owner.HandleFunc("GET /api/v1/conversations/{id}", s.handleGetConversation)
	owner.HandleFunc("POST /api/v1/conversations/{id}/messages", s.handleSendMessage)
	owner.HandleFunc("DELETE /api/v1/conversations/{id}", s.handleDeleteConversation)
	owner.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	owner.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)
	owner.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleCancelTask)
	owner.HandleFunc("POST /api/v1/assignments", s.handleCreateAssignment)
	owner.HandleFunc("GET /api/v1/inbox", s.handleListInbox)
	owner.HandleFunc("POST /api/v1/inbox/{id}/answer", s.handleAnswer)
	owner.HandleFunc("GET /api/v1/routines", s.handleListRoutines)
	owner.HandleFunc("POST /api/v1/routines", s.handleCreateRoutine)
	owner.HandleFunc("PUT /api/v1/routines/{id}", s.handleUpdateRoutine)
	owner.HandleFunc("POST /api/v1/routines/{id}/run", s.handleRunRoutine)
	owner.HandleFunc("GET /api/v1/routines/{id}/runs", s.handleRoutineRuns)
	owner.HandleFunc("POST /api/v1/routines/{id}/enable", s.handleRoutineEnable(true))
	owner.HandleFunc("POST /api/v1/routines/{id}/disable", s.handleRoutineEnable(false))
	owner.HandleFunc("DELETE /api/v1/routines/{id}", s.handleDeleteRoutine)
	owner.HandleFunc("GET /api/v1/activity", s.handleListActivity)
	owner.HandleFunc("GET /api/v1/steps", s.handleListSteps)
	owner.HandleFunc("GET /api/v1/secrets", s.handleListSecrets)
	owner.HandleFunc("PUT /api/v1/secrets/{name}", s.handlePutSecret)
	owner.HandleFunc("DELETE /api/v1/secrets/{name}", s.handleDeleteSecret)
	owner.HandleFunc("POST /api/v1/breaker/reset", s.handleBreakerReset)
	owner.HandleFunc("GET /api/v1/events", s.handleEvents)
	owner.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	owner.HandleFunc("PUT /api/v1/settings", s.handlePutSettings)
	owner.HandleFunc("GET /api/v1/providers", s.handleListProviders)
	owner.HandleFunc("GET /api/v1/providers/{id}/models", s.handleListModels)
	owner.HandleFunc("GET /api/v1/onboarding", s.handleGetOnboarding)
	owner.HandleFunc("POST /api/v1/onboarding", s.handleOnboard)
	owner.HandleFunc("GET /api/v1/profile", s.handleGetProfile)
	owner.HandleFunc("POST /api/v1/persona", s.handleCreatePersona)
	owner.HandleFunc("GET /api/v1/persona/{id}", s.handleGetPersona)
	owner.HandleFunc("GET /api/v1/memories", s.handleListMemories)
	owner.HandleFunc("POST /api/v1/memories", s.handleOwnerAddMemory)
	owner.HandleFunc("DELETE /api/v1/memories/{id}", s.handleDeleteMemory)

	worker := http.NewServeMux()
	worker.HandleFunc("POST /internal/v1/worker/start", s.handleWorkerStart)
	worker.HandleFunc("GET /internal/v1/agents", s.handleWorkerAgents)
	worker.HandleFunc("PATCH /internal/v1/agents/{id}/status", s.handleAgentStatus)
	worker.HandleFunc("POST /internal/v1/assignments", s.handleWorkerAssignment)
	worker.HandleFunc("POST /internal/v1/tasks/{id}/progress", s.handleProgress)
	worker.HandleFunc("POST /internal/v1/tasks/{id}/steps", s.handleSaveStep)
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
	worker.HandleFunc("DELETE /internal/v1/memories/{id}", s.handleDeleteMemory)

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
		s.log.Error("store error", "err", logSafe(err.Error()))
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
	var lastPrune time.Time
	for {
		s.fireRoutines(ctx)
		if time.Since(lastPrune) > 24*time.Hour {
			lastPrune = time.Now()
			if n, err := s.store.Prune(ctx); err != nil {
				s.log.Warn("prune old history", "err", err)
			} else if n > 0 {
				s.log.Info("pruned old history", "rows", n)
			}
		}
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

// managerID is the main agent, which receives assignments and automations.
func (s *Server) managerID(ctx context.Context) string {
	if a, err := s.store.MainAgent(ctx); err == nil {
		return a.ID
	}
	return "manager"
}
