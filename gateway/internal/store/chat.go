package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const chatSchema = `
CREATE TABLE IF NOT EXISTS conversations (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	task_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'done',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_conversation ON messages(conversation_id, created_at);
`

// Message roles.
const (
	MessageOwner = "owner"
	MessageAgent = "agent"
)

// Conversation is a chat between the owner and one agent. Each owner message
// becomes a chat task for that agent; its result becomes the agent's reply.
type Conversation struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	Title       string    `json:"title"`
	LastMessage string    `json:"last_message"`
	Busy        bool      `json:"busy"` // the agent is still replying
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	TaskID         string    `json:"task_id,omitempty"`
	Status         string    `json:"status"` // done | failed
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Store) CreateConversation(ctx context.Context, agentID, title string) (*Conversation, error) {
	if ok, err := s.AgentIsActive(ctx, agentID); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New("unknown or disabled agent")
	}
	now := s.ms()
	c := &Conversation{ID: newID("cnv"), AgentID: agentID, Title: strings.TrimSpace(title),
		CreatedAt: fromMS(now), UpdatedAt: fromMS(now)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations (id, agent_id, title, created_at, updated_at) VALUES (?,?,?,?,?)`,
		c.ID, c.AgentID, c.Title, now, now)
	return c, err
}

const conversationSelect = `SELECT c.id, c.agent_id, c.title, c.created_at, c.updated_at,
	COALESCE((SELECT content FROM messages m WHERE m.conversation_id = c.id ORDER BY created_at DESC, rowid DESC LIMIT 1), ''),
	EXISTS(SELECT 1 FROM tasks t WHERE t.conversation_id = c.id AND t.status IN ('queued','in_progress'))
	FROM conversations c `

func scanConversation(r scanner) (*Conversation, error) {
	var c Conversation
	var created, updated int64
	if err := r.Scan(&c.ID, &c.AgentID, &c.Title, &created, &updated, &c.LastMessage, &c.Busy); err != nil {
		return nil, err
	}
	c.CreatedAt, c.UpdatedAt = fromMS(created), fromMS(updated)
	c.LastMessage = preview(c.LastMessage, 140)
	return &c, nil
}

func (s *Store) ListConversations(ctx context.Context) ([]*Conversation, error) {
	rows, err := s.db.QueryContext(ctx, conversationSelect+`ORDER BY c.updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	c, err := scanConversation(s.db.QueryRowContext(ctx, conversationSelect+`WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// Messages returns a conversation's messages, oldest first.
func (s *Store) Messages(ctx context.Context, conversationID string, limit int) ([]*Message, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, role, content, task_id, status, created_at FROM
		(SELECT *, rowid AS rid FROM messages WHERE conversation_id = ? ORDER BY created_at DESC, rid DESC LIMIT ?)
		ORDER BY created_at, rid`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		var m Message
		var created int64
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.TaskID, &m.Status, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = fromMS(created)
		out = append(out, &m)
	}
	return out, rows.Err()
}

// SendMessage stores the owner's message and queues a chat task so the agent replies.
func (s *Store) SendMessage(ctx context.Context, conversationID, content string) (*Message, *Task, error) {
	var msg *Message
	var task *Task
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var agentID, title string
		err := tx.QueryRowContext(ctx, `SELECT agent_id, title FROM conversations WHERE id = ?`, conversationID).Scan(&agentID, &title)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var enabled int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE id = ? AND enabled = 1`, agentID).Scan(&enabled); err != nil {
			return err
		}
		if enabled == 0 {
			return errors.New("this agent was removed or turned off")
		}
		now := s.ms()
		msg = &Message{ID: newID("msg"), ConversationID: conversationID, Role: MessageOwner, Content: content,
			Status: StatusDone, CreatedAt: fromMS(now)}
		task, err = insertTask(ctx, tx, now, NewTask{
			Kind: KindChat, Title: preview(content, 120), Description: content, Assignee: agentID,
			ConversationID: conversationID,
		})
		if err != nil {
			return err
		}
		msg.TaskID = task.ID
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, conversation_id, role, content, task_id, status, created_at)
			VALUES (?,?,?,?,?,?,?)`, msg.ID, conversationID, MessageOwner, content, task.ID, StatusDone, now); err != nil {
			return err
		}
		if title == "" {
			title = preview(content, 60)
		}
		_, err = tx.ExecContext(ctx, `UPDATE conversations SET title = ?, updated_at = ? WHERE id = ?`, title, now, conversationID)
		return err
	})
	return msg, task, err
}

// addReply stores the agent's reply (or failure) for a finished chat task.
func addReply(ctx context.Context, tx *sql.Tx, now int64, t *Task, content, status string) error {
	if t.ConversationID == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, conversation_id, role, content, task_id, status, created_at)
		VALUES (?,?,?,?,?,?,?)`, newID("msg"), t.ConversationID, MessageAgent, content, t.ID, status, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now, t.ConversationID)
	return err
}

// DeleteConversation removes the conversation and cancels replies still pending.
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ?`, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status = ?, updated_at = ? WHERE conversation_id = ? AND status IN (?,?)`,
			StatusCancelled, s.ms(), id, StatusQueued, StatusInProgress)
		return err
	})
}

// preview shortens text to at most n runes on one line.
func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
