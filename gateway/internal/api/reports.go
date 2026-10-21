package api

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// WorkLogResponse is the work log of a period together with the same text ready to read.
type WorkLogResponse struct {
	*store.WorkLog
	Markdown string `json:"markdown"`
}

// buildWorkLog gathers what the staff did in a period, including the files they saved in that time.
func (s *Server) buildWorkLog(r *http.Request, period string) (*WorkLogResponse, int, string) {
	from, to, label, err := store.PeriodRange(time.Now(), period)
	if err != nil {
		return nil, http.StatusBadRequest, err.Error()
	}
	log, err := s.store.WorkLog(r.Context(), period, from, to, label)
	if err != nil {
		s.log.Error("work log", "err", logSafe(err.Error()))
		return nil, http.StatusInternalServerError, "internal error"
	}
	log.Files = s.filesBetween(from, to, 60)
	return &WorkLogResponse{WorkLog: log, Markdown: log.Markdown()}, http.StatusOK, ""
}

// filesBetween lists files the agents saved in their work folders during the period, newest last.
func (s *Server) filesBetween(from, to time.Time, limit int) []string {
	root := s.workspaceDir()
	var found []struct {
		path string
		at   time.Time
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && !info.ModTime().Before(from) && info.ModTime().Before(to) {
			if rel, err := filepath.Rel(root, p); err == nil {
				found = append(found, struct {
					path string
					at   time.Time
				}{filepath.ToSlash(rel), info.ModTime()})
			}
		}
		return nil
	})
	sort.Slice(found, func(i, j int) bool { return found[i].at.Before(found[j].at) })
	out := []string{}
	for _, f := range found {
		if len(out) >= limit {
			break
		}
		out = append(out, f.path)
	}
	return out
}

// handleWorkLog serves the work log of a period: GET /api/v1/work-log?period=today|yesterday|week|last_week|month.
func (s *Server) handleWorkLog(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = store.PeriodToday
	}
	resp, status, msg := s.buildWorkLog(r, period)
	if resp == nil {
		writeErr(w, status, msg)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCreateReport starts an assignment for the main agent to write the daily or weekly report. The work log is
// handed over in the assignment, so the report rests on what really happened.
func (s *Server) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Period string `json:"period"`
	}
	if !decode(w, r, &in) {
		return
	}
	resp, status, msg := s.buildWorkLog(r, in.Period)
	if resp == nil {
		writeErr(w, status, msg)
		return
	}
	kind := map[string]string{store.PeriodToday: "Daily report", store.PeriodYesterday: "Daily report",
		store.PeriodWeek: "Weekly report", store.PeriodLastWeek: "Weekly report", store.PeriodMonth: "Monthly report"}[in.Period]
	title := kind + ": " + resp.Label
	body := resp.Markdown
	if len(body) > 15000 {
		body = body[:15000] + "\n…(cut)"
	}
	description := "Write the " + strings.ToLower(kind) + " for the owner from the work log below. Open with a short summary of the " +
		"period, then what was finished, what is still open or failed, what needs the owner (open questions and approvals), " +
		"and what the team learned about them. Group related work, name the files that were produced, and keep it honest and " +
		"concise. Use only what the log says; if the log is thin, say so. Do not invent work.\n\n---\n\n" + body
	t, err := s.store.CreateTask(r.Context(), store.NewTask{
		Kind: store.KindAssignment, Title: title, Description: description, Assignee: s.managerID(r.Context()),
	})
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.publishTasks(t)
	s.logActivity(r.Context(), "", t.ID, "New assignment: "+t.Title)
	writeJSON(w, http.StatusCreated, t)
}

// handleWorkerWorkLog gives agents the same log, for the work_log tool.
func (s *Server) handleWorkerWorkLog(w http.ResponseWriter, r *http.Request) {
	s.handleWorkLog(w, r)
}
