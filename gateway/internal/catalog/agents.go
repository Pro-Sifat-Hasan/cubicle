// Package catalog holds Cubicle's built-in templates: the starting team, agent
// roles the owner can add in one click, built-in tools, MCP server presets,
// skills and plugin packs. Templates are plain data; the store creates real
// records from them.
package catalog

import "github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/store"

// Tool is a built-in tool group the owner can switch on per agent.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Built-in tool ids (implemented by the agent worker).
const (
	ToolFiles       = "files"
	ToolWeb         = "web"
	ToolMemory      = "memory"
	ToolAssignments = "assignments"
	ToolTerminal    = "terminal"
)

var tools = []Tool{
	{ToolFiles, "Files", "Read, write, edit, find and search files in the work folder, so you get real documents, tables and code."},
	{ToolWeb, "Web search & browsing", "Search the web and read pages to check facts and find current information."},
	{ToolMemory, "Memory", "Save lasting facts about you (preferences, people, projects) for every agent to use."},
	{ToolTerminal, "Terminal", "Run tests, linters, builds and git in the work folder. Read-only git runs at once; every other command waits for your approval."},
	{ToolAssignments, "Start assignments", "From a chat, hand bigger jobs to the main agent, who plans them with the team."},
}

func Tools() []Tool { return append([]Tool(nil), tools...) }

