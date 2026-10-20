package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const agentsSchema = `
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	avatar TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'specialist',
	description TEXT NOT NULL DEFAULT '',
	instructions TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	effort TEXT NOT NULL DEFAULT '',
	tools TEXT NOT NULL DEFAULT '[]',
	mcp_servers TEXT NOT NULL DEFAULT '[]',
	skills TEXT NOT NULL DEFAULT '[]',
	team TEXT NOT NULL DEFAULT '[]',
	enabled INTEGER NOT NULL DEFAULT 1,
	template_id TEXT NOT NULL DEFAULT '',
	plugin_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'offline',
	current_task_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
`

// Agent roles. Exactly one agent is the main agent: it receives assignments,
// plans them and hands the pieces to the agents on its team.
const (
	RoleMain       = "manager"
	RoleSpecialist = "specialist"
)

// MaxAgents bounds the roster; every agent runs its own desk in the worker.
const MaxAgents = 50

var (
	agentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	slugStrip      = regexp.MustCompile(`[^a-z0-9]+`)

	ErrLastMain = errors.New("this is the main agent: choose another main agent first")
)

// Agent is one member of the owner's AI staff, fully configured by the owner.
type Agent struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Title        string    `json:"title"`
	Avatar       string    `json:"avatar"`
	Role         string    `json:"role"`
	Description  string    `json:"description"`  // what they are good at; the main agent routes work by it
	Instructions string    `json:"instructions"` // how they think and work (their system prompt)
	Model        string    `json:"model"`        // empty = the office default
	Effort       string    `json:"effort"`       // empty = the office default
	Tools        []string  `json:"tools"`        // built-in tool ids
	MCPServers   []string  `json:"mcp_servers"`  // MCP server ids
	Skills       []string  `json:"skills"`       // skill ids
	Team         []string  `json:"team"`         // agents this one can hand work to
	Enabled      bool      `json:"enabled"`
	TemplateID   string    `json:"template_id,omitempty"`
	PluginID     string    `json:"plugin_id,omitempty"`
	Status       string    `json:"status"`
	CurrentTask  string    `json:"current_task_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (a Agent) IsMain() bool { return a.Role == RoleMain }

const agentCols = `id, name, title, avatar, role, description, instructions, model, effort, tools, mcp_servers,
	skills, team, enabled, template_id, plugin_id, status, current_task_id, created_at, updated_at`

func scanAgent(r scanner) (*Agent, error) {
	var a Agent
	var tools, mcp, skills, team string
	var enabled int
	var created, updated int64
	if err := r.Scan(&a.ID, &a.Name, &a.Title, &a.Avatar, &a.Role, &a.Description, &a.Instructions, &a.Model,
		&a.Effort, &tools, &mcp, &skills, &team, &enabled, &a.TemplateID, &a.PluginID, &a.Status, &a.CurrentTask,
		&created, &updated); err != nil {
		return nil, err
	}
	a.Tools, a.MCPServers, a.Skills, a.Team = jsonList(tools), jsonList(mcp), jsonList(skills), jsonList(team)
	a.Enabled = enabled == 1
	a.CreatedAt, a.UpdatedAt = fromMS(created), fromMS(updated)
	return &a, nil
}

func jsonList(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func listJSON(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// NormalizeAgent trims fields, de-duplicates lists and validates what can be
// checked without the database.
func NormalizeAgent(a *Agent) error {
	a.Name = strings.TrimSpace(a.Name)
	a.Title = strings.TrimSpace(a.Title)
	a.Avatar = strings.TrimSpace(a.Avatar)
	a.Model = strings.TrimSpace(a.Model)
	a.Effort = strings.TrimSpace(a.Effort)
	a.Description = strings.TrimSpace(a.Description)
	a.Instructions = strings.TrimSpace(a.Instructions)
	if a.Role != RoleMain {
		a.Role = RoleSpecialist
	}
	for _, f := range []struct {
		name, v string
		max     int
	}{{"name", a.Name, 60}, {"title", a.Title, 80}, {"avatar", a.Avatar, 16}, {"model", a.Model, 200},
		{"description", a.Description, 1000}, {"instructions", a.Instructions, 20000}} {
		if len(f.v) > f.max {
			return fmt.Errorf("%s exceeds %d characters", f.name, f.max)
		}
	}
	if a.Name == "" {
		return errors.New("name is required")
	}
	if strings.ContainsAny(a.Model, " \t\r\n") {
		return errors.New("model must not contain whitespace")
	}
	if a.Effort != "" && !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, a.Effort) {
		return errors.New("effort must be low, medium, high, xhigh or max")
	}
	a.Tools, a.MCPServers, a.Skills = uniq(a.Tools), uniq(a.MCPServers), uniq(a.Skills)
	a.Team = slices.DeleteFunc(uniq(a.Team), func(id string) bool { return id == a.ID })
	for _, l := range [][]string{a.Tools, a.MCPServers, a.Skills, a.Team} {
		if len(l) > 100 {
			return errors.New("lists are limited to 100 entries")
		}
	}
	return nil
}

func uniq(in []string) []string {
	out := []string{}
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// Slug turns a name into an agent id candidate ("Rhea Smith" -> "rhea-smith").
func Slug(name string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "agent"
	}
	return s
}

func uniqueAgentID(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	return uniqueID(ctx, tx, "agents", base)
}

func insertAgent(ctx context.Context, tx *sql.Tx, now int64, a *Agent) error {
	a.CreatedAt, a.UpdatedAt = fromMS(now), fromMS(now)
	if a.Status == "" {
		a.Status = "offline"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO agents (`+agentCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Title, a.Avatar, a.Role, a.Description, a.Instructions, a.Model, a.Effort,
		listJSON(a.Tools), listJSON(a.MCPServers), listJSON(a.Skills), listJSON(a.Team), boolInt(a.Enabled),
		a.TemplateID, a.PluginID, a.Status, "", now, now)
	return err
}

