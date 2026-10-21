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
	created_at INTEGER NOT NULL,
	kind TEXT NOT NULL DEFAULT 'fact'
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memories_content ON memories(content);
`

// MaxMemories caps long-term memory; the oldest entries are dropped first.
const MaxMemories = 300

// Memory kinds. There is one profile: everything known about the owner, kept as a single record the staff rewrite.
const (
	KindFact    = "fact"
	KindProfile = "profile"
)

const profileID = "mem_profile"

// Memory is a durable fact about the owner or their work that the staff should
// keep in mind ("Prefers British spelling", "Main client is Acme Ltd").
type Memory struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	EmployeeID string    `json:"employee_id,omitempty"` // empty when added by the owner
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"created_at"`
}

// AddMemory stores a fact. Exact duplicates are ignored and return the existing entry.
func (s *Store) AddMemory(ctx context.Context, content, employeeID string) (*Memory, error) {
	content = strings.TrimSpace(content)
	m := &Memory{ID: newID("mem"), Content: content, EmployeeID: employeeID, Kind: KindFact, CreatedAt: s.now().UTC()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO memories (id, content, employee_id, created_at) VALUES (?,?,?,?)
		ON CONFLICT(content) DO NOTHING`, m.ID, m.Content, employeeID, m.CreatedAt.UnixMilli())
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM memories WHERE id NOT IN
		(SELECT id FROM memories WHERE kind = 'fact' ORDER BY created_at DESC LIMIT ?) AND kind = 'fact'`, MaxMemories)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, content, employee_id, created_at, kind FROM memories
		ORDER BY (kind = 'profile') DESC, created_at DESC LIMIT ?`, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Memory{}
	for rows.Next() {
		var m Memory
		var created int64
		if err := rows.Scan(&m.ID, &m.Content, &m.EmployeeID, &created, &m.Kind); err != nil {
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

// SetProfile writes the owner's profile, replacing the previous one. Empty content removes it.
func (s *Store) SetProfile(ctx context.Context, content, employeeID string) (*Memory, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, profileID)
		return nil, err
	}
	// The profile must not collide with a fact of the same text (content is unique).
	if _, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE content = ? AND id <> ?`, content, profileID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO memories (id, content, employee_id, created_at, kind) VALUES (?,?,?,?, 'profile')
		ON CONFLICT(id) DO UPDATE SET content = excluded.content, employee_id = excluded.employee_id, created_at = excluded.created_at`,
		profileID, content, employeeID, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	return &Memory{ID: profileID, Content: content, EmployeeID: employeeID, Kind: KindProfile, CreatedAt: now}, nil
}
