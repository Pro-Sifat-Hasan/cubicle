package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const alertsKey = "alerts"

// Names of the secrets behind alerts. They are kept encrypted and never handed to the agents.
const (
	AlertSecretPrefix  = "ALERT_"
	SecretAlertDiscord = "ALERT_DISCORD_WEBHOOK"
	SecretAlertSMTP    = "ALERT_SMTP_PASSWORD"
)

// AlertSettings choose how the owner is reached outside the app when something waits for them.
type AlertSettings struct {
	Waiting  bool   `json:"waiting"` // an agent has a question or needs approval
	Done     bool   `json:"done"`    // an assignment is finished
	Discord  bool   `json:"discord"` // post to a Discord webhook
	Email    bool   `json:"email"`   // send an email
	EmailTo  string `json:"email_to"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPUser string `json:"smtp_user"`
	From     string `json:"email_from"`
}

func DefaultAlerts() AlertSettings { return AlertSettings{Waiting: true, SMTPPort: 587} }

func (s *Store) GetAlerts(ctx context.Context) (AlertSettings, error) {
	out := DefaultAlerts()
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, alertsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

func (s *Store) PutAlerts(ctx context.Context, v AlertSettings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, alertsKey, string(raw))
	return err
}
