package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ---- Inbox ("Ask me") ----

type InboxItem struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	EmployeeID string    `json:"employee_id"`
	Question   string    `json:"question"`
	Answer     string    `json:"answer,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	AnsweredAt time.Time `json:"answered_at,omitempty"`
}

// Ask parks an in-progress task until a human answers the question.
func (s *Store) Ask(ctx context.Context, taskID, employeeID, question string) (*InboxItem, error) {
	item := &InboxItem{ID: newID("ask"), TaskID: taskID, EmployeeID: employeeID, Question: question, Status: "open"}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.ms()
		res, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
			StatusWaitingHuman, now, taskID, StatusInProgress)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrConflict
		}
		item.CreatedAt = fromMS(now)
		_, err = tx.ExecContext(ctx, `INSERT INTO inbox (id, task_id, employee_id, question, status, created_at) VALUES (?,?,?,?,?,?)`,
			item.ID, taskID, employeeID, question, "open", now)
		return err
	})
	return item, err
}

// Answer records the human's reply and re-queues the task.
func (s *Store) Answer(ctx context.Context, id, answer string) (*InboxItem, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var taskID, status string
		err := tx.QueryRowContext(ctx, `SELECT task_id, status FROM inbox WHERE id = ?`, id).Scan(&taskID, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "open" {
			return ErrConflict
		}
		now := s.ms()
		if _, err := tx.ExecContext(ctx, `UPDATE inbox SET answer = ?, status = 'answered', answered_at = ? WHERE id = ?`, answer, now, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
			StatusQueued, now, taskID, StatusWaitingHuman)
		return err
	})
	if err != nil {
		return nil, err
	}
	items, err := s.listInbox(ctx, `WHERE id = ?`, id)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func (s *Store) ListInbox(ctx context.Context, status string) ([]*InboxItem, error) {
	if status == "" {
		return s.listInbox(ctx, ``)
	}
	return s.listInbox(ctx, `WHERE status = ?`, status)
}

func (s *Store) InboxForTask(ctx context.Context, taskID string) ([]*InboxItem, error) {
	return s.listInbox(ctx, `WHERE task_id = ?`, taskID)
}

func (s *Store) listInbox(ctx context.Context, where string, args ...any) ([]*InboxItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, task_id, employee_id, question, answer, status, created_at, answered_at
		FROM inbox `+where+` ORDER BY created_at ASC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*InboxItem{}
	for rows.Next() {
		var it InboxItem
		var created, answered int64
		if err := rows.Scan(&it.ID, &it.TaskID, &it.EmployeeID, &it.Question, &it.Answer, &it.Status, &created, &answered); err != nil {
			return nil, err
		}
		it.CreatedAt, it.AnsweredAt = fromMS(created), fromMS(answered)
		out = append(out, &it)
	}
	return out, rows.Err()
}

// ---- Activity log ----

type Activity struct {
	ID         int64     `json:"id"`
	EmployeeID string    `json:"employee_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) AddActivity(ctx context.Context, employeeID, taskID, message string) (*Activity, error) {
	now := s.ms()
	res, err := s.db.ExecContext(ctx, `INSERT INTO activity (employee_id, task_id, message, created_at) VALUES (?,?,?,?)`,
		employeeID, taskID, message, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Activity{ID: id, EmployeeID: employeeID, TaskID: taskID, Message: message, CreatedAt: fromMS(now)}, nil
}

func (s *Store) ListActivity(ctx context.Context, limit int) ([]*Activity, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, employee_id, task_id, message, created_at FROM activity ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Activity{}
	for rows.Next() {
		var a Activity
		var created int64
		if err := rows.Scan(&a.ID, &a.EmployeeID, &a.TaskID, &a.Message, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = fromMS(created)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// ListTaskActivity returns the activity of a task and of its subtasks, oldest first.
func (s *Store) ListTaskActivity(ctx context.Context, taskID string, limit int) ([]*Activity, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, employee_id, task_id, message, created_at FROM activity
		WHERE task_id = ? OR task_id IN (SELECT id FROM tasks WHERE parent_id = ?) ORDER BY id ASC LIMIT ?`, taskID, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Activity{}
	for rows.Next() {
		var a Activity
		var created int64
		if err := rows.Scan(&a.ID, &a.EmployeeID, &a.TaskID, &a.Message, &created); err != nil {
			return nil, err
		}
		a.CreatedAt = fromMS(created)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// ---- Usage ----

func (s *Store) AddUsage(ctx context.Context, taskID, employeeID string, in, out int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO usage (task_id, employee_id, input_tokens, output_tokens, created_at) VALUES (?,?,?,?,?)`,
		taskID, employeeID, in, out, s.ms())
	return err
}

// TokensSince returns total input+output tokens recorded at or after t.
func (s *Store) TokensSince(ctx context.Context, t time.Time) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(input_tokens + output_tokens) FROM usage WHERE created_at >= ?`, t.UnixMilli()).Scan(&n)
	return n.Int64, err
}

// ---- Secrets (ciphertext only; encryption lives in package secrets) ----

func (s *Store) PutSecret(ctx context.Context, name string, ciphertext []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO secrets (name, ciphertext, updated_at) VALUES (?,?,?)
		ON CONFLICT(name) DO UPDATE SET ciphertext = excluded.ciphertext, updated_at = excluded.updated_at`, name, ciphertext, s.ms())
	return err
}

func (s *Store) GetSecret(ctx context.Context, name string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT ciphertext FROM secrets WHERE name = ?`, name).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *Store) DeleteSecret(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListSecretNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
