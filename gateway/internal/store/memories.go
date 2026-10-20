package store

import (
	"context"
	"strings"
	"time"
)

const memoriesSchema = `
CREATE TABLE IF NOT EXISTS memories (
	id TEXT PRIMARY KEY,
	content TEXT NOT NULL,
	employee_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_content ON memories(content);
`

// MaxMemories caps long-term memory; the oldest entries are dropped first.
const MaxMemories = 300

// Memory is a durable fact about the owner or their work that the staff should
// keep in mind ("Prefers British spelling", "Main client is Acme Ltd").
type Memory struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	EmployeeID string    `json:"employee_id,omitempty"` // empty when added by the owner
	CreatedAt  time.Time `json:"created_at"`
}

// AddMemory stores a fact. Exact duplicates are ignored and return the existing entry.
func (s *Store) AddMemory(ctx context.Context, content, employeeID string) (*Memory, error) {
	content = strings.TrimSpace(content)
	m := &Memory{ID: newID("mem"), Content: content, EmployeeID: employeeID, CreatedAt: s.now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO memories (id, content, employee_id, created_at) VALUES (?,?,?,?)
		ON CONFLICT(content) DO NOTHING`, m.ID, m.Content, employeeID, m.CreatedAt.UnixMilli())
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM memories WHERE id NOT IN
		(SELECT id FROM memories ORDER BY created_at DESC LIMIT ?)`, MaxMemories)
	if err != nil {
		return nil, err
	}
	var created int64
	err = s.db.QueryRowContext(ctx, `SELECT id, employee_id, created_at FROM memories WHERE content = ?`, content).
		Scan(&m.ID, &m.EmployeeID, &created)
	m.CreatedAt = fromMS(created)
	return m, err
}

// ListMemories returns memories, newest first.
func (s *Store) ListMemories(ctx context.Context, limit int) ([]*Memory, error) {
	if limit <= 0 || limit > MaxMemories {
		limit = MaxMemories
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, content, employee_id, created_at FROM memories ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Memory{}
	for rows.Next() {
		var m Memory
		var created int64
		if err := rows.Scan(&m.ID, &m.Content, &m.EmployeeID, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = fromMS(created)
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