// SeedAgents installs the starting roster when the office has no agents yet.
// It returns false when agents already exist.
func (s *Store) SeedAgents(ctx context.Context, roster []Agent) (bool, error) {
	seeded := false
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents`).Scan(&n); err != nil || n > 0 {
			return err
		}
		mains := 0
		for _, a := range roster {
			if a.Role == RoleMain {
				mains++
			}
		}
		if mains != 1 {
			return errors.New("seed roster needs exactly one main agent")
		}
		now := s.ms()
		for i := range roster {
			a := roster[i]
			if !agentIDPattern.MatchString(a.ID) {
				return fmt.Errorf("invalid agent id %q", a.ID)
			}
			if err := NormalizeAgent(&a); err != nil {
				return fmt.Errorf("agent %s: %w", a.ID, err)
			}
			if err := insertAgent(ctx, tx, now, &a); err != nil {
				return err
			}
		}
		seeded = true
		return nil
	})
	return seeded, err
}

// CreateAgent adds an agent. The id is derived from the name when empty. When
// joinTeam is set, a new specialist also joins the main agent's team.
func (s *Store) CreateAgent(ctx context.Context, a Agent, joinTeam bool) (*Agent, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents`).Scan(&n); err != nil {
			return err
		}
		if n >= MaxAgents {
			return fmt.Errorf("an office can have at most %d agents", MaxAgents)
		}
		a.Role, a.Enabled = RoleSpecialist, true
		var err error
		if a.ID, err = uniqueAgentID(ctx, tx, firstNonEmpty(a.ID, a.Name)); err != nil {
			return err
		}
		if err := NormalizeAgent(&a); err != nil {
			return err
		}
		now := s.ms()
		if err := insertAgent(ctx, tx, now, &a); err != nil {
			return err
		}
		if joinTeam {
			_, err = tx.ExecContext(ctx, `UPDATE agents SET team = json_insert(team, '$[#]', ?), updated_at = ? WHERE role = ?`,
				a.ID, now, RoleMain)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetAgent(ctx, a.ID)
}

