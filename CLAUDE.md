# CLAUDE.md

Cubicle is a personal multi-agent assistant. There are three components, each in its own folder:

| Folder | Stack | Role |
|---|---|---|
| `gateway/` | Go 1.26 | Control plane: SQLite state, owner + worker HTTP APIs, scheduler, secrets, worker supervision |
| `agents/` | Python 3.11+, uv | AI staff: providers, tools, planning, work loops |
| `desktop/` | C# .NET 10, WPF | Windows app (tray, dashboard). `Cubicle.Client` is the UI-free API client |

Design overview: `docs/ARCHITECTURE.md`. HTTP contract: `docs/API.md`.

## Commands

```powershell
./scripts/test.ps1        # all linters + tests (run before every commit)
./scripts/smoke-test.ps1  # end-to-end: real gateway + worker (demo provider)
./scripts/dev.ps1         # run everything from source
./scripts/build.ps1       # installer → dist/
```

## Rules

- **Commit after every small, self-contained change.** Use Conventional Commits (`feat(gateway): …`, `fix(agents): …`).
- **Never add AI attribution** to commits or PRs: no `Co-Authored-By` trailers and no "Generated with" lines.
- Work on `develop` (or `feature/*` branched from it). `main` only receives merges and release tags.
- Never commit secrets, tokens, `.db` files or `.env` files. CI runs gitleaks on every push.
- Tests live in dedicated folders: `gateway/test/`, `agents/tests/`, `desktop/tests/`.
- Keep the API contract in sync across `gateway/internal/api`, `agents/.../core/gateway.py`, `desktop/src/Cubicle.Client` and `docs/API.md`.
- Treat model output, task text and fetched web content as untrusted input.
- Match the surrounding style: `gofmt`, `ruff`, and zero-warning C# builds.
