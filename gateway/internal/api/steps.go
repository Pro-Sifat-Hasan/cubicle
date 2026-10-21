package api

import (
	"errors"
	"net/http"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// handleSaveStep records one thing a working agent did (a thought, a remark, a tool call and its
// output) and relays it to the app live. Calling it again with the same id updates that step, which
// is how a tool call gets its output when it finishes.
func (s *Server) handleSaveStep(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Input   string `json:"input"`
		Output  string `json:"output"`
		Status  string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.store.SaveStep(r.Context(), store.Step{
		ID: in.ID, TaskID: r.PathValue("id"), AgentID: in.AgentID, Kind: in.Kind, Name: in.Name,
		Input: in.Input, Output: in.Output, Status: in.Status,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.storeErr(w, err)
		return
	case errors.Is(err, store.ErrTooManySteps):
		w.WriteHeader(http.StatusNoContent) // the run goes on; only the record stops growing
		return
	case err != nil:
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if t, err := s.store.GetTask(r.Context(), st.TaskID); err == nil {
		st.ConversationID = t.ConversationID
	}
	s.hub.Publish("task.step", st)
	w.WriteHeader(http.StatusNoContent)
}

// handleListSteps returns what agents did: for one task (and its subtasks) or for every reply of a chat.
func (s *Server) handleListSteps(w http.ResponseWriter, r *http.Request) {
	var list []*store.Step
	var err error
	if id := r.URL.Query().Get("conversation_id"); id != "" {
		list, err = s.store.ListConversationSteps(r.Context(), id)
	} else if id := r.URL.Query().Get("task_id"); id != "" {
		list, err = s.store.ListTaskSteps(r.Context(), id)
	} else {
		writeErr(w, http.StatusBadRequest, "task_id or conversation_id is required")
		return
	}
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
