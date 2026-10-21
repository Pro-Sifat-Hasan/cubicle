package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

const maxTriageBatch = 100

// handleTriageCheck tells an agent which of the message keys it found are new: POST /internal/v1/triage/check {keys}.
func (s *Server) handleTriageCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Keys []string `json:"keys"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Keys) > maxTriageBatch {
		writeErr(w, http.StatusBadRequest, "too many keys")
		return
	}
	for _, k := range in.Keys {
		if err := checkLen("key", strings.TrimSpace(k), 300, true); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	seen, err := s.store.SeenTriageKeys(r.Context(), in.Keys)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	fresh := []string{}
	for _, k := range in.Keys {
		if !seen[k] {
			fresh = append(fresh, k)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"new": fresh})
}

// handleTriageRecord saves what happened to messages: POST /internal/v1/triage/record {items:[{key,source,summary,status}]}.
func (s *Server) handleTriageRecord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []store.TriageItem `json:"items"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Items) == 0 || len(in.Items) > maxTriageBatch {
		writeErr(w, http.StatusBadRequest, "send between 1 and 100 items")
		return
	}
	for i := range in.Items {
		it := &in.Items[i]
		it.Key, it.Source, it.Summary = strings.TrimSpace(it.Key), strings.TrimSpace(it.Source), strings.TrimSpace(it.Summary)
		if err := checkLen("key", it.Key, 300, true); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := checkLen("summary", it.Summary, 400, false); err != nil || len(it.Source) > 60 || !store.ValidTriageStatus(it.Status) {
			writeErr(w, http.StatusBadRequest, "invalid item")
			return
		}
	}
	if err := s.store.RecordTriage(r.Context(), in.Items); err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("triage.updated", map[string]int{"count": len(in.Items)})
	w.WriteHeader(http.StatusNoContent)
}

// handleListTriage lists the messages the staff looked at: GET /api/v1/triage?status=&limit=.
func (s *Server) handleListTriage(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListTriage(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// MessageCheckTitle is the automation that goes through the owner's messages.
const MessageCheckTitle = "Check my messages"

const messageCheckDescription = "Go through my connected message apps (email, Discord, Slack) for anything new since the last check, " +
	"using the message-triage skill if you have it. Read only. Use triage_check so you never look at a message twice. " +
	"Find what needs me (requests, questions, mentions, deadlines, invitations), then call propose_tasks once with those items " +
	"and stop: I approve or change them. Do only what I approve; write replies as drafts in my voice and ask before sending anything. " +
	"If nothing new needs me, finish with one line: Nothing new needs you."

// ensureMessageCheck makes sure the "Check my messages" automation exists once an app that receives messages is
// connected. It goes to the first chosen agent that is not the main agent (it does the reading itself), or to the
// main agent. An automation the owner already has, changed or switched off, is left alone.
func (s *Server) ensureMessageCheck(ctx context.Context, agents []*store.Agent) {
	existing, err := s.store.ListRoutines(ctx)
	if err != nil {
		return
	}
	for _, rt := range existing {
		if rt.Title == MessageCheckTitle {
			return
		}
	}
	agentID := ""
	for _, a := range agents {
		if a.Role != store.RoleMain && a.Enabled {
			agentID = a.ID
			break
		}
	}
	rt, err := s.store.CreateRoutine(ctx, store.RoutineInput{
		Title: MessageCheckTitle, Description: messageCheckDescription, Schedule: "every:1h", AgentID: agentID,
	})
	if err != nil {
		s.log.Warn("could not create the message check automation", "err", logSafe(err.Error()))
		return
	}
	s.logActivity(ctx, "", "", "Turned on “"+rt.Title+"”: your staff look through your messages every hour and ask before doing anything.")
}
