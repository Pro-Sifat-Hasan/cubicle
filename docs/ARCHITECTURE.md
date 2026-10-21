# Architecture

Cubicle runs as three cooperating processes on your machine.

```mermaid
flowchart LR
    UI["Cubicle.exe<br/>C# · WPF"] -- "REST + live events<br/>(admin token)" --> GW
    subgraph GW["cubicle-gateway · Go"]
        API["Owner API"] --- DB[("SQLite")]
        SCH["Automation scheduler"] --- DB
        SEC["Encrypted secrets"] --- DB
        SUP["Worker supervisor"]
    end
    SUP -- "starts, configures, restarts" --> AG
    AG["cubicle-agents · Python<br/>Pydantic AI · two desks per agent"] -- "Worker API<br/>(worker token)" --> GW
    AG -- "official SDKs / CLI" --> LLM(("Claude · OpenAI · Gemini<br/>Ollama · Claude Code"))
    AG -- "MCP (stdio / HTTP)" --> MCP(("Your apps<br/>and services"))
```

| Process | Responsibility |
|---|---|
| **Desktop app** | What you see: tray icon, Today, Inbox (chat), Agents, Automations, Tools & skills, Memory, Activity and Settings. Holds no state of its own. |
| **Gateway** | The single source of truth. It stores agents, MCP servers, skills, plugins, conversations and work in SQLite, serves the owner and worker APIs, fires automations, enforces the token budget, encrypts secrets, and starts and restarts the agent worker. |
| **Agents** | The AI staff, built on [Pydantic AI](https://ai.pydantic.dev). Every agent works at two *desks*: one for tasks and one for chats, so a long task never blocks a reply. It claims work, does it with its model, tools, MCP servers and skills, and reports the result. |

## Agents

You manage agents in the app; the gateway stores them and the worker follows changes within moments
(it long-polls a revision counter), so editing an agent never interrupts work in progress.

| Setting | Effect |
|---|---|
| Instructions | The agent's system prompt: how it thinks and works |
| Good at | A short description; the main agent picks teammates by it |
| Model, effort | Override the office defaults from Settings |
| Tools | Built-in tools: files, web, memory, and (from chats) starting assignments |
| MCP servers | External tools the agent may use |
| Skills | Instructions the agent loads when a task matches (SKILL.md format) |
| Team | Agents it can hand work to or consult |

Exactly one agent is the **main agent**. It receives assignments and automations, plans them with
its team, and writes the final result. A new office starts with Max (main) and five specialists;
the Agents page adds more from 18 role templates in one click.

## How an assignment is done

```
assignment (you → main agent)
 ├── subtask → teammate        ┐ run in parallel unless one depends on another
 ├── subtask → teammate        ┘
 ├── review  → reviewer        (for documents, emails or code you will rely on)
 └── wrap-up → main agent      (runs once every subtask is done)
```

1. **Plan.** The main agent turns your request into a plan. Pydantic AI validates it against a
   JSON schema and asks the model to fix invalid answers. The worker keeps only real team members and
   valid dependencies, and the gateway checks again before creating the subtasks and the wrap-up in
   one transaction. A main agent without a team does the work itself.
2. **Work.** A teammate claims a subtask once its dependencies are finished. The claim includes
   the overall assignment, teammates' results and any answers you gave. The teammate works in a
   tool loop with its own instructions, tools, MCP servers and skills, and may consult its own teammates.
3. **Ask and approve.** If an agent is blocked, it asks you through the Inbox. Before anything with
   consequences outside its workspace (sending, publishing, deleting, spending, changing an account),
   it asks for your approval and acts only if you approve. The task pauses and resumes automatically
   with your answer. Each task can ask at most two questions.
4. **Wrap up.** The main agent merges the results into one deliverable, references the files that
   were produced, and lists anything that needs you. Completing the wrap-up completes the assignment.

You can also give an assignment straight to one agent, which then does it alone.

A failed subtask fails its assignment and cancels the remaining work, so nothing waits forever.

## Onboarding, passions and professions

First run walks the owner through five screens: name the chief of staff, pick a profession, pick a
passion (or type their own), watch the team get hired, and start a first mission.

- A **profession** pack decides the team (extra specialists), their routines and the starter
  missions. Hired teammates join the main agent's team; routines marked `enabled` are switched on,
  the rest wait in Automations.
- A **passion** pack decides the main agent's voice (`system_prompt_style`, appended to its
  instructions), the greetings on Today, the daily feed queries and a colour swatch. The app itself always keeps the Cubicle amber.
  Only the main agent takes the voice; specialists stay professional.
