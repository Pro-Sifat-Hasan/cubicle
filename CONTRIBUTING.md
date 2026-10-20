# Contributing to Cubicle

Thank you for helping improve Cubicle. This guide covers the codebase, local setup and the
workflow for changes.

## Project layout

Cubicle is made of three programs. Each uses the language best suited to its job.

| Folder | Language | Program | Responsibility |
|---|---|---|---|
| [`gateway/`](gateway) | Go | `cubicle-gateway` | API, SQLite state, routine scheduler, encrypted secrets, supervises the agent worker |
| [`agents/`](agents) | Python | `cubicle-agents` | The AI staff: model providers, tools, planning and work loops |
| [`desktop/`](desktop) | C# (.NET 10, WPF) | `Cubicle.exe` | Windows app: tray icon, dashboard, inbox, routines, memory, settings |

```
gateway/
  cmd/cubicle-gateway/     entry point
  internal/                api, store, providers, secrets, supervisor, events, config, winsvc
  test/                    black-box tests
agents/
  src/cubicle_agents/      core, llm (providers), tools, staff (roster + prompts), office (work loops)
  tests/
desktop/
  src/Cubicle.Client/      UI-free API client
  src/Cubicle.Desktop/     WPF app: Views, ViewModels, Services, Themes
  tests/Cubicle.Client.Tests/
installer/                 Inno Setup script
deploy/                    Docker and systemd
scripts/                   build, dev, test, smoke-test, brand
docs/                      architecture, configuration, API, branding
```

Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before making larger changes.

## Prerequisites

- Windows 10 or 11 (required for the desktop app; the gateway and agents also run on Linux and macOS)
- [Go 1.26+](https://go.dev/dl/)
- [uv](https://docs.astral.sh/uv/) (installs Python 3.11+ automatically)
- [.NET 10 SDK](https://dotnet.microsoft.com/download)
- Optional: [Inno Setup 6](https://jrsoftware.org/isinfo.php) to build the installer
- Optional: [pre-commit](https://pre-commit.com) for local secret scanning and formatting (`pre-commit install`)

## Everyday commands

```powershell
./scripts/dev.ps1          # run gateway, agents and desktop from source, each in its own window
./scripts/test.ps1         # every linter and test suite (the same checks as CI)
./scripts/smoke-test.ps1   # end-to-end: real gateway and worker using the demo provider
./scripts/build.ps1        # release build and installer in dist/
```

Per component:

```powershell
cd gateway; go test ./...
cd agents;  uv run pytest; uv run ruff check .
cd desktop; dotnet test Cubicle.slnx
```

## Branches and releases

| Branch | Purpose |
|---|---|
| `main` | Stable. Matches the latest release and only receives merges. |
| `develop` | Integration branch. **Open pull requests against `develop`.** |
| `feature/<name>` | Your work, branched from `develop`. |
| `hotfix/<name>` | Urgent fixes, branched from `main` and merged into `main` and `develop`. |

To release: merge `develop` into `main`, then push a `vX.Y.Z` tag. The Release workflow builds the
installer and Docker image and publishes them.

## Making changes

- Keep commits small and focused, written as [Conventional Commits](https://www.conventionalcommits.org):
  `feat(agents): …`, `fix(gateway): …`, `docs: …`.
- Add or update tests with every behaviour change. Tests live in `gateway/test/`, `agents/tests/`
  and `desktop/tests/`.
- Keep the HTTP contract in sync across `gateway/internal/api`, `agents/…/core/gateway.py`,
  `desktop/src/Cubicle.Client` and `docs/API.md`.
- Style: `gofmt` and `go vet` for Go, `ruff` for Python, zero-warning builds for C#.
- Never commit secrets. CI runs gitleaks on every push, and GitHub push protection is enabled.
- Treat model output, task text and web content as untrusted input.

## Good first contributions

- New employees or better prompts: `agents/src/cubicle_agents/staff/`
- New tools: `agents/src/cubicle_agents/tools/`
- New providers: `agents/src/cubicle_agents/llm/` and `gateway/internal/providers/catalog.go`
- UI polish: `desktop/src/Cubicle.Desktop/Views/`
- Documentation and translations

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). By contributing, you agree that
your contributions are licensed under the [MIT License](LICENSE).
