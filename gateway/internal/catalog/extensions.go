package catalog

import "github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"

// MCPTemplate is a preset for a well-known MCP server.
type MCPTemplate struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	SecretEnv   []string          `json:"secret_env"`
	URL         string            `json:"url,omitempty"`
	AuthSecret  string            `json:"auth_secret,omitempty"`
	// Requires names what must be installed on this PC (e.g. "Node.js").
	Requires string `json:"requires,omitempty"`
	// Setup explains what the owner should fill in before adding it.
	Setup string `json:"setup,omitempty"`
}

func (t MCPTemplate) Server() store.MCPServer {
	return store.MCPServer{
		ID: t.ID, Name: t.Name, Description: t.Description, Transport: t.Transport, Command: t.Command,
		Args: append([]string(nil), t.Args...), Env: copyMap(t.Env), SecretEnv: append([]string(nil), t.SecretEnv...),
		URL: t.URL, AuthSecret: t.AuthSecret, TemplateID: t.ID, Enabled: true,
	}
}

func copyMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

var mcpTemplates = []MCPTemplate{
	{
		ID: "filesystem", Name: "Filesystem", Transport: store.TransportStdio,
		Description: "Read, search and edit files in folders you choose on this PC.",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "C:\\Users\\Public\\Documents"},
		Requires: "Node.js", Setup: "Replace the last argument with the folder(s) agents may use.",
	},
	{
		ID: "fetch", Name: "Fetch", Transport: store.TransportStdio,
		Description: "Fetch web pages and convert them to Markdown for agents to read.",
		Command:     "uvx", Args: []string{"mcp-server-fetch"}, Requires: "uv (Python)",
	},
	{
		ID: "time", Name: "Time", Transport: store.TransportStdio,
		Description: "Current time and time-zone conversions.",
		Command:     "uvx", Args: []string{"mcp-server-time"}, Requires: "uv (Python)",
	},
	{
		ID: "memory-graph", Name: "Knowledge graph memory", Transport: store.TransportStdio,
		Description: "A persistent knowledge graph of people, projects and relationships.",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-memory"}, Requires: "Node.js",
	},
	{
		ID: "sequential-thinking", Name: "Sequential thinking", Transport: store.TransportStdio,
		Description: "Structured step-by-step reasoning for complex problems.",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-sequential-thinking"}, Requires: "Node.js",
	},
	{
		ID: "github", Name: "GitHub", Transport: store.TransportHTTP,
		Description: "Repositories, issues, pull requests and code search on GitHub.",
		URL:         "https://api.githubcopilot.com/mcp/", AuthSecret: "GITHUB_PERSONAL_ACCESS_TOKEN",
		Setup: "Needs a GitHub personal access token, stored encrypted as GITHUB_PERSONAL_ACCESS_TOKEN.",
	},
	{
		ID: "brave-search", Name: "Brave Search", Transport: store.TransportStdio,
		Description: "Web and news search through the Brave Search API.",
		Command:     "npx", Args: []string{"-y", "@brave/brave-search-mcp-server"}, SecretEnv: []string{"BRAVE_API_KEY"},
		Requires: "Node.js", Setup: "Needs a Brave Search API key, stored encrypted as BRAVE_API_KEY.",
	},
	{
		ID: "playwright", Name: "Browser (Playwright)", Transport: store.TransportStdio,
		Description: "Drive a real web browser: open pages, click, fill forms and take snapshots.",
		Command:     "npx", Args: []string{"-y", "@playwright/mcp@latest"}, Requires: "Node.js",
	},
	{
		ID: "context7", Name: "Context7 docs", Transport: store.TransportHTTP,
		Description: "Up-to-date documentation and code examples for programming libraries.",
		URL:         "https://mcp.context7.com/mcp",
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

// SkillTemplate is a ready-made skill (SKILL.md content).
type SkillTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func (t SkillTemplate) Skill() store.Skill {
	return store.Skill{ID: t.ID, Name: t.Name, Description: t.Description, Content: t.Content, TemplateID: t.ID, Enabled: true}
}

var skillTemplates = []SkillTemplate{
	{
		ID: "email-writing", Name: "email-writing",
		Description: "Use when drafting or replying to an email: structure, tone and a ready-to-send result.",
		Content: `# Email writing

1. Work out the goal of the email and what the reader must do or know.
2. Subject line: specific and under 8 words (only for new emails).
3. Open with the point in the first sentence. No "I hope this finds you well".
4. Keep it short: 3 short paragraphs at most; bullets for several items or questions.
5. Make any request explicit, with a date if timing matters.
6. Match the owner's usual tone and sign-off (check what the office remembers).
7. Never invent facts, prices, dates or commitments. Mark anything to fill in as [like this].

Deliver the email ready to paste: subject (if new) and body.`,
	},
	{
		ID: "meeting-notes", Name: "meeting-notes",
		Description: "Use when turning meeting notes or a transcript into a summary with decisions and action items.",
		Content: `# Meeting notes

Produce, in this order:

## Summary
Two or three sentences on what the meeting achieved.

## Decisions
Bullet list. Only decisions actually made.

## Action items
| Owner | Action | Due |
Use "Unassigned" or "No date" when not stated. Never invent owners or dates.

## Open questions
Anything left unresolved.

Keep the speakers' meaning; remove filler and small talk.`,
	},
	{
		ID: "research-report", Name: "research-report",
		Description: "Use for research questions: how to search, weigh sources and write a cited report.",
		Content: `# Research report

Method:
- Break the question into 2-4 sub-questions and search for each.
- Prefer primary, official and recent sources; cross-check important numbers in two places.
- Note the publication date of anything time-sensitive.

Report format:
1. **Answer**: the direct answer in 2-3 sentences.
2. **Key findings**: bullets, each with its source as [title](url).
3. **Comparison table** when comparing options.
4. **Caveats**: what is uncertain, disputed or likely to change.

Never cite a source you did not open. Say "not found" rather than guess.`,
	},
	{
		ID: "daily-briefing", Name: "daily-briefing",
		Description: "Use for a morning or daily briefing: priorities, schedule, deadlines and one focus for the day.",
		Content: `# Daily briefing

Keep it scannable in under a minute:

**Today's focus**: one sentence.
**Top 3 priorities**: numbered, each with the first concrete step.
**Schedule / deadlines**: times and due items, earliest first.
**Waiting on others**: who and what.
**Heads-up**: risks, conflicts or anything due in the next 3 days.

Use only information you have; say "nothing on record" for empty sections rather than inventing items.`,
	},
	{
		ID: "weekly-review", Name: "weekly-review",
		Description: "Use for a weekly review or plan: wins, open loops, and priorities for next week.",
		Content: `# Weekly review

## Wins this week
## Still open (and why)
## Next week's top priorities
Numbered, at most five, each with a clear done-criterion.
## Calendar and deadlines next week
## One thing to stop, start or keep

Be honest and specific; turn vague items into next actions.`,
	},
	{
		ID: "social-post", Name: "social-post",
		Description: "Use when writing social media posts: platform formats, hooks and calls to action.",
		Content: `# Social posts

- **LinkedIn**: hook in the first line, short paragraphs, one insight, a question or CTA at the end; 3 hashtags max.
- **X / Threads**: under 280 characters per post; one idea; threads numbered when longer.
- **Instagram**: caption with a hook, value, CTA; up to 5 relevant hashtags at the end.

Always give 2-3 variants with different hooks. Avoid clichés ("game-changer", "in today's fast-paced world").`,
	},
	{
		ID: "code-review", Name: "code-review",
		Description: "Use when reviewing or writing code: correctness, security, clarity and tests checklist.",
		Content: `# Code review checklist

1. **Correctness**: does it do what was asked? Edge cases: empty input, errors, limits, concurrency.
2. **Security**: untrusted input validated; no secrets in code; safe file paths and queries.
3. **Clarity**: clear names, small functions, comments only where intent is not obvious.
4. **Tests**: what should be tested and how; never claim tests passed if you did not run them.
5. **Running it**: exact commands and prerequisites.

Report issues by severity (must fix, should fix, nit) and include the corrected code.`,
	},
	{
		ID: "decision-memo", Name: "decision-memo",
		Description: "Use when the owner must choose between options: a one-page decision memo with a recommendation.",
		Content: `# Decision memo

**Decision needed**: one sentence.
**Recommendation**: the option you recommend and the main reason.
**Options**
| Option | Pros | Cons | Cost / effort | Risk |
**What would change the recommendation**
**Next step**: the first action and who takes it.

Be decisive; state assumptions explicitly.`,
	},
}

func SkillTemplates() []SkillTemplate { return append([]SkillTemplate(nil), skillTemplates...) }

func SkillTemplateByID(id string) (SkillTemplate, bool) {
	for _, t := range skillTemplates {
		if t.ID == id {
			return t, true
		}
	}
	return SkillTemplate{}, false
}

// PluginTemplate is an installable pack of agents, MCP servers and skills.
type PluginTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Agents      []string `json:"agents"`      // agent template ids
	MCPServers  []string `json:"mcp_servers"` // MCP template ids
	Skills      []string `json:"skills"`      // skill template ids
	Requires    string   `json:"requires,omitempty"`
}

