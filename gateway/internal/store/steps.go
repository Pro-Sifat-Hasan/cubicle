package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const stepsSchema = `
CREATE TABLE IF NOT EXISTS steps (
	task_id TEXT NOT NULL,
	id TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	input TEXT NOT NULL DEFAULT '',
	output TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'running',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (task_id, id)
);
CREATE INDEX IF NOT EXISTS idx_steps_created ON steps(created_at);
`

// Step kinds.
const (
	StepThinking = "thinking" // the model reasoning (text may be empty when the provider hides it)
	StepNote     = "note"     // what the agent says between tool calls
	StepTool     = "tool"     // a tool call: its input, then its output
)

// Step statuses.
const (
	StepRunning = "running"
	StepDone    = "done"
	StepError   = "error"
)

// Limits keep one runaway run from filling the database or the live event stream.
const (
	maxStepsPerTask = 400
	maxStepName     = 120
	maxStepInput    = 8000
	maxStepOutput   = 12000
	maxStepNote     = 6000
)

// Step is one thing an agent did while working on a task: thought, said something, or used a tool.
// A tool step is first stored when the call starts (status running, with its input) and updated
// with its output when the call finishes.
type Step struct {
	ID             string    `json:"id"`
	TaskID         string    `json:"task_id"`
	ConversationID string    `json:"conversation_id,omitempty"`
	AgentID        string    `json:"agent_id,omitempty"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name,omitempty"`
	Input          string    `json:"input,omitempty"`
	Output         string    `json:"output,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ErrTooManySteps is returned when a task already holds the maximum number of steps.
var ErrTooManySteps = errors.New("this task has reached its step limit")

// SaveStep inserts a step, or replaces the content of the step with the same id on the same task.
func (s *Store) SaveStep(ctx context.Context, st Step) (*Step, error) {
	switch st.Kind {
	case StepThinking, StepNote, StepTool:
	default:
		return nil, errors.New("kind must be thinking, note or tool")
	}
	switch st.Status {
	case "":
		st.Status = StepDone
	case StepRunning, StepDone, StepError:
	default:
		return nil, errors.New("status must be running, done or error")
	}
	if strings.TrimSpace(st.ID) == "" || len(st.ID) > 80 {
		return nil, errors.New("id must be 1-80 characters")
	}
	if _, err := s.GetTask(ctx, st.TaskID); err != nil {
		return nil, err
	}
	st.Name = cut(st.Name, maxStepName)
	st.Input = cut(st.Input, maxStepInput)
	st.Output = cut(st.Output, maxStepOutput)
	if st.Kind == StepNote || st.Kind == StepThinking {
		st.Output = cut(st.Output, maxStepNote)
	}

	now := s.ms()
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var created int64
		err := tx.QueryRowContext(ctx, `SELECT created_at FROM steps WHERE task_id = ? AND id = ?`, st.TaskID, st.ID).Scan(&created)
		if err == nil {
			st.CreatedAt = fromMS(created)
			_, err = tx.ExecContext(ctx, `UPDATE steps SET agent_id = ?, kind = ?, name = ?, input = ?, output = ?, status = ?, updated_at = ?
				WHERE task_id = ? AND id = ?`, st.AgentID, st.Kind, st.Name, st.Input, st.Output, st.Status, now, st.TaskID, st.ID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM steps WHERE task_id = ?`, st.TaskID).Scan(&n); err != nil {
			return err
		}
		if n >= maxStepsPerTask {
			return ErrTooManySteps
		}
		st.CreatedAt = fromMS(now)
		_, err = tx.ExecContext(ctx, `INSERT INTO steps (task_id, id, agent_id, kind, name, input, output, status, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, st.TaskID, st.ID, st.AgentID, st.Kind, st.Name, st.Input, st.Output, st.Status, now, now)
		return err
	})
	if err != nil {
		return nil, err
	}
	st.UpdatedAt = fromMS(now)
	return &st, nil
}

// ListTaskSteps returns the steps of a task and of its subtasks, oldest first.
func (s *Store) ListTaskSteps(ctx context.Context, taskID string) ([]*Step, error) {
	return s.querySteps(ctx, `WHERE s.task_id = ? OR s.task_id IN (SELECT id FROM tasks WHERE parent_id = ?)`, taskID, taskID)
}

// ListConversationSteps returns the steps of every reply task in a chat, oldest first.
func (s *Store) ListConversationSteps(ctx context.Context, conversationID string) ([]*Step, error) {
	return s.querySteps(ctx, `WHERE s.task_id IN (SELECT id FROM tasks WHERE conversation_id = ?)`, conversationID)
}

func (s *Store) querySteps(ctx context.Context, where string, args ...any) ([]*Step, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.task_id, s.id, s.agent_id, s.kind, s.name, s.input, s.output, s.status,
		s.created_at, s.updated_at, COALESCE((SELECT t.conversation_id FROM tasks t WHERE t.id = s.task_id), '')
		FROM steps s `+where+` ORDER BY s.created_at, s.rowid LIMIT 5000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Step{}
	for rows.Next() {
		var st Step
		var created, updated int64
		if err := rows.Scan(&st.TaskID, &st.ID, &st.AgentID, &st.Kind, &st.Name, &st.Input, &st.Output, &st.Status,
			&created, &updated, &st.ConversationID); err != nil {
			return nil, err
		}
		st.CreatedAt, st.UpdatedAt = fromMS(created), fromMS(updated)
		out = append(out, &st)
	}
	return out, rows.Err()
}

// FinishSteps marks steps still running for a task as done, so a finished reply never shows a spinner.
func (s *Store) FinishSteps(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE steps SET status = ?, updated_at = ? WHERE task_id = ? AND status = ?`,
		StepDone, s.ms(), taskID, StepRunning)
	return err
}

// cut shortens s to at most n bytes without splitting a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}
