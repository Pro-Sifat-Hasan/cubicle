package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Task struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parent_id,omitempty"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Assignee    string    `json:"assignee"`
	Status      string    `json:"status"`
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	DependsOn   []string  `json:"depends_on"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewTask is the input for creating a task.
type NewTask struct {
	ParentID    string
	Kind        string
	Title       string
	Description string
	Assignee    string
	DependsOn   []string
}

const taskCols = `id, parent_id, kind, title, description, assignee, status, result, error, depends_on, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanTask(r scanner) (*Task, error) {
	var t Task
	var deps string
	var created, updated int64
	if err := r.Scan(&t.ID, &t.ParentID, &t.Kind, &t.Title, &t.Description, &t.Assignee,
		&t.Status, &t.Result, &t.Error, &deps, &created, &updated); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(deps), &t.DependsOn); err != nil || t.DependsOn == nil {
		t.DependsOn = []string{}
	}
	t.CreatedAt, t.UpdatedAt = fromMS(created), fromMS(updated)
	return &t, nil
}

func isTerminal(status string) bool {
	return status == StatusDone || status == StatusFailed || status == StatusCancelled
}

func insertTask(ctx context.Context, tx *sql.Tx, now int64, n NewTask) (*Task, error) {
	if n.DependsOn == nil {
		n.DependsOn = []string{}
	}
	deps, _ := json.Marshal(n.DependsOn)
	t := &Task{
		ID: newID("tsk"), ParentID: n.ParentID, Kind: n.Kind, Title: n.Title,
		Description: n.Description, Assignee: n.Assignee, Status: StatusQueued,
		DependsOn: n.DependsOn, CreatedAt: fromMS(now), UpdatedAt: fromMS(now),
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO tasks (`+taskCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ParentID, t.Kind, t.Title, t.Description, t.Assignee, t.Status, "", "", string(deps), now, now)
	return t, err
}

func (s *Store) CreateTask(ctx context.Context, n NewTask) (*Task, error) {
	var t *Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		t, err = insertTask(ctx, tx, s.ms(), n)
		return err
	})
	return t, err
}

func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	t, err := scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

type TaskFilter struct {
	Status   string
	ParentID *string
	Limit    int
}

func (s *Store) ListTasks(ctx context.Context, f TaskFilter) ([]*Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks WHERE 1=1`
	var args []any
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.ParentID != nil {
		q += ` AND parent_id = ?`
		args = append(args, *f.ParentID)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, f.Limit)
	return s.queryTasks(ctx, q, args...)
}

func (s *Store) queryTasks(ctx context.Context, q string, args ...any) ([]*Task, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTasks returns the tasks with the given IDs, in no particular order.
func (s *Store) GetTasks(ctx context.Context, ids []string) ([]*Task, error) {
	if len(ids) == 0 {
		return []*Task{}, nil
	}
	q := `SELECT ` + taskCols + ` FROM tasks WHERE id IN (` + placeholders(len(ids)) + `)`
	return s.queryTasks(ctx, q, toAny(ids)...)
}

// ClaimTask atomically moves the oldest runnable queued task assigned to one of
// employeeIDs into in_progress. A task is runnable when all its dependencies are done.
// Returns (nil, nil) when nothing is available.
func (s *Store) ClaimTask(ctx context.Context, employeeIDs []string) (*Task, error) {
	if len(employeeIDs) == 0 {
		return nil, nil
	}
	var claimed *Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE status = ? AND assignee IN (`+
			placeholders(len(employeeIDs))+`) ORDER BY created_at ASC LIMIT 50`,
			append([]any{StatusQueued}, toAny(employeeIDs)...)...)
		if err != nil {
			return err
		}
		var candidates []*Task
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}

		for _, t := range candidates {
			if len(t.DependsOn) > 0 {
				var pending int
				err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status != ? AND id IN (`+
					placeholders(len(t.DependsOn))+`)`, append([]any{StatusDone}, toAny(t.DependsOn)...)...).Scan(&pending)
				if err != nil {
					return err
				}
				if pending > 0 {
					continue
				}
			}
			now := s.ms()
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
				StatusInProgress, now, t.ID); err != nil {
				return err
			}
			t.Status, t.UpdatedAt = StatusInProgress, fromMS(now)
			claimed = t
			return nil
		}
		return nil
	})
	return claimed, err
}

// Delegation is the manager's plan for an assignment.
type Delegation struct {
	Subtasks  []DelegatedSubtask
	Synthesis NewTask // Title/Description/Assignee only; wiring is done here
}

