package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Extensions give agents extra abilities: MCP servers (external tools), skills
// (reusable instructions an agent loads when relevant) and plugins (installable
// bundles of agents, MCP servers and skills).

const extensionsSchema = `
CREATE TABLE IF NOT EXISTS mcp_servers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	transport TEXT NOT NULL,
	command TEXT NOT NULL DEFAULT '',
	args TEXT NOT NULL DEFAULT '[]',
	env TEXT NOT NULL DEFAULT '{}',
	secret_env TEXT NOT NULL DEFAULT '[]',
	url TEXT NOT NULL DEFAULT '',
	headers TEXT NOT NULL DEFAULT '{}',
	auth_secret TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	template_id TEXT NOT NULL DEFAULT '',
	plugin_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS skills (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	template_id TEXT NOT NULL DEFAULT '',
	plugin_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS plugins (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	version TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	catalog_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
`

const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

var (
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	skillName      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// ---- MCP servers ----

// MCPServer is an external tool server agents can use (Model Context Protocol).
// Secrets never live here: SecretEnv and AuthSecret name encrypted secrets that
// the worker injects when it starts or calls the server.
type MCPServer struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"` // stdio | http
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`        // plain, non-secret variables
	SecretEnv   []string          `json:"secret_env"` // secrets passed to a stdio server as variables
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers"`               // plain, non-secret headers
	AuthSecret  string            `json:"auth_secret,omitempty"` // secret sent as a bearer token (http)
	Enabled     bool              `json:"enabled"`
	TemplateID  string            `json:"template_id,omitempty"`
	PluginID    string            `json:"plugin_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

const mcpCols = `id, name, description, transport, command, args, env, secret_env, url, headers, auth_secret, enabled,
	template_id, plugin_id, created_at, updated_at`

func scanMCP(r scanner) (*MCPServer, error) {
	var m MCPServer
	var args, env, secretEnv, headers string
	var enabled int
	var created, updated int64
	if err := r.Scan(&m.ID, &m.Name, &m.Description, &m.Transport, &m.Command, &args, &env, &secretEnv, &m.URL,
		&headers, &m.AuthSecret, &enabled, &m.TemplateID, &m.PluginID, &created, &updated); err != nil {
		return nil, err
	}
	m.Args, m.SecretEnv = jsonList(args), jsonList(secretEnv)
	m.Env, m.Headers = jsonMap(env), jsonMap(headers)
	m.Enabled = enabled == 1
	m.CreatedAt, m.UpdatedAt = fromMS(created), fromMS(updated)
	return &m, nil
}

func jsonMap(raw string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func mapJSON(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// NormalizeMCPServer validates a server definition.
func NormalizeMCPServer(m *MCPServer) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.TrimSpace(m.Description)
	m.Command = strings.TrimSpace(m.Command)
	m.URL = strings.TrimSpace(m.URL)
	m.AuthSecret = strings.TrimSpace(m.AuthSecret)
	if m.Name == "" || len(m.Name) > 80 {
		return errors.New("name is required (up to 80 characters)")
	}
	if len(m.Description) > 1000 {
		return errors.New("description exceeds 1000 characters")
	}
	if m.Args == nil {
		m.Args = []string{}
	}
	if m.Env == nil {
		m.Env = map[string]string{}
	}
	if m.Headers == nil {
		m.Headers = map[string]string{}
	}
	m.SecretEnv = uniq(m.SecretEnv)
	switch m.Transport {
	case TransportStdio:
		if m.Command == "" || len(m.Command) > 500 {
			return errors.New("a stdio server needs a command, e.g. npx or uvx")
		}
		m.URL, m.Headers, m.AuthSecret = "", map[string]string{}, ""
	case TransportHTTP:
		u, err := url.Parse(m.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("an http server needs an http(s) URL")
		}
		m.Command, m.Args, m.Env, m.SecretEnv = "", []string{}, map[string]string{}, []string{}
	default:
		return errors.New("transport must be stdio or http")
	}
	if len(m.Args) > 50 || len(m.Env) > 50 || len(m.Headers) > 50 || len(m.SecretEnv) > 20 {
		return errors.New("too many arguments, variables or headers")
	}
	for _, a := range m.Args {
		if len(a) > 2000 {
			return errors.New("argument too long")
		}
	}
	for k, v := range m.Env {
		if !envNamePattern.MatchString(k) || len(v) > 4000 {
			return fmt.Errorf("invalid environment variable %q", k)
		}
	}
	for _, k := range m.SecretEnv {
		if !envNamePattern.MatchString(k) {
			return fmt.Errorf("invalid secret name %q", k)
		}
	}
	for k, v := range m.Headers {
		if k == "" || len(k) > 200 || len(v) > 4000 || strings.ContainsAny(k+v, "\r\n") {
			return fmt.Errorf("invalid header %q", k)
		}
	}
	if m.AuthSecret != "" && !envNamePattern.MatchString(m.AuthSecret) {
		return fmt.Errorf("invalid secret name %q", m.AuthSecret)
	}
	return nil
}

func insertMCP(ctx context.Context, tx *sql.Tx, now int64, m *MCPServer) error {
	var err error
	if m.ID, err = uniqueID(ctx, tx, "mcp_servers", firstNonEmpty(m.ID, m.Name)); err != nil {
		return err
	}
	if err := NormalizeMCPServer(m); err != nil {
		return err
	}
	m.CreatedAt, m.UpdatedAt = fromMS(now), fromMS(now)
	_, err = tx.ExecContext(ctx, `INSERT INTO mcp_servers (`+mcpCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Name, m.Description, m.Transport, m.Command, listJSON(m.Args), mapJSON(m.Env), listJSON(m.SecretEnv),
		m.URL, mapJSON(m.Headers), m.AuthSecret, boolInt(m.Enabled), m.TemplateID, m.PluginID, now, now)
	return err
}

func (s *Store) CreateMCPServer(ctx context.Context, m MCPServer) (*MCPServer, error) {
	m.Enabled = true
	err := s.withTx(ctx, func(tx *sql.Tx) error { return insertMCP(ctx, tx, s.ms(), &m) })
	if err != nil {
		return nil, err
	}
	return s.GetMCPServer(ctx, m.ID)
}

func (s *Store) UpdateMCPServer(ctx context.Context, m MCPServer) (*MCPServer, error) {
	if err := NormalizeMCPServer(&m); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE mcp_servers SET name=?, description=?, transport=?, command=?, args=?, env=?,
		secret_env=?, url=?, headers=?, auth_secret=?, enabled=?, updated_at=? WHERE id = ?`,
		m.Name, m.Description, m.Transport, m.Command, listJSON(m.Args), mapJSON(m.Env), listJSON(m.SecretEnv), m.URL,
		mapJSON(m.Headers), m.AuthSecret, boolInt(m.Enabled), s.ms(), m.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetMCPServer(ctx, m.ID)
}

func (s *Store) GetMCPServer(ctx context.Context, id string) (*MCPServer, error) {
	m, err := scanMCP(s.db.QueryRowContext(ctx, `SELECT `+mcpCols+` FROM mcp_servers WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMCPServers(ctx context.Context) ([]*MCPServer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mcpCols+` FROM mcp_servers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*MCPServer{}
	for rows.Next() {
		m, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMCPServer removes a server and detaches it from every agent.
func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return deleteRowAndRefs(ctx, tx, s.ms(), "mcp_servers", "mcp_servers", id) })
}

// ---- Skills ----

// Skill is a reusable set of instructions (the SKILL.md format): agents see the
// name and description, and load the full content when a task calls for it.
type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Enabled     bool      `json:"enabled"`
	TemplateID  string    `json:"template_id,omitempty"`
	PluginID    string    `json:"plugin_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const skillCols = `id, name, description, content, enabled, template_id, plugin_id, created_at, updated_at`

func scanSkill(r scanner) (*Skill, error) {
	var sk Skill
	var enabled int
	var created, updated int64
	if err := r.Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Content, &enabled, &sk.TemplateID, &sk.PluginID,
		&created, &updated); err != nil {
		return nil, err
	}
	sk.Enabled = enabled == 1
	sk.CreatedAt, sk.UpdatedAt = fromMS(created), fromMS(updated)
	return &sk, nil
}

// NormalizeSkill validates a skill. Names follow the SKILL.md convention:
// lowercase letters, digits and hyphens.
func NormalizeSkill(sk *Skill) error {
	sk.Name = Slug(strings.TrimSpace(sk.Name))
	sk.Description = strings.TrimSpace(sk.Description)
	sk.Content = strings.TrimSpace(sk.Content)
	if !skillName.MatchString(sk.Name) {
		return errors.New("name must use lowercase letters, digits and hyphens")
	}
	if sk.Description == "" || len(sk.Description) > 1024 {
		return errors.New("description is required (up to 1024 characters): it tells agents when to use the skill")
	}
	if sk.Content == "" || len(sk.Content) > 100_000 {
		return errors.New("content is required (up to 100,000 characters)")
	}
	return nil
}

func insertSkill(ctx context.Context, tx *sql.Tx, now int64, sk *Skill) error {
	if err := NormalizeSkill(sk); err != nil {
		return err
	}
	var err error
	if sk.ID, err = uniqueID(ctx, tx, "skills", firstNonEmpty(sk.ID, sk.Name)); err != nil {
		return err
	}
	sk.CreatedAt, sk.UpdatedAt = fromMS(now), fromMS(now)
	_, err = tx.ExecContext(ctx, `INSERT INTO skills (`+skillCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		sk.ID, sk.Name, sk.Description, sk.Content, boolInt(sk.Enabled), sk.TemplateID, sk.PluginID, now, now)
	return err
}

func (s *Store) CreateSkill(ctx context.Context, sk Skill) (*Skill, error) {
	sk.Enabled = true
	err := s.withTx(ctx, func(tx *sql.Tx) error { return insertSkill(ctx, tx, s.ms(), &sk) })
	if err != nil {
		return nil, err
	}
	return s.GetSkill(ctx, sk.ID)
}

func (s *Store) UpdateSkill(ctx context.Context, sk Skill) (*Skill, error) {
	if err := NormalizeSkill(&sk); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE skills SET name=?, description=?, content=?, enabled=?, updated_at=? WHERE id = ?`,
		sk.Name, sk.Description, sk.Content, boolInt(sk.Enabled), s.ms(), sk.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetSkill(ctx, sk.ID)
}

func (s *Store) GetSkill(ctx context.Context, id string) (*Skill, error) {
	sk, err := scanSkill(s.db.QueryRowContext(ctx, `SELECT `+skillCols+` FROM skills WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sk, err
}

func (s *Store) ListSkills(ctx context.Context) ([]*Skill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+skillCols+` FROM skills ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// DeleteSkill removes a skill and detaches it from every agent.
func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error { return deleteRowAndRefs(ctx, tx, s.ms(), "skills", "skills", id) })
}

// ---- Plugins ----

// Plugin is an installed bundle. Its agents, MCP servers and skills carry its id
// and are removed together with it.
type Plugin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Source      string    `json:"source"` // catalog | import
	CatalogID   string    `json:"catalog_id,omitempty"`
	Agents      []string  `json:"agents"`
	MCPServers  []string  `json:"mcp_servers"`
	Skills      []string  `json:"skills"`
	CreatedAt   time.Time `json:"created_at"`
}

// PluginBundle is what gets installed. Agents may refer to the bundle's own
// MCP servers and skills by the ids used inside the bundle.
type PluginBundle struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Version     string      `json:"version"`
	Agents      []Agent     `json:"agents"`
	MCPServers  []MCPServer `json:"mcp_servers"`
	Skills      []Skill     `json:"skills"`
}

// InstallPlugin creates every item of a bundle in one transaction. Bundle-local
// references from agents to MCP servers and skills are rewritten to the ids that
// were actually assigned; new agents join the main agent's team.
func (s *Store) InstallPlugin(ctx context.Context, b PluginBundle, source, catalogID string) (*Plugin, error) {
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len(b.Name) > 80 || len(b.Description) > 2000 || len(b.Version) > 40 {
		return nil, errors.New("a plugin needs a name (up to 80 characters)")
	}
	if len(b.Agents)+len(b.MCPServers)+len(b.Skills) == 0 {
		return nil, errors.New("this plugin contains no agents, MCP servers or skills")
	}
	if len(b.Agents) > 20 || len(b.MCPServers) > 20 || len(b.Skills) > 50 {
		return nil, errors.New("this plugin is too large")
	}
	p := &Plugin{Name: b.Name, Description: strings.TrimSpace(b.Description), Version: strings.TrimSpace(b.Version),
		Source: source, CatalogID: catalogID}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.ms()
		var err error
		if p.ID, err = uniqueID(ctx, tx, "plugins", b.Name); err != nil {
			return err
		}
		mcpIDs, skillIDs := map[string]string{}, map[string]string{}
		for i := range b.MCPServers {
			m := b.MCPServers[i]
			local := firstNonEmpty(m.ID, m.Name)
			m.PluginID, m.Enabled = p.ID, true
			if err := insertMCP(ctx, tx, now, &m); err != nil {
				return fmt.Errorf("MCP server %q: %w", local, err)
			}
			mcpIDs[local], p.MCPServers = m.ID, append(p.MCPServers, m.ID)
		}
		for i := range b.Skills {
			sk := b.Skills[i]
			local := firstNonEmpty(sk.ID, sk.Name)
			sk.PluginID, sk.Enabled = p.ID, true
			if err := insertSkill(ctx, tx, now, &sk); err != nil {
				return fmt.Errorf("skill %q: %w", local, err)
			}
			skillIDs[local], p.Skills = sk.ID, append(p.Skills, sk.ID)
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents`).Scan(&count); err != nil {
			return err
		}
		if count+len(b.Agents) > MaxAgents {
			return fmt.Errorf("an office can have at most %d agents", MaxAgents)
		}
		for i := range b.Agents {
			a := b.Agents[i]
			a.Role, a.Enabled, a.PluginID, a.Team = RoleSpecialist, true, p.ID, nil
			a.MCPServers, a.Skills = remap(a.MCPServers, mcpIDs), remap(a.Skills, skillIDs)
			if a.ID, err = uniqueAgentID(ctx, tx, firstNonEmpty(a.ID, a.Name)); err != nil {
				return err
			}
			if err := NormalizeAgent(&a); err != nil {
				return fmt.Errorf("agent %q: %w", a.Name, err)
			}
			if err := insertAgent(ctx, tx, now, &a); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE agents SET team = json_insert(team, '$[#]', ?) WHERE role = ?`,
				a.ID, RoleMain); err != nil {
				return err
			}
			p.Agents = append(p.Agents, a.ID)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO plugins (id, name, description, version, source, catalog_id, created_at)
			VALUES (?,?,?,?,?,?,?)`, p.ID, p.Name, p.Description, p.Version, source, catalogID, now)
		p.CreatedAt = fromMS(now)
		return err
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// remap rewrites bundle-local ids; ids not defined in the bundle are kept, so a
// bundle can also refer to items the office already has.
func remap(ids []string, local map[string]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if v, ok := local[id]; ok {
			id = v
		}
		out = append(out, id)
	}
	return out
}

func (s *Store) ListPlugins(ctx context.Context) ([]*Plugin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, version, source, catalog_id, created_at FROM plugins ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	var out []*Plugin
	for rows.Next() {
		var p Plugin
		var created int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Version, &p.Source, &p.CatalogID, &created); err != nil {
			rows.Close()
			return nil, err
		}
		p.CreatedAt = fromMS(created)
		out = append(out, &p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, p := range out {
		for _, q := range []struct {
			table string
			dst   *[]string
		}{{"agents", &p.Agents}, {"mcp_servers", &p.MCPServers}, {"skills", &p.Skills}} {
			if *q.dst, err = s.idsWhere(ctx, q.table, "plugin_id", p.ID); err != nil {
				return nil, err
			}
		}
	}
	if out == nil {
		out = []*Plugin{}
	}
	return out, nil
}

// UninstallPlugin removes the plugin and everything it installed. A plugin agent
// that is currently the main agent is kept (and detached) so the office keeps working.
func (s *Store) UninstallPlugin(ctx context.Context, id string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM plugins WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		now := s.ms()
		for _, q := range []struct{ table, refColumn string }{{"mcp_servers", "mcp_servers"}, {"skills", "skills"}} {
			ids, err := idsWhereTx(ctx, tx, q.table, "plugin_id", id)
			if err != nil {
				return err
			}
			for _, item := range ids {
				if err := deleteRowAndRefs(ctx, tx, now, q.table, q.refColumn, item); err != nil {
					return err
				}
			}
		}
		agents, err := idsWhereTx(ctx, tx, "agents", "plugin_id", id)
		if err != nil {
			return err
		}
		for _, a := range agents {
			if err := deleteAgentTx(ctx, tx, now, a); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents SET plugin_id = '' WHERE plugin_id = ?`, id)
		return err
	})
}

// ---- helpers ----

// uniqueID finds a free slug-based id in table.
func uniqueID(ctx context.Context, tx *sql.Tx, table, base string) (string, error) {
	base = Slug(base)
	for i := 1; i < 1000; i++ {
		id := base
		if i > 1 {
			id = fmt.Sprintf("%s-%d", base, i)
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return id, nil
		}
	}
	return "", errors.New("could not find a free id")
}

// deleteRowAndRefs deletes one row and removes its id from the agents' list column.
func deleteRowAndRefs(ctx context.Context, tx *sql.Tx, now int64, table, agentColumn, id string) error {
	res, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return removeFromLists(ctx, tx, "agents", agentColumn, id, now)
}

func (s *Store) idsWhere(ctx context.Context, table, column, value string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM `+table+` WHERE `+column+` = ? ORDER BY created_at`, value)
	if err != nil {
		return nil, err
	}
	return collectIDs(rows)
}

func idsWhereTx(ctx context.Context, tx *sql.Tx, table, column, value string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+table+` WHERE `+column+` = ?`, value)
	if err != nil {
		return nil, err
	}
	return collectIDs(rows)
}

func collectIDs(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
