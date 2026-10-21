package test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/alerts"
)

func TestOnlyDiscordWebhooksAreAccepted(t *testing.T) {
	for _, ok := range []string{"https://discord.com/api/webhooks/1/abc", "https://canary.discord.com/api/webhooks/1/abc", "https://discordapp.com/api/webhooks/1/abc"} {
		if err := alerts.ValidWebhook(ok); err != nil {
			t.Errorf("%s should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://discord.com/api/webhooks/1/abc", "https://evil.example/api/webhooks/1/abc",
		"https://discord.com.evil.example/api/webhooks/1/abc", "https://discord.com/other", "https://user:pw@discord.com/api/webhooks/1/a", "file:///etc/passwd", ""} {
		if err := alerts.ValidWebhook(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestMailHeadersCannotBeInjected(t *testing.T) {
	m := alerts.Mail{From: "Cubicle <a@example.com>", To: "me@example.com"}
	raw := string(m.Message("Hello\r\nBcc: x@evil.example", "line one\nline two", time.Unix(0, 0)))
	head, body, _ := strings.Cut(raw, "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") {
		t.Fatalf("a line break in the subject must not start a new header:\n%s", head)
	}
	if body != "line one\r\nline two" {
		t.Fatalf("body should use CRLF line ends: %q", body)
	}
	if err := (alerts.Mail{Host: "smtp.example.com", Port: 25, User: "u", Password: "p", From: "bad", To: "me@example.com"}).Validate(); err == nil {
		t.Fatal("an invalid sender address must be refused")
	}
}

func TestWaitingApprovalIsPostedToDiscordAndSecretsStayPrivate(t *testing.T) {
	h := newHarness(t)
	got := make(chan string, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(raw, &m)
		got <- m.Content
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()
	h.gw.Alerts.AnyHost = true

	// Turning Discord on without a webhook is refused.
	h.do("PUT", "/api/v1/alerts", adminTok, map[string]any{"waiting": true, "discord": true}, 400, nil)

	var view map[string]any
	h.do("PUT", "/api/v1/alerts", adminTok, map[string]any{"waiting": true, "discord": true, "discord_webhook": hook.URL + "/api/webhooks/1/secret-token"}, 200, &view)
	if view["discord_webhook_set"] != true {
		t.Fatalf("the webhook should be stored: %+v", view)
	}
	var again map[string]any
	h.do("GET", "/api/v1/alerts", adminTok, nil, 200, &again)
	if raw, _ := json.Marshal(again); strings.Contains(string(raw), "secret-token") {
		t.Fatalf("the webhook must never be returned: %s", raw)
	}

	h.do("POST", "/api/v1/alerts/test", adminTok, map[string]string{"channel": "discord"}, 204, nil)
	if msg := <-got; !strings.Contains(msg, "Cubicle can reach you") {
		t.Fatalf("test message: %q", msg)
	}

	_, claim := startChatReply(h, "manager")
	h.do("POST", "/internal/v1/tasks/"+claim.Task.ID+"/ask", workerTok, map[string]string{
		"employee_id": "manager", "kind": "approval", "question": "**Send the report** to bob@example.com?"}, 201, nil)
	select {
	case msg := <-got:
		if !strings.Contains(msg, "need your approval") || !strings.Contains(msg, "Send the report to bob@example.com?") || strings.Contains(msg, "**Send") {
			t.Fatalf("the alert should name the approval and carry the plain question: %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no alert arrived")
	}

	// Switched off: nothing more is sent.
	h.do("PUT", "/api/v1/alerts", adminTok, map[string]any{"waiting": false, "discord": true}, 200, nil)
	_, second := startChatReply(h, "manager")
	h.do("POST", "/internal/v1/tasks/"+second.Task.ID+"/ask", workerTok, map[string]string{
		"employee_id": "manager", "kind": "question", "question": "Another one?"}, 201, nil)
	select {
	case msg := <-got:
		t.Fatalf("alerts for waiting items are off, got %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}
