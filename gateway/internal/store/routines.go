package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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

// Routine is an automation: work an agent receives on a schedule ("every
// weekday at 09:00, triage my inbox and draft replies"). Sent to the main agent,
// it is planned with the team; sent to any other agent, that agent does it alone.
type Routine struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Schedule    string    `json:"schedule"`
	AgentID     string    `json:"agent_id"` // empty = the main agent
	Enabled     bool      `json:"enabled"`
	NextRunAt   time.Time `json:"next_run_at"`
	LastRunAt   time.Time `json:"last_run_at,omitempty"`
	LastTaskID  string    `json:"last_task_id,omitempty"`
	LastStatus  string    `json:"last_status,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// RoutineInput is what the owner can set on an automation.
type RoutineInput struct {
	Title       string
	Description string
	Schedule    string
	AgentID     string
}

// Schedule formats (local time):
//
//	daily@HH:MM               every day at HH:MM
//	weekdays@HH:MM            Monday-Friday at HH:MM
//	weekly@mon,thu@HH:MM      on the listed days at HH:MM
//	every:<duration>          fixed interval, minimum 5m (e.g. every:30m, every:2h)
//	once@YYYY-MM-DDTHH:MM     a single run; the automation turns itself off afterwards
type Schedule struct {
	kind     string // daily | weekdays | weekly | every | once
	hour     int
	minute   int
	days     []time.Weekday
	interval time.Duration
	at       time.Time // once: wall-clock date and time
}

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
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
	if rest, ok := strings.CutPrefix(s, "once@"); ok {
		at, err := time.Parse("2006-01-02t15:04", rest)
		if err != nil {
			return Schedule{}, fmt.Errorf("invalid date %q (use once@YYYY-MM-DDTHH:MM)", rest)
		}
		return Schedule{kind: "once", at: at}, nil
	}
	kind, rest, ok := strings.Cut(s, "@")
	var days []time.Weekday
	if ok && kind == "weekly" {
		var list string
		list, rest, ok = strings.Cut(rest, "@")
		for _, d := range strings.Split(list, ",") {
			wd, known := weekdayNames[strings.TrimSpace(d)]
			if !known {
				return Schedule{}, fmt.Errorf("unknown day %q (use mon, tue, wed, thu, fri, sat, sun)", d)
			}
			if !slices.Contains(days, wd) {
				days = append(days, wd)
			}
		}
	}
	if !ok || (kind != "daily" && kind != "weekdays" && kind != "weekly") {
		return Schedule{}, fmt.Errorf("unknown schedule %q (use daily@HH:MM, weekdays@HH:MM, weekly@mon,fri@HH:MM, every:30m or once@YYYY-MM-DDTHH:MM)", s)
	}
	hh, mm, ok := strings.Cut(rest, ":")
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if !ok || err1 != nil || err2 != nil || len(mm) != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return Schedule{}, fmt.Errorf("invalid time %q (use HH:MM, 24h)", rest)
	}
	return Schedule{kind: kind, hour: h, minute: m, days: days}, nil
}

// Next returns the first run time strictly after `after`, or the zero time when
// the schedule will never run again (a past one-off).
func (sc Schedule) Next(after time.Time) time.Time {
	switch sc.kind {
	case "every":
		return after.Add(sc.interval)
	case "once":
		t := time.Date(sc.at.Year(), sc.at.Month(), sc.at.Day(), sc.at.Hour(), sc.at.Minute(), 0, 0, after.Location())
		if t.After(after) {
			return t
		}
		return time.Time{}
	}
	t := time.Date(after.Year(), after.Month(), after.Day(), sc.hour, sc.minute, 0, 0, after.Location())
	for !t.After(after) || !sc.runsOn(t.Weekday()) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func (sc Schedule) runsOn(d time.Weekday) bool {
	switch sc.kind {
	case "weekdays":
		return d != time.Saturday && d != time.Sunday
	case "weekly":
		return slices.Contains(sc.days, d)
	}
	return true
}

const routineCols = `id, title, description, schedule, agent_id, enabled, next_run_at, last_run_at, last_task_id, created_at,
	COALESCE((SELECT status FROM tasks WHERE tasks.id = routines.last_task_id), '')`

func scanRoutine(r scanner) (*Routine, error) {
	var rt Routine
	var enabled int
	var next, last, created int64
	if err := r.Scan(&rt.ID, &rt.Title, &rt.Description, &rt.Schedule, &rt.AgentID, &enabled, &next, &last,
		&rt.LastTaskID, &created, &rt.LastStatus); err != nil {
		return nil, err
	}
	rt.Enabled = enabled == 1
	rt.NextRunAt, rt.LastRunAt, rt.CreatedAt = fromMS(next), fromMS(last), fromMS(created)
	return &rt, nil
}

func (s *Store) validRoutine(ctx context.Context, in RoutineInput) (Schedule, error) {
	sc, err := ParseSchedule(in.Schedule)
	if err != nil {
		return sc, err
	}
	if sc.kind == "once" && sc.Next(s.now()).IsZero() {
		return sc, errors.New("that date and time is already in the past")
	}
	if in.AgentID != "" {
		if ok, err := s.AgentIsActive(ctx, in.AgentID); err != nil {
			return sc, err
		} else if !ok {
			return sc, errors.New("unknown or disabled agent")
		}
	}
	return sc, nil
}

func (s *Store) CreateRoutine(ctx context.Context, in RoutineInput) (*Routine, error) {
	sc, err := s.validRoutine(ctx, in)
	if err != nil {
		return nil, err
	}
	now := s.now()
	id := newID("rtn")
	_, err = s.db.ExecContext(ctx, `INSERT INTO routines (id, title, description, schedule, agent_id, enabled, next_run_at,
		last_run_at, last_task_id, created_at) VALUES (?,?,?,?,?,1,?,0,'',?)`,
		id, in.Title, in.Description, strings.TrimSpace(in.Schedule), in.AgentID, sc.Next(now).UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	return s.GetRoutine(ctx, id)
}

// UpdateRoutine edits an automation. A changed schedule restarts its clock.
func (s *Store) UpdateRoutine(ctx context.Context, id string, in RoutineInput) (*Routine, error) {
	sc, err := s.validRoutine(ctx, in)
	if err != nil {
		return nil, err
	}
	cur, err := s.GetRoutine(ctx, id)
	if err != nil {
		return nil, err
	}
	next := cur.NextRunAt
	if cur.Schedule != strings.TrimSpace(in.Schedule) || next.IsZero() {
		next = sc.Next(s.now())
	}
	_, err = s.db.ExecContext(ctx, `UPDATE routines SET title=?, description=?, schedule=?, agent_id=?, next_run_at=? WHERE id = ?`,
		in.Title, in.Description, strings.TrimSpace(in.Schedule), in.AgentID, next.UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	return s.GetRoutine(ctx, id)
}

func (s *Store) GetRoutine(ctx context.Context, id string) (*Routine, error) {
	rt, err := scanRoutine(s.db.QueryRowContext(ctx, `SELECT `+routineCols+` FROM routines WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
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
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rt, err := scanRoutine(tx.QueryRowContext(ctx, `SELECT `+routineCols+` FROM routines WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		sc, err := ParseSchedule(rt.Schedule)
		if err != nil {
			return err
		}
		next := rt.NextRunAt
		if enabled { // don't fire a backlog of missed runs on resume
			if next = sc.Next(s.now()); next.IsZero() {
				return errors.New("this one-off automation's time has passed; edit it to pick a new time")
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE routines SET enabled = ?, next_run_at = ? WHERE id = ?`,
			boolInt(enabled), next.UnixMilli(), id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetRoutine(ctx, id)
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

// routineTask creates the assignment for one run of a routine. Work for an
// agent that was removed or turned off goes to the main agent instead.
func routineTask(ctx context.Context, tx *sql.Tx, now time.Time, rt *Routine, mainID string) (*Task, error) {
	assignee := mainID
	if rt.AgentID != "" {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE id = ? AND enabled = 1`, rt.AgentID).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			assignee = rt.AgentID
		}
	}
	t, err := insertTask(ctx, tx, now.UnixMilli(), NewTask{
		Kind: KindAssignment, Assignee: assignee, RoutineID: rt.ID,
		Title:       fmt.Sprintf("%s (%s)", rt.Title, now.Format("Mon 02 Jan 15:04")),
		Description: rt.Description,
	})
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE routines SET last_run_at = ?, last_task_id = ? WHERE id = ?`, now.UnixMilli(), t.ID, rt.ID)
	return t, err
}

// RunRoutineNow starts a run immediately without changing the schedule.
func (s *Store) RunRoutineNow(ctx context.Context, id, mainID string) (*Task, error) {
	var t *Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rt, err := scanRoutine(tx.QueryRowContext(ctx, `SELECT `+routineCols+` FROM routines WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		t, err = routineTask(ctx, tx, s.now(), rt, mainID)
		return err
	})
	return t, err
}

// FireDueRoutines creates the work for every enabled routine whose next run is
// due, and schedules its following run. Missed runs (e.g. the PC was asleep)
// collapse into a single run rather than a burst. One-off automations turn off.
func (s *Store) FireDueRoutines(ctx context.Context, mainID string) ([]*Task, error) {
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
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}

		for _, rt := range due {
			sc, err := ParseSchedule(rt.Schedule)
			if err != nil {
				continue
			}
			t, err := routineTask(ctx, tx, now, rt, mainID)
			if err != nil {
				return err
			}
			created = append(created, t)
			next, enabled := sc.Next(now), 1
			if next.IsZero() {
				next, enabled = rt.NextRunAt, 0
			}
			if _, err := tx.ExecContext(ctx, `UPDATE routines SET next_run_at = ?, enabled = ? WHERE id = ?`,
				next.UnixMilli(), enabled, rt.ID); err != nil {
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