type DelegatedSubtask struct {
	Title       string
	Description string
	Assignee    string
	DependsOn   []int // indices into Subtasks
}

// Delegate turns an in-progress assignment into subtasks plus a synthesis task
// that depends on all of them, and marks the assignment delegated.
func (s *Store) Delegate(ctx context.Context, assignmentID string, d Delegation) ([]*Task, error) {
	if len(d.Subtasks) == 0 {
		return nil, errors.New("delegation needs at least one subtask")
	}
	var created []*Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		parent, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, assignmentID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if parent.Status != StatusInProgress || parent.Kind != KindAssignment {
			return ErrConflict
		}
		now := s.ms()
		ids := make([]string, len(d.Subtasks))
		for i, st := range d.Subtasks {
			var deps []string
			for _, j := range st.DependsOn {
				if j < 0 || j >= i {
					return fmt.Errorf("subtask %d: depends_on index %d must reference an earlier subtask", i, j)
				}
				deps = append(deps, ids[j])
			}
			t, err := insertTask(ctx, tx, now, NewTask{
				ParentID: parent.ID, Kind: KindSubtask, Title: st.Title,
				Description: st.Description, Assignee: st.Assignee, DependsOn: deps,
			})
			if err != nil {
				return err
			}
			ids[i] = t.ID
			created = append(created, t)
		}
		syn, err := insertTask(ctx, tx, now, NewTask{
			ParentID: parent.ID, Kind: KindSynthesis, Title: d.Synthesis.Title,
			Description: d.Synthesis.Description, Assignee: d.Synthesis.Assignee, DependsOn: ids,
		})
		if err != nil {
			return err
		}
		created = append(created, syn)
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`, StatusDelegated, now, parent.ID)
		return err
	})
	return created, err
}

// Complete marks a task done. Completing a synthesis task also completes its
// parent assignment with the same result. Returns every task that changed.
func (s *Store) Complete(ctx context.Context, id, result string) ([]*Task, error) {
	var changed []*Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		t, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if t.Status != StatusInProgress {
			return ErrConflict
		}
		now := s.ms()
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, result = ?, updated_at = ? WHERE id = ?`,
			StatusDone, result, now, id); err != nil {
			return err
		}
		t.Status, t.Result, t.UpdatedAt = StatusDone, result, fromMS(now)
		changed = append(changed, t)

		if t.Kind == KindSynthesis && t.ParentID != "" {
			res, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, result = ?, updated_at = ? WHERE id = ? AND status = ?`,
				StatusDone, result, now, t.ParentID, StatusDelegated)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				p, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, t.ParentID))
				if err != nil {
					return err
				}
				changed = append(changed, p)
			}
		}
		return nil
	})
	return changed, err
}

// Fail marks a task failed. A failed subtask fails its parent assignment and
// cancels the siblings that have not finished, so nothing waits forever.
func (s *Store) Fail(ctx context.Context, id, reason string) ([]*Task, error) {
	var changed []string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		t, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if isTerminal(t.Status) {
			return ErrConflict
		}
		now := s.ms()
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
			StatusFailed, reason, now, id); err != nil {
			return err
		}
		changed = append(changed, id)
		if t.ParentID == "" {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status NOT IN (?,?,?)`,
			StatusFailed, fmt.Sprintf("%q failed: %s", t.Title, reason), now, t.ParentID, StatusDone, StatusFailed, StatusCancelled); err != nil {
			return err
		}
		changed = append(changed, t.ParentID)
		ids, err := cancelChildren(ctx, tx, now, t.ParentID)
		changed = append(changed, ids...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetTasks(ctx, changed)
}

// Cancel cancels a task and all of its unfinished children.
func (s *Store) Cancel(ctx context.Context, id string) ([]*Task, error) {
	var changed []string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id = ?`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if isTerminal(status) {
			return ErrConflict
		}
		now := s.ms()
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`, StatusCancelled, now, id); err != nil {
			return err
		}
		changed = append(changed, id)
		ids, err := cancelChildren(ctx, tx, now, id)
		changed = append(changed, ids...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetTasks(ctx, changed)
}

func cancelChildren(ctx context.Context, tx *sql.Tx, now int64, parentID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE parent_id = ? AND status NOT IN (?,?,?)`,
		parentID, StatusDone, StatusFailed, StatusCancelled)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`, StatusCancelled, now, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// TaskCounts returns the number of tasks per status.
func (s *Store) TaskCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM tasks GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