var pluginTemplates = []PluginTemplate{
	{
		ID: "developer-toolkit", Name: "Developer toolkit", Version: "1.0",
		Description: "A code reviewer with GitHub access and up-to-date library docs.",
		Agents:      []string{"engineer"}, MCPServers: []string{"github", "context7"}, Skills: []string{"code-review"},
		Requires: "A GitHub personal access token",
	},
	{
		ID: "research-desk", Name: "Research desk", Version: "1.0",
		Description: "A fact checker with page fetching and the research-report method.",
		Agents:      []string{"fact-checker"}, MCPServers: []string{"fetch"}, Skills: []string{"research-report"},
		Requires: "uv (Python)",
	},
	{
		ID: "inbox-zero", Name: "Inbox & meetings", Version: "1.0",
		Description: "An email assistant with skills for replies and meeting notes.",
		Agents:      []string{"email-assistant"}, Skills: []string{"email-writing", "meeting-notes"},
	},
	{
		ID: "daily-planner", Name: "Daily planner", Version: "1.0",
		Description: "An executive assistant with daily briefings, weekly reviews and time-zone tools.",
		Agents:      []string{"executive-assistant"}, MCPServers: []string{"time"},
		Skills: []string{"daily-briefing", "weekly-review", "meeting-notes"}, Requires: "uv (Python)",
	},
	{
		ID: "content-studio", Name: "Content studio", Version: "1.0",
		Description: "A social media manager and a marketing strategist who plan and write content together.",
		Agents:      []string{"social-media", "marketing-strategist"}, Skills: []string{"social-post"},
	},
}

