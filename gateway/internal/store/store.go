// Package store persists the office state (employees, tasks, inbox, activity,
// usage, encrypted secrets) in a single SQLite file. It uses a pure-Go driver,
// so the gateway builds without cgo and ships as one static binary.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("task is not in a state that allows this action")
)

// Task statuses.
const (
	StatusQueued       = "queued"
	StatusInProgress   = "in_progress"
	StatusDelegated    = "delegated"
	StatusWaitingHuman = "waiting_human"
	StatusDone         = "done"
	StatusFailed       = "failed"
	StatusCancelled    = "cancelled"
)

// Task kinds.
const (
	KindAssignment = "assignment" // top-level request from a human, handled by the manager
	KindSubtask    = "subtask"    // delegated piece of work for a specialist
	KindSynthesis  = "synthesis"  // manager's final roll-up once all subtasks are done
)

const schema = `
CREATE TABLE IF NOT EXISTS employees (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	title TEXT NOT NULL,
	role TEXT NOT NULL,
	avatar TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	skills TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'offline',
	current_task_id TEXT NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	parent_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	assignee TEXT NOT NULL,
	status TEXT NOT NULL,
	result TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	depends_on TEXT NOT NULL DEFAULT '[]',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status, assignee);
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_id);
CREATE TABLE IF NOT EXISTS inbox (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	employee_id TEXT NOT NULL,
	question TEXT NOT NULL,
	answer TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'open',
	created_at INTEGER NOT NULL,
	answered_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS activity (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	employee_id TEXT NOT NULL DEFAULT '',
	task_id TEXT NOT NULL DEFAULT '',
	message TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS usage (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id TEXT NOT NULL DEFAULT '',
	employee_id TEXT NOT NULL DEFAULT '',
	input_tokens INTEGER NOT NULL,
	output_tokens INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_time ON usage(created_at);
CREATE TABLE IF NOT EXISTS secrets (
	name TEXT PRIMARY KEY,
	ciphertext BLOB NOT NULL,
	updated_at INTEGER NOT NULL
);
`

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serialises writes and removes SQLITE_BUSY races; the
	// workload (a handful of agents) is far below what this can sustain.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema + routinesSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SetClock overrides the time source (used by tests to simulate schedules).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) ms() int64 { return s.now().UnixMilli() }

func newID(prefix string) string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