- A **custom** passion is written by the main agent: the gateway queues a hidden `persona` task, the
  worker answers with schema-validated JSON, and the gateway clamps it before anything is stored.
  With no model available a plain persona is used instead, so setup never blocks.
- The owner's profession and stack are saved as memories, so every agent sees them.

Packs are JSON in `gateway/internal/catalog/packs/`; adding one needs no code.

## Chat

Each message you send in the Inbox becomes a chat task for that agent, with the conversation so far.
While it works, the agent streams progress ("Searching the web…") and the reply as it is written.
In a chat an agent can consult teammates (`ask_teammate`: the teammate runs with its own
configuration and returns its answer) and, with the *assignments* tool, start a full assignment for
the team.

Everything an agent does is recorded as **steps**: its thoughts (`thinking`), the remarks it makes between
tool calls (`note`) and each tool call (`tool`, with its input and, when it ends, its output and status). The
worker reports a step each time it changes (throttled), the gateway stores it (cut to sensible lengths, at most
400 per task) and relays it as a `task.step` event, so the Inbox shows a live timeline and keeps it under the
reply as "How it was done". A teammate consulted during a reply reports its steps on the same task, marked with
its own id.

A chat reply can pause too: an agent that needs an answer or an approval parks its reply, the request
appears in the chat and in the Inbox's **Needs you** thread, and the same reply resumes with your
decision. Every reply keeps a trail of the tools it used ("How it was done"), and an assignment started
from a chat is shown under the reply that started it, with its status, plan, trail and result.

## Providers

The gateway turns the provider you choose in Settings into environment variables for the worker,
and restarts the worker whenever settings or keys change.

| Adapter | Used for | How it works |
|---|---|---|
| Pydantic AI | Claude API, OpenAI, Gemini, OpenRouter, Ollama, custom servers | One engine: tool loop, validated structured output with retries, streaming, usage limits. Claude gets adaptive thinking, effort control, prompt caching and server-side web search and fetch; OpenAI gets native web search; other servers get DuckDuckGo search and Cubicle's guarded fetch |
| Claude Code | Your Claude subscription | Runs your installed `claude` CLI headlessly in the work folder and streams its work (`stream-json`). It gets its own file and web tools (never a shell), limited to what the agent may do, and Cubicle's own tools over a local MCP bridge. It is isolated from your personal Claude Code setup. MCP servers are passed with `--mcp-config` |
| Demo | Trying Cubicle out | Sample answers with no model |

**Test connection** in Settings runs `cubicle-agents models`, which asks the provider for its model
list. A successful list confirms the key works.

## Tools

| Tool | Purpose | Safeguards |
|---|---|---|
| `write_file`, `read_file`, `list_files` | Produce deliverables in `workspace/<assignment or chat>/` | Paths are confined to that folder, with size and file-count limits |
| web search and fetch | Check facts and read pages | Cubicle's own fetch allows only public http(s) addresses, re-checks every redirect, and limits size |
| `remember` | Save a durable fact about you to office memory | Length limit, de-duplication, and a cap of 300 facts |
| `load_skill` | Load a skill's full instructions | Only the agent's own skills |
| `ask_teammate` | Consult a teammate | Only the agent's team; a consulted teammate cannot consult further |
| `start_assignment` | Start a team assignment from a chat | Only in chats, only with the *assignments* tool |
| `edit_file`, `find_files`, `grep_files` | Change part of a file, find files by name, search file contents | Same workspace confinement as the file tools; edits need an exact, unique match |
| `run_command` | Run tests, linters, builds and git in the workspace (the *terminal* tool, off by default except for developer roles) | No shell, an allowlist of programs, a minimal environment with no secrets, a time limit and an output cap. Read-only git runs at once; everything else needs your approval of that exact command |
| `forget` | Remove a wrong or outdated remembered fact | Matches the closest stored fact |
| `ask_owner` | Ask you a question through the Inbox, in an assignment or a chat | Ends the turn; at most two questions per task |
| `request_approval` | Ask you to approve one specific action before it happens | Ends the turn; the agent acts only on "Approved" and only on that action |

