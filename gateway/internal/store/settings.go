package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const settingsSchema = `
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

const officeSettingsKey = "office"

// Settings are the owner's choices for how the office runs.
type Settings struct {
	Provider  string `json:"provider"`   // providers.Auto or a catalog id
	Model     string `json:"model"`      // empty = provider default
	Effort    string `json:"effort"`     // low | medium | high | xhigh | max
	BaseURL   string `json:"base_url"`   // only for providers with an editable base URL
	OwnerName string `json:"owner_name"` // how the staff address the owner
	WebAccess bool   `json:"web_access"` // allow web search / fetch tools
	// DailyTokenBudget overrides the gateway config when set; 0 means unlimited.
	DailyTokenBudget *int64 `json:"daily_token_budget"`
}

func DefaultSettings() Settings {
	return Settings{Provider: "auto", Effort: "medium", OwnerName: "the owner", WebAccess: true}
}

func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	out := DefaultSettings()
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, officeSettingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

// Flag reports whether a one-time step (a data upgrade) has already run.
func (s *Store) Flag(ctx context.Context, name string) (bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, "flag:"+name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && v == "1", err
}

// SetFlag records that a one-time step has run.
func (s *Store) SetFlag(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, '1')
		ON CONFLICT(key) DO UPDATE SET value = '1'`, "flag:"+name)
	return err
}

// ClearFlag forgets a one-time step, so it runs again (used by tests).
func (s *Store) ClearFlag(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, "flag:"+name)
	return err
}

func (s *Store) PutSettings(ctx context.Context, v Settings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, officeSettingsKey, string(raw))
	return err
}
