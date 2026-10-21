# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **Onboarding:** five short screens to name your chief of staff, pick a profession and a passion, watch your team get hired, and start a first mission. Professions (software engineer, video editor, marketer, founder, other) hire a specialist team, set up routines and suggest starter missions. Ten passions give the main agent a voice, greetings and the app's accent colour, and **Custom** lets you type your own and have the model write the persona. Packs are JSON files. Run setup again from Settings.
- Starter missions on Today while there is no work yet.
- **A calmer Today page:** a command bar with optional details, questions that need you answered right on the page, assignments newest first with a progress bar while they run, and rectangular team cards with live status. The assignment panel closes with the X and reopens when you click an assignment; it shows the result first, with the plan (who, what they were asked, their activity and output) and how it was done (tools used) collapsed.
- Agents record the tools they use, so every assignment shows how the work was done.
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
