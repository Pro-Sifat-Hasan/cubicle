# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-10-20

### Added
- **Gateway (Go):** SQLite-backed office state; owner and worker APIs with separate tokens; long-poll task claiming with dependencies; delegation, synthesis, failure propagation and cancellation; the "Ask me" inbox; routines (daily, weekdays, interval) with missed-run collapsing; a daily token-budget circuit breaker; AES-GCM secrets with DPAPI key protection; agent-worker supervision with backoff; SSE live events; a Windows service mode.
- **Agents (Python):** a default six-person staff; manager planning with structured output; specialist work; wrap-up synthesis; owner questions; a Claude provider (official Anthropic SDK, adaptive thinking, refusal fallback); an offline demo provider; custom rosters via `staff.json`.
- **Desktop (C#):** a WPF Fluent app with Today, Inbox, Routines, Activity and Settings pages; a tray icon; hide-on-close; start with Windows; single instance; automatic gateway launch; notifications.
- **Distribution:** a per-user Inno Setup installer, a Docker image and compose file, a systemd unit, and CI and release workflows.
