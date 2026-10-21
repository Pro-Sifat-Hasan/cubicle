# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **Daily and weekly reports:** the Today page has Today, Yesterday, This week and Last week buttons. The main agent writes the report from a work log of what really happened (assignments and outcomes, chats, tool calls, tokens, questions and approvals, new things learned about you, files made). Agents with the assignments tool can read the same log with `work_log`, so an automation can send you a report every evening.
- **Chats are titled by the model** from your first message, and each chat is its own session with separate history. A chat you name yourself keeps its name.
- **Close button** (and Esc) for the open chat in the Inbox.
- **A better edit tool for the Dev:** `edit_file` makes several changes to a file at once, keeps its line endings and indentation, shows the diff and warns when a Python or JSON file no longer parses; `insert_lines` adds lines at a line number.
- **Pre-push safety checks** (`./scripts/install-hooks.ps1`): secret and API key scan, a block on `.env`, database and key files, a vulnerability audit of Go, Python and .NET dependencies, then the linters and tests. Issue templates for agent problems, questions and documentation.

### Fixed
- Resolved the open CodeQL alerts (log injection, floating point comparison, disposal on error paths) and the gitleaks findings (fake keys in a test), and pinned a Go toolchain without known vulnerabilities.
- **See everything an agent does, like Claude Code:** the Inbox shows a live timeline of each reply: what the agent is thinking, the remarks it makes between steps, and every tool call with its input (a file's content appears as it is written) and its output, with a spinner while it runs, a check or a cross when it ends, and how long it took. Click a call to open it. The timeline stays under the reply as "How it was done", and a teammate's steps carry their face and name. Replies are typed out as they are written.
- **Cartoon avatars:** every agent is a cartoon character, drawn from its id and name so it always looks the same, with a prop for the job (a headset for engineers, a tie for the chief of staff, a beret for writers, glasses for analysts and reviewers).
- **The main agent leads and the Dev builds:** a main agent with a team can no longer create files itself. It briefs the right teammate (the engineer for code, websites and portfolios), checks what they made and reports back. With Claude Code, the teammate builds with Claude Code's own file tools in the shared work folder.
- **Claude Code gets Cubicle's tools:** over a local, token-protected bridge it can ask teammates, start assignments, remember, load skills, ask you and request approval, and run commands under Cubicle's approval rules. Agents also know which apps are connected, and work in a fixed order: check skills and apps, work, then verify before reporting.
- Claude Code runs isolated from your own Claude Code setup (no personal MCP servers, skills, settings or CLAUDE.md files), which cut each request from about 320,000 to about 5,000 tokens, and it can write only inside the work folder.
- **Onboarding:** five short screens to name your chief of staff, pick a profession and a passion, watch your team get hired, and start a first mission. Professions (software engineer, video editor, marketer, founder, other) hire a specialist team, set up routines and suggest starter missions. Ten passions give the main agent a voice and greetings, and **Custom** lets you type your own and have the model write the persona. Packs are JSON files. Run setup again from Settings.
- Starter missions on Today while there is no work yet.
- **A calmer Today page:** a command bar with optional details, questions that need you answered right on the page, assignments newest first with a progress bar while they run, and rectangular team cards with live status. The assignment panel closes with the X and reopens when you click an assignment; it shows the result first, with the plan (who, what they were asked, their activity and output) and how it was done (tools used) collapsed.
- Agents record the tools they use, so every assignment shows how the work was done.
- **Agent tools:** `edit_file`, `find_files`, `grep_files`, and a guarded **terminal** (tests, linters, builds, git; every command except read-only git needs your approval). Developer roles get it by default; existing developer agents get it once on upgrade.
- **19 more built-in skills** (debugging, test writing, git workflow, refactoring, API design, security review, data analysis, SQL, copywriting, SEO, project planning and more), attached to the agents they help. Install more from **skills.sh** by pasting a link or `npx skills add owner/repo`.
- **Integrations page** (replaces Tools & skills): connect GitHub, Git, Slack, Notion, Linear, local files, SQLite, web search and more with guided setup, keys stored encrypted, the right agents pre-selected, and a connection test.
- **Team page** (was Agents): a card grid, and an editor that closes with X and has Profile, Abilities and Team sections with plain descriptions of every tool.
- Agents remember better and cheaper: only the facts that fit each request are sent, new facts are saved automatically, wrong ones can be forgotten, and secrets are never stored. Long chats and tool output are trimmed to save tokens.
- The app keeps the Cubicle amber everywhere. Setup refuses a chief-of-staff name that another agent already has.
- **Questions and approvals everywhere:** agents can ask you a question in a chat or an assignment, and must ask your approval before acting outside their workspace. A redesigned Inbox has a **Needs you** thread for both, shows requests inside the chat they came from, and shows each reply's tools, plus the plan, trail and result of any assignment it started. The Files button is gone.
- **Agents page:** create agents, add them from 18 role templates in one click, and customise each one's instructions, model, effort, tools, MCP servers, skills and team. Choose the main agent, which plans assignments with its team. Changes apply without restarting.
- **Chat in the Inbox:** talk to any agent, with live progress and replies that stream in. Agents can consult teammates, and the main agent can start a full assignment from a chat.
- **MCP servers:** connect agents to apps and services (GitHub, files, browser, search and more) with presets or custom stdio and HTTP servers, encrypted secrets, and a live connection test.
- **Skills and plugins:** SKILL.md skills that agents load when relevant (templates, an editor and file import), and plugins that install agents, MCP servers and skills together, including Claude Code plugin folders.
- **Automations** (formerly Routines): send work to the main agent or any agent, weekly and one-off schedules, editing, Run now, and the last run's status.
- Assign work straight to one agent from the Today page.
- Light and dark themes with a toggle (Light / Dark / System), plus a title bar that matches the theme.
- A redesigned, responsive interface: a dashboard with stats, staff avatars, an assignment plan and rendered Markdown results, icon navigation that collapses on narrow windows, and placeholder hints.
- A portable build (`Cubicle-Portable-<version>-win-x64.zip`) that runs without installing, alongside an installed copy.
- Staff know today's date and check their work before delivering it.
- Claude prompt caching and extra retries for transient API errors.

### Changed
- The agent worker runs on Pydantic AI: one engine for every API provider, validated plans that are retried when invalid, streaming, and usage limits.
- Agents are stored by the gateway and edited in the app; `staff.json` is only imported once on upgrade, and `cubicle-agents init-staff` and `roster` were removed.

### Fixed
- Work in progress when the worker restarted (for example after saving settings) stayed "Working" forever; it is now resumed.
- Dark text on dark backgrounds, and the Windows accent colour (for example green) leaking into selections and the title bar.
- A blurry app and tray icon at small sizes.
- VS Code C# Dev Kit loading WPF's temporary project files.

## [0.1.0] - 2026-10-20

The first public release.

### Added
- **Your AI staff:** a Chief of Staff who plans and delegates, five specialists (research, writing, engineering, planning, review) and a wrap-up step that merges everything into one deliverable. A quality-review step is added for work you will rely on.
- **Choose your AI:** Anthropic Claude API, OpenAI, Google Gemini, OpenRouter, Ollama, any OpenAI-compatible server, or your Claude subscription through the Claude Code CLI. Includes a live model list and a connection test.
- **Tools:** a per-assignment file workspace, web search and reading, long-term memory, and questions to you through the Inbox.
- **Routines:** daily, weekday or interval schedules that run in the background. Missed runs collapse into one.
- **Memory:** a shared, editable long-term memory that every employee sees.
- **Desktop app:** Today, Inbox, Routines, Memory, Activity and Settings pages; a tray icon; start with Windows; live updates and notifications; a light and dark Fluent theme.
- **Safety:** a daily token budget with a circuit breaker, DPAPI-encrypted keys, a localhost-only API with separate owner and worker tokens, a sandboxed workspace, and an SSRF-guarded web fetch.
- **Distribution:** a per-user Windows installer, a Windows service mode, a Docker image and a hardened systemd unit.
- **Engineering:** CI on Windows and Linux with an end-to-end smoke test, CodeQL, gitleaks, dependency review, grouped Dependabot updates and a release pipeline.

[Unreleased]: https://github.com/Pro-Sifat-Hasan/cubicle/compare/v0.1.0...develop
[0.1.0]: https://github.com/Pro-Sifat-Hasan/cubicle/releases/tag/v0.1.0
