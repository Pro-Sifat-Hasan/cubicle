package test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// A database written by v0.2 (before agents, chat and automations targets)
// must open and keep its data.
func TestOpenMigratesOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE employees (id TEXT PRIMARY KEY, name TEXT NOT NULL, title TEXT NOT NULL, role TEXT NOT NULL,
	avatar TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', skills TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'offline', current_task_id TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL);
CREATE TABLE tasks (id TEXT PRIMARY KEY, parent_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '', assignee TEXT NOT NULL, status TEXT NOT NULL, result TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '', depends_on TEXT NOT NULL DEFAULT '[]', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE routines (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', schedule TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1, next_run_at INTEGER NOT NULL, last_run_at INTEGER NOT NULL DEFAULT 0,
	last_task_id TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL);
INSERT INTO tasks VALUES ('tsk_old', '', 'assignment', 'Old work', '', 'manager', 'done', 'ok', '', '[]', 1, 1);
INSERT INTO routines VALUES ('rtn_old', 'Brief', '', 'daily@09:00', 1, 9999999999999, 0, '', 1);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open older database: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	task, err := st.GetTask(ctx, "tsk_old")
	if err != nil || task.Result != "ok" {
		t.Fatalf("old task lost: %v %+v", err, task)
	}
	rt, err := st.GetRoutine(ctx, "rtn_old")
	if err != nil || rt.AgentID != "" {
		t.Fatalf("old routine lost: %v %+v", err, rt)
	}
	// Opening twice must be a no-op.
	st.Close()
	if st, err = store.Open(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st.Close()
}
