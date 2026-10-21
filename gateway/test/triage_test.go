package test

import (
	"testing"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

func TestTriageLedgerRemembersWhatWasLookedAt(t *testing.T) {
	h := newHarness(t)

	var fresh struct {
		New []string `json:"new"`
	}
	h.do("POST", "/internal/v1/triage/check", workerTok, map[string]any{"keys": []string{"gmail:1", "gmail:2"}}, 200, &fresh)
	if len(fresh.New) != 2 {
		t.Fatalf("nothing is known yet: %+v", fresh)
	}

	h.do("POST", "/internal/v1/triage/record", workerTok, map[string]any{"items": []map[string]string{
		{"key": "gmail:1", "source": "gmail", "summary": "Invoice question from Acme", "status": "proposed"},
	}}, 204, nil)
	h.do("POST", "/internal/v1/triage/record", workerTok, map[string]any{"items": []map[string]string{
		{"key": "gmail:1", "status": "done"},
	}}, 204, nil)
	h.do("POST", "/internal/v1/triage/check", workerTok, map[string]any{"keys": []string{"gmail:1", "gmail:2"}}, 200, &fresh)
	if len(fresh.New) != 1 || fresh.New[0] != "gmail:2" {
		t.Fatalf("only the unseen message is new: %+v", fresh)
	}

	var list []store.TriageItem
	h.do("GET", "/api/v1/triage?status=done", adminTok, nil, 200, &list)
	if len(list) != 1 || list[0].Summary != "Invoice question from Acme" || list[0].Status != store.TriageDone {
		t.Fatalf("the summary is kept when a later update has none: %+v", list)
	}
	h.do("POST", "/internal/v1/triage/record", workerTok, map[string]any{"items": []map[string]string{{"key": "x", "status": "weird"}}}, 400, nil)
}

func TestConnectingAMessageAppTurnsOnTheMessageCheck(t *testing.T) {
	h := newHarness(t)
	h.do("POST", "/api/v1/integrations/gmail/connect", adminTok, map[string]any{
		"values": map[string]string{"email": "sam@gmail.com", "password": "abcdefghijklmnop"}, "agent_ids": []string{"manager"},
	}, 200, nil)
	h.do("POST", "/api/v1/integrations/gmail/connect", adminTok, map[string]any{
		"values": map[string]string{"email": "sam@gmail.com"}, "agent_ids": []string{"manager"},
	}, 200, nil)

	var routines []store.Routine
	h.do("GET", "/api/v1/routines", adminTok, nil, 200, &routines)
	found := 0
	for _, r := range routines {
		if r.Title == "Check my messages" {
			found++
			if !r.Enabled || r.Schedule != "every:1h" {
				t.Fatalf("the check should be on and hourly: %+v", r)
			}
		}
	}
	if found != 1 {
		t.Fatalf("connecting twice must leave one automation, got %d", found)
	}
}