func PluginTemplates() []PluginTemplate { return append([]PluginTemplate(nil), pluginTemplates...) }

// PluginBundle expands a plugin template into installable records. Each agent
// gets the plugin's MCP servers and its own template skills.
func PluginBundle(id string) (store.PluginBundle, bool) {
	for _, p := range pluginTemplates {
		if p.ID != id {
			continue
		}
		b := store.PluginBundle{Name: p.Name, Description: p.Description, Version: p.Version}
		for _, m := range p.MCPServers {
			if t, ok := MCPTemplateByID(m); ok {
				b.MCPServers = append(b.MCPServers, t.Server())
			}
		}
		skills := map[string]bool{}
		addSkill := func(s string) {
			if t, ok := SkillTemplateByID(s); ok && !skills[s] {
				skills[s] = true
				b.Skills = append(b.Skills, t.Skill())
			}
		}
		for _, s := range p.Skills {
			addSkill(s)
		}
		for _, a := range p.Agents {
			t, ok := AgentTemplateByID(a)
			if !ok {
				continue
			}
			agent := t.Agent()
			agent.MCPServers = append([]string(nil), p.MCPServers...)
			for _, s := range t.Skills {
				addSkill(s)
			}
			agent.Skills = append(append([]string(nil), p.Skills...), t.Skills...)
			b.Agents = append(b.Agents, agent)
		}
		return b, true
	}
	return store.PluginBundle{}, false
}
