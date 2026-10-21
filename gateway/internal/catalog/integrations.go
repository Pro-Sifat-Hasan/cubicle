package catalog

import (
	"strings"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"
)

// Field kinds an integration asks the owner for.
const (
	FieldSecret = "secret" // stored encrypted under Field.Secret
	FieldText   = "text"   // a plain value, put into {key} placeholders
	FieldFolder = "folder" // a folder or file path on this PC
)

// Field is one thing the owner fills in to connect an app: a key, a folder, a workspace id.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Secret      string `json:"secret,omitempty"` // secret name, for FieldSecret
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Optional    bool   `json:"optional,omitempty"`
}

// MCPTemplate is an app agents can connect to (an MCP server preset). The values of its Fields replace
// {key} placeholders in Args, Env values and URL when the owner connects it.
type MCPTemplate struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Icon        string            `json:"icon"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	SecretEnv   []string          `json:"secret_env"`
	URL         string            `json:"url,omitempty"`
	AuthSecret  string            `json:"auth_secret,omitempty"`
	Fields      []Field           `json:"fields"`
	// Agents are the agent ids and role templates that benefit most; the app pre-selects them.
	Agents []string `json:"agents"`
	// Link is where to get a key or token.
	Link string `json:"link,omitempty"`
	// Requires names what must be installed on this PC (e.g. "Node.js").
	Requires string `json:"requires,omitempty"`
	// Setup explains what the owner should know before connecting.
	Setup string `json:"setup,omitempty"`
}

func (t MCPTemplate) Server() store.MCPServer {
	return store.MCPServer{
		ID: t.ID, Name: t.Name, Description: t.Description, Transport: t.Transport, Command: t.Command,
		Args: append([]string(nil), t.Args...), Env: copyMap(t.Env), SecretEnv: append([]string(nil), t.SecretEnv...),
		URL: t.URL, AuthSecret: t.AuthSecret, TemplateID: t.ID, Enabled: true,
	}
}

// Fill returns the server with the owner's values put into the {key} placeholders.
func (t MCPTemplate) Fill(values map[string]string) store.MCPServer {
	m := t.Server()
	sub := func(s string) string {
		for k, v := range values {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return s
	}
	for i := range m.Args {
		m.Args[i] = sub(m.Args[i])
	}
	for k, v := range m.Env {
		m.Env[k] = sub(v)
	}
	m.URL = sub(m.URL)
	return m
}

func copyMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

var codeAgents = []string{"engineer", "pr-reviewer", "bug-triager", "docs-writer"}

var mcpTemplates = []MCPTemplate{
	{
		ID: "github", Name: "GitHub", Category: "Code", Icon: "🐙", Transport: store.TransportHTTP,
		Description: "Repositories, issues, pull requests, code search and CI status. Agents ask your approval before they comment, push or merge.",
		URL:         "https://api.githubcopilot.com/mcp/", AuthSecret: "GITHUB_PERSONAL_ACCESS_TOKEN",
		Fields: []Field{{Key: "token", Label: "Personal access token", Kind: FieldSecret, Secret: "GITHUB_PERSONAL_ACCESS_TOKEN",
			Placeholder: "github_pat_…", Help: "Create a fine-grained token with read access to the repositories agents should see."}},
		Agents: codeAgents, Link: "https://github.com/settings/personal-access-tokens/new",
	},
	{
		ID: "git", Name: "Git (local project)", Category: "Code", Icon: "🌿", Transport: store.TransportStdio,
		Description: "Read history, diffs and branches of a project folder on this PC.",
		Command:     "uvx", Args: []string{"mcp-server-git", "--repository", "{repo}"}, Requires: "uv (Python)",
		Fields: []Field{{Key: "repo", Label: "Project folder", Kind: FieldFolder, Placeholder: "C:\\Projects\\my-app",
			Help: "The folder that contains the .git directory."}},
		Agents: codeAgents,
	},
	{
		ID: "filesystem", Name: "Files on this PC", Category: "Code", Icon: "📁", Transport: store.TransportStdio,
		Description: "Read, search and edit files in one folder you choose, such as a project or a documents folder.",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "{folder}"}, Requires: "Node.js",
		Fields: []Field{{Key: "folder", Label: "Folder agents may use", Kind: FieldFolder, Placeholder: "C:\\Projects\\my-app",
			Help: "Agents can read and change files only inside this folder."}},
		Agents: []string{"engineer", "docs-writer", "writer", "data-analyst"},
	},
	{
		ID: "context7", Name: "Library docs (Context7)", Category: "Code", Icon: "📚", Transport: store.TransportHTTP,
		Description: "Up-to-date documentation and code examples for programming libraries.",
		URL:         "https://mcp.context7.com/mcp", Agents: []string{"engineer", "docs-writer", "pr-reviewer"},
	},
	{
		ID: "sqlite", Name: "SQLite database", Category: "Data", Icon: "🗄️", Transport: store.TransportStdio,
		Description: "Query and explore a SQLite database file.",
		Command:     "uvx", Args: []string{"mcp-server-sqlite", "--db-path", "{db}"}, Requires: "uv (Python)",
		Fields: []Field{{Key: "db", Label: "Database file", Kind: FieldFolder, Placeholder: "C:\\Data\\app.db"}},
		Agents: []string{"data-analyst", "analyst", "engineer"},
	},
	{
		ID: "notion", Name: "Notion", Category: "Productivity", Icon: "📓", Transport: store.TransportStdio,
		Description: "Search, read and update the pages and databases you share with the integration.",
		Command:     "npx", Args: []string{"-y", "@notionhq/notion-mcp-server"}, SecretEnv: []string{"NOTION_TOKEN"}, Requires: "Node.js",
		Fields: []Field{{Key: "token", Label: "Integration token", Kind: FieldSecret, Secret: "NOTION_TOKEN", Placeholder: "ntn_…",
			Help: "Create an internal integration, then share the pages you want agents to see with it."}},
		Agents: []string{"manager", "executive-assistant", "product-manager", "ops-planner"}, Link: "https://www.notion.so/profile/integrations",
	},
	{
		ID: "slack", Name: "Slack", Category: "Productivity", Icon: "💬", Transport: store.TransportStdio,
		Description: "Read channels and threads, and post messages. Agents ask your approval before they post.",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-slack"}, SecretEnv: []string{"SLACK_BOT_TOKEN"},
		Env: map[string]string{"SLACK_TEAM_ID": "{team}"}, Requires: "Node.js",
		Fields: []Field{
			{Key: "token", Label: "Bot token", Kind: FieldSecret, Secret: "SLACK_BOT_TOKEN", Placeholder: "xoxb-…",
				Help: "From your Slack app's OAuth page, with the channels:read, channels:history and chat:write scopes."},
			{Key: "team", Label: "Workspace ID", Kind: FieldText, Placeholder: "T0123456789", Help: "Starts with T; find it in the Slack admin pages."},
		},
		Agents: []string{"manager", "executive-assistant", "social-planner"}, Link: "https://api.slack.com/apps",
	},
	{
		ID: "linear", Name: "Linear", Category: "Productivity", Icon: "📐", Transport: store.TransportHTTP,
		Description: "Issues, projects and cycles in Linear.",
		URL:         "https://mcp.linear.app/mcp", AuthSecret: "LINEAR_API_KEY",
		Fields: []Field{{Key: "token", Label: "API key", Kind: FieldSecret, Secret: "LINEAR_API_KEY", Placeholder: "lin_api_…"}},
		Agents: []string{"manager", "product-manager", "bug-triager", "engineer"}, Link: "https://linear.app/settings/account/security",
	},
	{
		ID: "brave-search", Name: "Brave Search", Category: "Web", Icon: "🦁", Transport: store.TransportStdio,
		Description: "Web and news search through the Brave Search API.",
		Command:     "npx", Args: []string{"-y", "@brave/brave-search-mcp-server"}, SecretEnv: []string{"BRAVE_API_KEY"}, Requires: "Node.js",
		Fields: []Field{{Key: "key", Label: "API key", Kind: FieldSecret, Secret: "BRAVE_API_KEY", Placeholder: "BSA…"}},
		Agents: []string{"researcher", "fact-checker", "market-researcher", "competitor-watcher"}, Link: "https://brave.com/search/api/",
	},
	{
		ID: "fetch", Name: "Web pages (Fetch)", Category: "Web", Icon: "🌐", Transport: store.TransportStdio,
		Description: "Fetch web pages and convert them to Markdown for agents to read.",
		Command:     "uvx", Args: []string{"mcp-server-fetch"}, Requires: "uv (Python)",
		Agents: []string{"researcher", "fact-checker", "market-researcher"},
	},
	{
		ID: "playwright", Name: "Browser (Playwright)", Category: "Web", Icon: "🧭", Transport: store.TransportStdio,
		Description: "Drive a real web browser: open pages, click, fill forms and take snapshots.",
		Command:     "npx", Args: []string{"-y", "@playwright/mcp@latest"}, Requires: "Node.js",
		Agents: []string{"researcher", "engineer"},
	},
	{
		ID: "time", Name: "Time and time zones", Category: "Utilities", Icon: "🕒", Transport: store.TransportStdio,
		Description: "Current time and time-zone conversions.",
		Command:     "uvx", Args: []string{"mcp-server-time"}, Requires: "uv (Python)", Agents: []string{"executive-assistant", "travel-planner"},
	},
	{
		ID: "sequential-thinking", Name: "Step-by-step thinking", Category: "Utilities", Icon: "🧩", Transport: store.TransportStdio,
		Description: "Structured reasoning for complex problems. Uses more tokens; best for hard questions.",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-sequential-thinking"}, Requires: "Node.js",
		Agents: []string{"analyst"},
	},
}

func MCPTemplates() []MCPTemplate { return append([]MCPTemplate(nil), mcpTemplates...) }

func MCPTemplateByID(id string) (MCPTemplate, bool) {
	for _, t := range mcpTemplates {
		if t.ID == id {
			return t, true
		}
	}
	return MCPTemplate{}, false
}
