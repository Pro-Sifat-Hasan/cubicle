// Package alerts tells the owner outside the app when something waits for them: a Discord message through a
// webhook, or an email through their own mail server. Credentials are handed in by the caller and never logged.
package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const timeout = 15 * time.Second

// Sender delivers alerts. The zero value is ready to use; tests set Client and AnyHost.
type Sender struct {
	Client  *http.Client
	AnyHost bool // accept a webhook on any host (tests); real use only allows Discord
}

func (s *Sender) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// ValidWebhook reports whether a URL is a Discord webhook (https, discord.com or discordapp.com, /api/webhooks/).
func ValidWebhook(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil {
		return errors.New("the webhook must be an https link from Discord")
	}
	host := strings.ToLower(u.Hostname())
	okHost := host == "discord.com" || host == "discordapp.com" || strings.HasSuffix(host, ".discord.com") || strings.HasSuffix(host, ".discordapp.com")
	if !okHost || !strings.HasPrefix(u.Path, "/api/webhooks/") {
		return errors.New("that does not look like a Discord webhook (https://discord.com/api/webhooks/…)")
	}
	return nil
}

// Discord posts a message to a webhook. Mentions are switched off so message text can never ping anyone.
func (s *Sender) Discord(ctx context.Context, webhook, text string) error {
	if !s.AnyHost {
		if err := ValidWebhook(webhook); err != nil {
			return err
		}
	}
	body, _ := json.Marshal(map[string]any{"content": truncate(text, 1900), "allowed_mentions": map[string]any{"parse": []string{}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(webhook), bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid webhook")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return errors.New("could not reach Discord")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("discord refused the message (%d); check the webhook", resp.StatusCode)
	}
	return nil
}

// Mail is how to reach the owner's mail server.
type Mail struct {
	Host, User, Password, From, To string
	Port                           int
}

// Validate checks the settings without connecting.
func (m Mail) Validate() error {
	if strings.TrimSpace(m.Host) == "" || strings.ContainsAny(m.Host, " /\r\n") {
		return errors.New("mail server is required, for example smtp.gmail.com")
	}
	if m.Port != 465 && m.Port != 587 && m.Port != 25 {
		return errors.New("mail server port must be 465, 587 or 25")
	}
	if strings.TrimSpace(m.User) == "" || m.Password == "" {
		return errors.New("mail user and password are required")
	}
	for _, a := range []string{m.From, m.To} {
		if _, err := mail.ParseAddress(a); err != nil {
			return fmt.Errorf("%q is not a valid email address", a)
		}
	}
	return nil
}

// Message builds the raw mail: plain text, UTF-8, with header values stripped of line breaks.
func (m Mail) Message(subject, body string, now time.Time) []byte {
	clean := func(v string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(v) }
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n", clean(m.From), clean(m.To),
		mime.QEncoding.Encode("utf-8", clean(subject)), now.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return b.Bytes()
}

// Send mails the owner over an encrypted connection (implicit TLS on 465, STARTTLS otherwise; never plain text).
func (m Mail) Send(ctx context.Context, subject, body string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	from, _ := mail.ParseAddress(m.From)
	to, _ := mail.ParseAddress(m.To)
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return errors.New("could not reach the mail server")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * timeout))
	tlsConf := &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}
	if m.Port == 465 {
		conn = tls.Client(conn, tlsConf)
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return errors.New("the mail server did not answer properly")
	}
	defer c.Close()
	if m.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the mail server does not offer an encrypted connection")
		}
		if err := c.StartTLS(tlsConf); err != nil {
			return errors.New("could not make the mail connection encrypted")
		}
	}
	if err := c.Auth(smtp.PlainAuth("", m.User, m.Password, m.Host)); err != nil {
		return errors.New("the mail server refused the user name or password (Gmail needs an app password)")
	}
	if err := c.Mail(from.Address); err != nil {
		return errors.New("the mail server refused the sender address")
	}
	if err := c.Rcpt(to.Address); err != nil {
		return errors.New("the mail server refused the recipient address")
	}
	w, err := c.Data()
	if err != nil {
		return errors.New("the mail server refused the message")
	}
	if _, err := w.Write(m.Message(subject, body, time.Now())); err != nil {
		return errors.New("could not send the message")
	}
	if err := w.Close(); err != nil {
		return errors.New("the mail server did not accept the message")
	}
	return c.Quit()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
