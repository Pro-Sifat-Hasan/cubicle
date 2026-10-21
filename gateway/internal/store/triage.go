package store

import (
	"context"
	"strings"
	"time"
)

const triageSchema = `
CREATE TABLE IF NOT EXISTS triage_items (
	key TEXT PRIMARY KEY,
	source TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_triage_updated ON triage_items(updated_at);
`

// Triage statuses: what happened to a message the staff looked at.
const (
	TriageProposed  = "proposed"  // turned into a task and put to the owner for approval
	TriageApproved  = "approved"  // the owner said yes
	TriageDone      = "done"      // the work is finished
	TriageDismissed = "dismissed" // the owner declined it, or it needed nothing
)

// MaxTriageItems caps the ledger; the oldest entries go first.
const MaxTriageItems = 5000

// TriageItem is one message (an email, a Discord post, a mention) the staff have looked at, so the same message is
// never raised twice and the owner can see what came in and what was done about it.
type TriageItem struct {
	Key       string    `json:"key"` // stable id from the source, e.g. "gmail:18c2f" or "discord:123/456"
	Source    string    `json:"source"`
	Summary   string    `json:"summary"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ValidTriageStatus(s string) bool {
	switch s {
	case TriageProposed, TriageApproved, TriageDone, TriageDismissed:
		return true
	}
	return false
}

// SeenTriageKeys returns the keys (of those given) that are already in the ledger.
func (s *Store) SeenTriageKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	seen := map[string]bool{}
	for _, k := range keys {
		var one int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM triage_items WHERE key = ?`, k).Scan(&one)
		if err == nil {
			seen[k] = true
		}
	}
	return seen, nil
}

// RecordTriage saves what happened to messages; an existing key moves to the new status.
func (s *Store) RecordTriage(ctx context.Context, items []TriageItem) error {
	now := s.now().UTC().UnixMilli()
	for _, it := range items {
		_, err := s.db.ExecContext(ctx, `INSERT INTO triage_items (key, source, summary, status, created_at, updated_at) VALUES (?,?,?,?,?,?)
			ON CONFLICT(key) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at,
				summary = CASE WHEN excluded.summary = '' THEN summary ELSE excluded.summary END`,
			it.Key, it.Source, it.Summary, it.Status, now, now)
		if err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM triage_items WHERE key NOT IN
		(SELECT key FROM triage_items ORDER BY updated_at DESC LIMIT ?)`, MaxTriageItems)
	return err
}

// ListTriage returns the ledger, most recently changed first; an empty status means all.
func (s *Store) ListTriage(ctx context.Context, status string, limit int) ([]*TriageItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT key, source, summary, status, created_at, updated_at FROM triage_items`
	args := []any{}
	if status = strings.TrimSpace(status); status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY updated_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TriageItem{}
	for rows.Next() {
		var it TriageItem
		var created, updated int64
		if err := rows.Scan(&it.Key, &it.Source, &it.Summary, &it.Status, &created, &updated); err != nil {
			return nil, err
		}
		it.CreatedAt, it.UpdatedAt = fromMS(created), fromMS(updated)
		out = append(out, &it)
	}
	return out, rows.Err()
}