The Demo provider cannot run Cubicle's tools and uses plain-text directives instead: a line starting
with `REMEMBER:` saves a fact, and `ASK_OWNER:` asks a question. Its skills are included in full in the prompt.

**Claude Code and Cubicle's tools.** For one run the worker serves every tool Claude Code does not have
natively (`ask_teammate`, `start_assignment`, `remember`, `forget`, `load_skill`, `ask_owner`,
`request_approval`, `run_command`) as an MCP server on `127.0.0.1`, guarded by a random bearer token that
exists only for that run. Claude Code's own `Read`, `Write`, `Edit`, `Glob` and `Grep` replace the file
tools, and `Write` and `Edit` work only inside the work folder.

**Leading and building.** A main agent with a team coordinates: it gets read-only file tools and must brief
a teammate (`ask_teammate`, or `start_assignment` for a big job) for anything that has to be built as files.
Teammates save into the same work folder, so the main agent can look at the result and send it back for fixes.

## MCP servers, skills and plugins

- **Apps (integrations)** are MCP servers with guided setup. The Integrations page lists apps by kind
  (GitHub, Git, Slack, Notion, Linear, files, databases, web); *Connect* asks only for what the app
  needs, stores keys encrypted, gives the app to the agents that benefit, and tests it. Agents ask for
  approval before changing anything in a connected app.
- **MCP servers** run (stdio) or are reached (HTTP) only for the run that needs them. A stdio server
  gets a minimal environment plus the variables and secrets configured for it, never every secret the
  worker holds. A server that fails to start is skipped with a warning in the Activity log, and the
  work goes on. Its own output goes to `<home>/logs/mcp-<id>.log`. **Test** on the Tools page starts
  the server through the worker and lists its tools.
- **Skills** use the `SKILL.md` format: a name, a description that says when to use it, and
  instructions. Agents see the descriptions and load a skill when a task matches. Every role
  template and profession teammate comes with the skills for its job. Skills can also be installed
  from [skills.sh](https://skills.sh): paste a link or `npx skills add owner/repo`, and the gateway
  reads the public GitHub repository (only github.com is contacted). Imported skills belong to no
  agent until you attach them, because an agent follows what a skill says.
- **Plugins** install agents, MCP servers and skills together, and uninstall them together. Built-in
  packs are available, and Claude Code plugin folders (`.claude-plugin/plugin.json`, `agents/`,
  `skills/`, `.mcp.json`) can be imported. `${NAME}` values become references to Cubicle secrets.

## Memory and tokens

Agents save durable facts about you as soon as they learn them (`remember`), and fix wrong ones
(`forget`); keys, tokens and passwords are refused. The office can hold hundreds of facts, but each
request carries only the few that fit it (matching words, plus the newest), within a size budget.
Long chats keep their latest turns whole and trim old long ones, tool output and file reads are
capped (long files are read in parts), skills load only when needed, and a single-step assignment
skips the wrap-up model call. Activity older than 45 days and usage older than 400 days are pruned.

## Automations and the budget

Automations (`daily@HH:MM`, `weekdays@HH:MM`, `weekly@mon,thu@HH:MM`, `every:30m` with a 5-minute
minimum, or `once@YYYY-MM-DDTHH:MM`) become assignments for the main agent, or for the agent you
chose. Runs missed while the PC was off collapse into a single run. **Run now** starts one
immediately without changing the schedule.

Every model call reports its token usage. When the daily budget is reached, the circuit breaker
opens: new work pauses until midnight or until you click **Resume work**.

## Reliability

- Task claims are 25-second long-polls inside a single SQLite transaction. Any change wakes the waiting desks instantly.
- SQLite runs in WAL mode with one connection, which serialises writes. New columns are added on start, so older databases upgrade in place.
- A crashed worker restarts with exponential backoff, and work it was doing is queued again when the new worker starts. The Windows service restarts on failure.
- The desktop app reconnects automatically and refreshes from the live event stream.
