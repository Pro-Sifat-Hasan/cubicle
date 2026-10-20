package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const routinesSchema = `
CREATE TABLE IF NOT EXISTS routines (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	schedule TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	next_run_at INTEGER NOT NULL,
	last_run_at INTEGER NOT NULL DEFAULT 0,
	last_task_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
`

// Routine is a standing order: an assignment the manager receives on a schedule
// ("every weekday at 09:00, triage my inbox and draft replies").
type Routine struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Schedule    string    `json:"schedule"`
	Enabled     bool      `json:"enabled"`
	NextRunAt   time.Time `json:"next_run_at"`
	LastRunAt   time.Time `json:"last_run_at,omitempty"`
	LastTaskID  string    `json:"last_task_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Schedule formats (local time):
//
//	daily@HH:MM      every day at HH:MM
//	weekdays@HH:MM   Monday-Friday at HH:MM
//	every:<duration> fixed interval, minimum 5m (e.g. every:30m, every:2h)
type Schedule struct {
	kind     string // daily | weekdays | every
	hour     int
	minute   int
	interval time.Duration
}

func ParseSchedule(s string) (Schedule, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if rest, ok := strings.CutPrefix(s, "every:"); ok {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return Schedule{}, fmt.Errorf("invalid interval %q: %w", rest, err)
		}
		if d < 5*time.Minute {
			return Schedule{}, errors.New("interval must be at least 5m")
		}
		return Schedule{kind: "every", interval: d}, nil
	}
	kind, at, ok := strings.Cut(s, "@")
	if !ok || (kind != "daily" && kind != "weekdays") {
		return Schedule{}, fmt.Errorf("unknown schedule %q (use daily@HH:MM, weekdays@HH:MM or every:30m)", s)
	}
	hh, mm, ok := strings.Cut(at, ":")
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if !ok || err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return Schedule{}, fmt.Errorf("invalid time %q (use HH:MM, 24h)", at)
	}
	return Schedule{kind: kind, hour: h, minute: m}, nil
}

// Next returns the first run time strictly after `after`.
func (sc Schedule) Next(after time.Time) time.Time {
	if sc.kind == "every" {
		return after.Add(sc.interval)
	}
	t := time.Date(after.Year(), after.Month(), after.Day(), sc.hour, sc.minute, 0, 0, after.Location())
	for !t.After(after) || (sc.kind == "weekdays" && (t.Weekday() == time.Saturday || t.Weekday() == time.Sunday)) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

const routineCols = `id, title, description, schedule, enabled, next_run_at, last_run_at, last_task_id, created_at`

func scanRoutine(r scanner) (*Routine, error) {
	var rt Routine
	var enabled int
	var next, last, created int64
	if err := r.Scan(&rt.ID, &rt.Title, &rt.Description, &rt.Schedule, &enabled, &next, &last, &rt.LastTaskID, &created); err != nil {
		return nil, err
	}
	rt.Enabled = enabled == 1
	rt.NextRunAt, rt.LastRunAt, rt.CreatedAt = fromMS(next), fromMS(last), fromMS(created)
	return &rt, nil
}

func (s *Store) CreateRoutine(ctx context.Context, title, description, schedule string) (*Routine, error) {
	sc, err := ParseSchedule(schedule)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rt := &Routine{
		ID: newID("rtn"), Title: title, Description: description, Schedule: schedule,
		Enabled: true, NextRunAt: sc.Next(now).UTC(), CreatedAt: now.UTC(),
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO routines (`+routineCols+`) VALUES (?,?,?,?,1,?,0,'',?)`,
		rt.ID, title, description, schedule, rt.NextRunAt.UnixMilli(), now.UnixMilli())
	return rt, err
}

func (s *Store) ListRoutines(ctx context.Context) ([]*Routine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+routineCols+` FROM routines ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Routine{}
	for rows.Next() {
		rt, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rt)
	}
	return out, rows.Err()
}

func (s *Store) SetRoutineEnabled(ctx context.Context, id string, enabled bool) (*Routine, error) {
	var rt *Routine
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		rt, err = scanRoutine(tx.QueryRowContext(ctx, `SELECT `+routineCols+` FROM routines WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		sc, err := ParseSchedule(rt.Schedule)
		if err != nil {
			return err
		}
		rt.Enabled = enabled
		if enabled { // don't fire a backlog of missed runs on resume
			rt.NextRunAt = sc.Next(s.now()).UTC()
		}
		_, err = tx.ExecContext(ctx, `UPDATE routines SET enabled = ?, next_run_at = ? WHERE id = ?`,
			boolInt(enabled), rt.NextRunAt.UnixMilli(), id)
		return err
	})
	return rt, err
}

func (s *Store) DeleteRoutine(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM routines WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FireDueRoutines creates a manager assignment for every enabled routine whose
// next run is due, and schedules its following run. Missed runs (e.g. the PC
// was asleep) collapse into a single run rather than a burst.
func (s *Store) FireDueRoutines(ctx context.Context, managerID string) ([]*Task, error) {
	var created []*Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		rows, err := tx.QueryContext(ctx, `SELECT `+routineCols+` FROM routines WHERE enabled = 1 AND next_run_at <= ?`, now.UnixMilli())
		if err != nil {
			return err
		}
		var due []*Routine
		for rows.Next() {
			rt, err := scanRoutine(rows)
			if err != nil {
				rows.Close()
				return err
			}
			due = append(due, rt)
		}
		rows.Close()

		for _, rt := range due {
			sc, err := ParseSchedule(rt.Schedule)
			if err != nil {
				continue
			}
			t, err := insertTask(ctx, tx, now.UnixMilli(), NewTask{
				Kind: KindAssignment, Assignee: managerID,
				Title:       fmt.Sprintf("%s (%s)", rt.Title, now.Format("Mon 02 Jan 15:04")),
				Description: rt.Description,
			})
			if err != nil {
				return err
			}
			created = append(created, t)
			if _, err := tx.ExecContext(ctx, `UPDATE routines SET next_run_at = ?, last_run_at = ?, last_task_id = ? WHERE id = ?`,
				sc.Next(now).UnixMilli(), now.UnixMilli(), t.ID, rt.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return created, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
