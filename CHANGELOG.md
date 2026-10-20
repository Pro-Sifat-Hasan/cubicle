# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Light and dark themes with a toggle (Light / Dark / System), plus a title bar that matches the theme.
- A redesigned, responsive interface: a dashboard with stats, staff avatars, an assignment plan and rendered Markdown results, icon navigation that collapses on narrow windows, and placeholder hints.
- A portable build (`Cubicle-Portable-<version>-win-x64.zip`) that runs without installing, alongside an installed copy.
- Staff know today's date and check their work before delivering it.
- Claude prompt caching and extra retries for transient API errors.

### Fixed
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
