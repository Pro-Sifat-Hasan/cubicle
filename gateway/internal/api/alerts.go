package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/alerts"
	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// alertView is the alert settings as the owner sees them: the secrets are never returned, only whether they are set.
type alertView struct {
	store.AlertSettings
	DiscordSet      bool `json:"discord_webhook_set"`
	SMTPPasswordSet bool `json:"smtp_password_set"`
}

func (s *Server) alertView(ctx context.Context, a store.AlertSettings) (alertView, error) {
	names, err := s.store.ListSecretNames(ctx)
	if err != nil {
		return alertView{}, err
	}
	v := alertView{AlertSettings: a}
	for _, n := range names {
		v.DiscordSet = v.DiscordSet || n == store.SecretAlertDiscord
		v.SMTPPasswordSet = v.SMTPPasswordSet || n == store.SecretAlertSMTP
	}
	return v, nil
}

// handleGetAlerts: GET /api/v1/alerts.
func (s *Server) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAlerts(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	v, err := s.alertView(r.Context(), a)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// handlePutAlerts: PUT /api/v1/alerts. discord_webhook and smtp_password are write-only; empty keeps what is stored.
func (s *Server) handlePutAlerts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		store.AlertSettings
		DiscordWebhook string `json:"discord_webhook"`
		SMTPPassword   string `json:"smtp_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	a := in.AlertSettings
	a.EmailTo, a.SMTPHost, a.SMTPUser, a.From = strings.TrimSpace(a.EmailTo), strings.TrimSpace(a.SMTPHost),
		strings.TrimSpace(a.SMTPUser), strings.TrimSpace(a.From)
	in.DiscordWebhook, in.SMTPPassword = strings.TrimSpace(in.DiscordWebhook), strings.TrimSpace(in.SMTPPassword)
	if a.SMTPPort == 0 {
		a.SMTPPort = 587
	}
	for _, c := range []struct {
		field, v string
		max      int
	}{{"email_to", a.EmailTo, 200}, {"smtp_host", a.SMTPHost, 200}, {"smtp_user", a.SMTPUser, 200}, {"email_from", a.From, 200},
		{"discord_webhook", in.DiscordWebhook, 500}, {"smtp_password", in.SMTPPassword, 500}} {
		if err := checkLen(c.field, c.v, c.max, false); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ctx := r.Context()
	view, err := s.alertView(ctx, a)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if in.DiscordWebhook != "" && !s.Alerts.AnyHost {
		if err := alerts.ValidWebhook(in.DiscordWebhook); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if a.Discord && in.DiscordWebhook == "" && !view.DiscordSet {
		writeErr(w, http.StatusBadRequest, "paste the Discord webhook link to turn Discord alerts on")
		return
	}
	if a.Email {
		password := in.SMTPPassword
		if password == "" && view.SMTPPasswordSet {
			password = "stored"
		}
		if err := (alerts.Mail{Host: a.SMTPHost, Port: a.SMTPPort, User: a.SMTPUser, Password: password, From: a.From, To: a.EmailTo}).Validate(); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for name, value := range map[string]string{store.SecretAlertDiscord: in.DiscordWebhook, store.SecretAlertSMTP: in.SMTPPassword} {
		if value == "" {
			continue
		}
		sealed, err := s.secrets.Seal(name, []byte(value))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "encryption failed")
			return
		}
		if err := s.store.PutSecret(ctx, name, sealed); err != nil {
			s.storeErr(w, err)
			return
		}
	}
	if err := s.store.PutAlerts(ctx, a); err != nil {
		s.storeErr(w, err)
		return
	}
	s.logActivity(ctx, "", "", "Alert settings updated.")
	view, err = s.alertView(ctx, a)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleTestAlert sends one test message: POST /api/v1/alerts/test {channel: "discord" | "email"}.
func (s *Server) handleTestAlert(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channel string `json:"channel"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, err := s.store.GetAlerts(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	switch in.Channel {
	case "discord":
		err = s.sendDiscord(ctx, "✅ Cubicle can reach you here. You will get a message like this when your staff need you.")
	case "email":
		err = s.sendEmail(ctx, a, "Cubicle test", "Cubicle can reach you by email. You will get a message like this when your staff need you.")
	default:
		writeErr(w, http.StatusBadRequest, "channel must be discord or email")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) openSecret(ctx context.Context, name string) (string, error) {
	sealed, err := s.store.GetSecret(ctx, name)
	if err != nil {
		return "", err
	}
	plain, err := s.secrets.OpenSealed(name, sealed)
	return string(plain), err
}

func (s *Server) sendDiscord(ctx context.Context, text string) error {
	hook, err := s.openSecret(ctx, store.SecretAlertDiscord)
	if err != nil {
		return errAlert("save the Discord webhook link first")
	}
	return s.Alerts.Discord(ctx, hook, text)
}

func (s *Server) sendEmail(ctx context.Context, a store.AlertSettings, subject, body string) error {
	password, err := s.openSecret(ctx, store.SecretAlertSMTP)
	if err != nil {
		return errAlert("save the mail password first")
	}
	return alerts.Mail{Host: a.SMTPHost, Port: a.SMTPPort, User: a.SMTPUser, Password: password, From: a.From, To: a.EmailTo}.Send(ctx, subject, body)
}

type errAlert string

func (e errAlert) Error() string { return string(e) }

// notifyOwner tells the owner outside the app, if they asked for that. kind is "waiting" or "done". It never blocks
// the request that caused it, and the same message is not sent twice within a minute.
func (s *Server) notifyOwner(kind, title, body string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		a, err := s.store.GetAlerts(ctx)
		if err != nil || (kind == "waiting" && !a.Waiting) || (kind == "done" && !a.Done) || (!a.Discord && !a.Email) {
			return
		}
		text := strings.NewReplacer("**", "", "__", "").Replace(strings.TrimSpace(body))
		if len([]rune(text)) > 1200 {
			text = string([]rune(text)[:1199]) + "…"
		}
		if s.alertRecentlySent(title + "\n" + text) {
			return
		}
		if a.Discord {
			if err := s.sendDiscord(ctx, "**"+title+"**\n"+text); err != nil {
				s.log.Warn("discord alert failed", "err", logSafe(err.Error()))
			}
		}
		if a.Email {
			if err := s.sendEmail(ctx, a, title, text); err != nil {
				s.log.Warn("email alert failed", "err", logSafe(err.Error()))
			}
		}
	}()
}

func (s *Server) alertRecentlySent(key string) bool {
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	now := time.Now()
	for k, at := range s.alertSent {
		if now.Sub(at) > time.Minute {
			delete(s.alertSent, k)
		}
	}
	if _, dup := s.alertSent[key]; dup {
		return true
	}
	if s.alertSent == nil {
		s.alertSent = map[string]time.Time{}
	}
	s.alertSent[key] = now
	return false
}