// UpdateAgent replaces an agent's editable fields. Role, status and origin are
// kept; use SetMainAgent to change the main agent.
func (s *Store) UpdateAgent(ctx context.Context, in Agent) (*Agent, error) {
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := scanAgent(tx.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, in.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		in.Role = cur.Role
		if cur.IsMain() && !in.Enabled {
			return errors.New("the main agent cannot be turned off")
		}
		if err := NormalizeAgent(&in); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET name=?, title=?, avatar=?, description=?, instructions=?, model=?,
			effort=?, tools=?, mcp_servers=?, skills=?, team=?, enabled=?, updated_at=? WHERE id = ?`,
			in.Name, in.Title, in.Avatar, in.Description, in.Instructions, in.Model, in.Effort, listJSON(in.Tools),
			listJSON(in.MCPServers), listJSON(in.Skills), listJSON(in.Team), boolInt(in.Enabled), s.ms(), in.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetAgent(ctx, in.ID)
}

// SetMainAgent promotes an agent to main. The previous main agent joins the new
// main agent's team, so nobody falls out of the organisation.
func (s *Store) SetMainAgent(ctx context.Context, id string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		next, err := scanAgent(tx.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if next.IsMain() {
			return nil
		}
		team := next.Team
		var prev *Agent
		prev, err = scanAgent(tx.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE role = ?`, RoleMain))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if prev != nil {
			team = uniq(append(append(prev.Team, prev.ID), team...))
		}
		team = slices.DeleteFunc(team, func(t string) bool { return t == id })
		now := s.ms()
		if _, err := tx.ExecContext(ctx, `UPDATE agents SET role = ?, updated_at = ? WHERE role = ?`, RoleSpecialist, now, RoleMain); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET role = ?, enabled = 1, team = ?, updated_at = ? WHERE id = ?`,
			RoleMain, listJSON(team), now, id)
		return err
	})
}

// DeleteAgent removes an agent, drops it from every team, and hands its
// unfinished queued work to the main agent so nothing waits forever.
func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		a, err := scanAgent(tx.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if a.IsMain() {
			return ErrLastMain
		}
		return deleteAgentTx(ctx, tx, s.ms(), id)
	})
}

func deleteAgentTx(ctx context.Context, tx *sql.Tx, now int64, id string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE id = ? AND role != ?`, id, RoleMain); err != nil {
		return err
	}
	if err := removeFromLists(ctx, tx, "agents", "team", id, now); err != nil {
		return err
	}
	var mainID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE role = ?`, RoleMain).Scan(&mainID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE tasks SET assignee = ?, updated_at = ? WHERE assignee = ? AND status = ?`,
		mainID, now, id, StatusQueued)
	return err
}

// removeFromLists deletes value from a JSON list column on every row of table.
func removeFromLists(ctx context.Context, tx *sql.Tx, table, column, value string, now int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, `+column+` FROM `+table+` WHERE EXISTS
		(SELECT 1 FROM json_each(`+table+`.`+column+`) WHERE value = ?)`, value)
	if err != nil {
		return err
	}
	updates := map[string]string{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		updates[id] = listJSON(slices.DeleteFunc(jsonList(raw), func(v string) bool { return v == value }))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for id, list := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET `+column+` = ?, updated_at = ? WHERE id = ?`, list, now, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetAgent(ctx context.Context, id string) (*Agent, error) {
	a, err := scanAgent(s.db.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// MainAgent returns the agent that receives assignments.
func (s *Store) MainAgent(ctx context.Context) (*Agent, error) {
	a, err := scanAgent(s.db.QueryRowContext(ctx, `SELECT `+agentCols+` FROM agents WHERE role = ?`, RoleMain))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// ListAgents returns the roster: the main agent first, then by name.
func (s *Store) ListAgents(ctx context.Context) ([]*Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentCols+` FROM agents
		ORDER BY CASE role WHEN 'manager' THEN 0 ELSE 1 END, created_at, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AgentIsActive reports whether an enabled agent with this id exists.
func (s *Store) AgentIsActive(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE id = ? AND enabled = 1`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) SetAgentStatus(ctx context.Context, id, status, taskID string) (*Agent, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE agents SET status = ?, current_task_id = ? WHERE id = ?`, status, taskID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetAgent(ctx, id)
}

// MarkAllOffline is called when the worker process disconnects or exits.
func (s *Store) MarkAllOffline(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agents SET status = 'offline', current_task_id = ''`)
	return err
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
