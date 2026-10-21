package api

import (
	"net/http"
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// ---- owner API: conversations ----

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListConversations(r.Context())
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentID string `json:"agent_id"`
		Title   string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("title", in.Title, 120, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.store.CreateConversation(r.Context(), in.AgentID, in.Title)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Publish("conversation.updated", c)
	writeJSON(w, http.StatusCreated, c)
}

type ConversationDetail struct {
	*store.Conversation
	Messages []*store.Message `json:"messages"`
	// Started are the assignments agents started from this chat, each tied to the reply task that started it.
	Started []store.StartedAssignment `json:"started"`
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	msgs, err := s.store.Messages(r.Context(), c.ID, 500)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	started, err := s.store.StartedFromChat(r.Context(), c.ID)
	if err != nil {
		s.storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ConversationDetail{Conversation: c, Messages: msgs, Started: started})
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if err := checkLen("content", in.Content, 20000, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	msg, task, err := s.store.SendMessage(r.Context(), r.PathValue("id"), in.Content)
	if err != nil {
		if err == store.ErrNotFound {
			s.storeErr(w, err)
		} else {
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	s.publishTasks(task)
	s.publishConversation(r, msg.ConversationID)
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteConversation(r.Context(), r.PathValue("id")); err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("conversation.deleted", map[string]string{"id": r.PathValue("id")})
	s.hub.Wake()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) publishConversation(r *http.Request, id string) {
	if c, err := s.store.GetConversation(r.Context(), id); err == nil {
		s.hub.Publish("conversation.updated", c)
	}
}

// ---- worker API ----

// handleProgress relays live progress from a working agent ("Searching the
// web…", or the reply so far) to the app. Progress is not stored.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
		Text   string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("status", in.Status, 300, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := s.store.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.storeErr(w, err)
		return
	}
	s.hub.Publish("task.progress", map[string]string{
		"task_id": t.ID, "conversation_id": t.ConversationID, "agent_id": t.Assignee,
		"status": in.Status, "text": truncate(in.Text, 200000),
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleWorkerAssignment lets an agent with the "assignments" tool start an
// assignment for the main agent from a chat.
func (s *Server) handleWorkerAssignment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		AgentID     string `json:"agent_id"`
		// SourceTaskID is the chat task whose reply started this assignment.
		SourceTaskID string `json:"source_task_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := checkLen("title", in.Title, 300, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := checkLen("description", in.Description, 20000, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := s.store.CreateTask(r.Context(), store.NewTask{
		Kind: store.KindAssignment, Title: in.Title, Description: in.Description, Assignee: s.managerID(r.Context()),
	})
	if err != nil {
		s.storeErr(w, err)
		return
	}
	if in.SourceTaskID != "" {
		if err := s.store.LinkAssignment(r.Context(), t.ID, in.SourceTaskID); err != nil {
			s.storeErr(w, err)
			return
		}
	}
	s.publishTasks(t)
	s.logActivity(r.Context(), in.AgentID, t.ID, "New assignment from a chat: "+t.Title)
	if in.SourceTaskID != "" {
		s.logActivity(r.Context(), in.AgentID, in.SourceTaskID, "Started the assignment “"+t.Title+"”.")
	}
	writeJSON(w, http.StatusCreated, t)
}