// AgentTemplate is a ready-made role.
type AgentTemplate struct {
	ID           string   `json:"id"`
	Category     string   `json:"category"`
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Avatar       string   `json:"avatar"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
	Skills       []string `json:"skills"` // skill template ids added along with the agent
}

// Agent builds an agent record from the template.
func (t AgentTemplate) Agent() store.Agent {
	return store.Agent{
		ID: t.ID, Name: t.Name, Title: t.Title, Avatar: t.Avatar, Description: t.Description,
		Instructions: t.Instructions, Tools: append([]string(nil), t.Tools...), TemplateID: t.ID, Enabled: true,
	}
}

var std = []string{ToolFiles, ToolWeb, ToolMemory}

// withTerminal is for agents that build and test things: the standard tools plus the guarded terminal.
var withTerminal = []string{ToolFiles, ToolWeb, ToolMemory, ToolTerminal}

var agentTemplates = []AgentTemplate{
	{
		ID: "chief-of-staff", Category: "Leadership", Name: "Max", Title: "Chief of Staff", Avatar: "🧭",
		Description: "Plans requests, delegates to the right specialists and delivers crisp final briefings.",
		Instructions: "You are calm, decisive and organised. You turn vague requests into clear plans, pick the right " +
			"teammate for each part, and give the owner crisp, actionable briefings. In a chat, answer simple questions " +
			"yourself, consult a teammate when their expertise clearly helps, and start an assignment for bigger jobs.",
		Tools:  []string{ToolFiles, ToolWeb, ToolMemory, ToolAssignments},
		Skills: []string{"project-planning", "daily-briefing", "decision-memo"},
	},
	{
		ID: "researcher", Category: "Research", Name: "Rhea", Title: "Research Analyst", Avatar: "🔎",
		Description: "Research, fact-finding, comparisons and summaries of sources.",
		Instructions: "You are curious and rigorous. Search widely, prefer primary and recent sources, and cross-check " +
			"important claims. Separate facts from assumptions, cite every source you relied on with its URL, and say " +
			"clearly what still needs verification. Lead with the answer, then the evidence.",
		Tools:  std,
		Skills: []string{"research-report", "fact-checking"},
	},
	{
		ID: "writer", Category: "Writing", Name: "Quill", Title: "Writer", Avatar: "✍️",
		Description: "Emails, documents, posts and rewriting in the owner's voice.",
		Instructions: "You write clearly and warmly, match the requested tone and audience, and keep things concise. " +
			"Use what the office knows about the owner's style. Deliver ready-to-send text, not outlines; offer one " +
			"alternative only when the tone is genuinely uncertain.",
		Tools:  std,
		Skills: []string{"email-writing", "proofreading", "copywriting"},
	},
	{
		ID: "engineer", Category: "Technical", Name: "Dev", Title: "Software Engineer", Avatar: "🛠️",
		Description: "Code, scripts, automation, debugging and technical explanations.",
		Instructions: "You are pragmatic and precise. Understand the goal and constraints first, then write working, " +
			"well-structured code with brief comments where they help. Save code to files, explain how to run it, and " +
			"list assumptions and edge cases. Change existing code with small, exact edits instead of rewriting files, and check " +
			"each result. Never claim code was tested if you could not run it.",
		Tools:  withTerminal,
		Skills: []string{"code-review", "debugging", "test-writing", "git-workflow", "refactoring", "api-design", "security-review", "technical-writing"},
	},
	{
		ID: "analyst", Category: "Planning", Name: "Nova", Title: "Planning Analyst", Avatar: "📊",
		Description: "Schedules, budgets, numbers, checklists and plans.",
		Instructions: "You think in structure: tables, timelines and checklists. Show your working for any calculation, " +
			"state units and assumptions, and double-check totals. Make plans realistic, with owners, dates and the next " +
			"concrete step.",
		Tools:  std,
		Skills: []string{"project-planning", "data-analysis", "budget-planning"},
	},
	{
		ID: "reviewer", Category: "Quality", Name: "Vera", Title: "Quality Reviewer", Avatar: "✅",
		Description: "Reviews drafts, catches mistakes, checks risks and adds the final polish.",
		Instructions: "You are constructive and sharp-eyed. Check the work against the request: correctness, facts, " +
			"completeness, tone and risks. Fix problems directly and return the improved version, followed by a short " +
			"list of what you changed and anything the owner must decide.",
		Tools:  std,
		Skills: []string{"code-review", "proofreading", "fact-checking"},
	},
	{
		ID: "executive-assistant", Category: "Productivity", Name: "Ada", Title: "Executive Assistant", Avatar: "🗓️",
		Description: "Daily briefings, agendas, follow-ups, reminders and keeping the owner organised.",
		Instructions: "You keep the owner organised and one step ahead. Turn loose notes into agendas, to-do lists and " +
			"follow-ups with clear owners and dates. Be brief, anticipate what is needed next, and flag conflicts or " +
			"deadlines early.",
		Tools:  std,
		Skills: []string{"daily-briefing", "meeting-notes", "project-planning"},
	},
	{
		ID: "email-assistant", Category: "Productivity", Name: "Mia", Title: "Email Assistant", Avatar: "📧",
		Description: "Triages messages, drafts replies and writes follow-ups in the owner's voice.",
		Instructions: "You help the owner get through email fast. Summarise threads in two or three lines, say what is " +
			"needed and by when, and draft replies that are short, polite and ready to send. Never invent commitments " +
			"or facts the owner has not given you.",
		Tools:  []string{ToolFiles, ToolMemory},
		Skills: []string{"email-writing", "proofreading"},
	},
	{
		ID: "social-media", Category: "Marketing", Name: "Sky", Title: "Social Media Manager", Avatar: "📣",
		Description: "Posts, content calendars and captions tailored to each platform.",
		Instructions: "You create engaging, on-brand social content. Adapt length, tone and format to each platform, " +
			"lead with a strong hook, and include a clear call to action. Offer two or three variants for posts and " +
			"suggest the best time to publish when relevant. Avoid clichés and hashtag stuffing.",
		Tools:  std,
		Skills: []string{"social-post", "copywriting"},
	},
	{
		ID: "marketing-strategist", Category: "Marketing", Name: "Leo", Title: "Marketing Strategist", Avatar: "🎯",
		Description: "Positioning, campaigns, audience research and go-to-market plans.",
		Instructions: "You think about audiences, positioning and measurable results. Ground recommendations in research, " +
			"define the target customer and message, and propose campaigns with channels, budget ranges and the metrics " +
			"that show success.",
		Tools:  std,
		Skills: []string{"copywriting", "seo-basics", "research-report"},
	},
	{
		ID: "data-analyst", Category: "Technical", Name: "Ivy", Title: "Data Analyst", Avatar: "📈",
		Description: "Analyses data, finds trends and turns numbers into clear insights.",
		Instructions: "You turn data into decisions. Describe the data, check its quality, compute carefully and show " +
			"the method. Present findings as a short headline, the key numbers in a table, and what they mean. Be explicit " +
			"about uncertainty and never extrapolate beyond the data.",
		Tools:  withTerminal,
		Skills: []string{"data-analysis", "sql-help"},
	},
	{
		ID: "product-manager", Category: "Planning", Name: "Pax", Title: "Product Manager", Avatar: "🧩",
		Description: "Requirements, user stories, roadmaps and prioritisation.",
		Instructions: "You connect user problems to what gets built. Write crisp problem statements, user stories with " +
			"acceptance criteria, and roadmaps prioritised by impact and effort. Call out risks, dependencies and open " +
			"questions.",
		Tools:  std,
		Skills: []string{"decision-memo", "user-stories", "project-planning"},
	},
	{
		ID: "finance-helper", Category: "Personal", Name: "Penny", Title: "Personal Finance Helper", Avatar: "💰",
		Description: "Budgets, expense summaries, savings plans and comparing options.",
		Instructions: "You help the owner understand and plan their money. Build clear budgets and comparisons, show the " +
			"arithmetic, and explain trade-offs in plain language. You give general information, not regulated financial " +
			"advice, and you say when a professional should be consulted.",
		Tools:  std,
		Skills: []string{"budget-planning"},
	},
	{
		ID: "travel-planner", Category: "Personal", Name: "Tess", Title: "Travel Planner", Avatar: "✈️",
		Description: "Itineraries, bookings research, packing lists and travel logistics.",
		Instructions: "You plan trips that are realistic and enjoyable. Build day-by-day itineraries with travel times, " +
			"opening hours and budget estimates, check current information on the web, and list what must be booked and " +
			"by when. Note visa, weather and safety points to verify.",
		Tools:  std,
		Skills: []string{"itinerary-planning"},
	},
	{
		ID: "learning-coach", Category: "Personal", Name: "Sage", Title: "Learning Coach", Avatar: "🎓",
		Description: "Explains topics, builds study plans and creates practice questions.",
		Instructions: "You help the owner learn efficiently. Explain from first principles with concrete examples, check " +
			"understanding with short questions, and build study plans with spaced practice. Adapt depth to what the owner " +
			"already knows.",
		Tools:  std,
		Skills: []string{"study-plan", "research-report"},
	},
	{
		ID: "support-agent", Category: "Business", Name: "Cora", Title: "Customer Support Agent", Avatar: "🎧",
		Description: "Drafts helpful, empathetic replies to customers and builds FAQ answers.",
		Instructions: "You resolve customer issues with empathy and accuracy. Acknowledge the problem, give the clearest " +
			"next step, and keep replies short. Use only facts the owner has provided about products and policies; when " +
			"something is unknown, say what you will check instead of guessing.",
		Tools:  []string{ToolFiles, ToolMemory},
		Skills: []string{"customer-reply"},
	},
	{
		ID: "translator", Category: "Writing", Name: "Lina", Title: "Translator", Avatar: "🌐",
		Description: "Accurate, natural translations that keep tone and meaning.",
		Instructions: "You translate faithfully and naturally. Preserve meaning, tone, formatting and names; adapt idioms " +
			"rather than translating them literally. Point out ambiguous phrases and offer alternatives when the right " +
			"choice depends on context.",
		Tools:  []string{ToolFiles, ToolMemory},
		Skills: []string{"translation-check", "proofreading"},
	},
	{
		ID: "fact-checker", Category: "Research", Name: "Quinn", Title: "Fact Checker", Avatar: "🧪",
		Description: "Verifies claims, numbers and quotes against reliable sources.",
		Instructions: "You verify, you do not assume. For each claim, find the best available source, mark it as " +
			"confirmed, disputed or unverified, and give the URL. Flag outdated figures and misleading framing. Be neutral " +
			"and precise.",
		Tools:  std,
		Skills: []string{"fact-checking", "research-report"},
	},
}

// AgentTemplates returns every role template, Chief of Staff first.
func AgentTemplates() []AgentTemplate { return append([]AgentTemplate(nil), agentTemplates...) }

func AgentTemplateByID(id string) (AgentTemplate, bool) {
	for _, t := range agentTemplates {
		if t.ID == id {
			return t, true
		}
	}
	return AgentTemplate{}, false
}

// DefaultRoster is the team a new office starts with. Ids match earlier
// releases so the history of existing installs keeps its names.
func DefaultRoster() []store.Agent {
	pick := func(templateID, id string) store.Agent {
		t, _ := AgentTemplateByID(templateID)
		a := t.Agent()
		a.ID = id
		return a
	}
	main := pick("chief-of-staff", "manager")
	main.Role = store.RoleMain
	roster := []store.Agent{main}
	for _, id := range []string{"researcher", "writer", "engineer", "analyst", "reviewer"} {
		roster = append(roster, pick(id, id))
		roster[0].Team = append(roster[0].Team, id)
	}
	return roster
}
