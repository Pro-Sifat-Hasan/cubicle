# Contributing to Cubicle

Thanks for helping build everyone's personal AI staff! 🎉

## Branches

| Branch | Purpose |
|---|---|
| `main` | Stable. Always matches the latest release. Protected: changes arrive only by PR from `develop` (or `hotfix/*`). |
| `develop` | Integration branch. **Open your PRs against `develop`.** |
| `feature/<name>` | Your work, branched from `develop`. |
| `hotfix/<name>` | Urgent fixes, branched from `main` and merged into both `main` and `develop`. |

Releases: merge `develop` into `main`, then tag `vX.Y.Z` on `main`. The Release workflow builds the
installer and Docker image and publishes them.

## Setup

You need Go 1.26+, [uv](https://docs.astral.sh/uv/), the .NET 10 SDK, and Windows for the desktop app.

```powershell
./scripts/dev.ps1         # run everything from source
./scripts/test.ps1        # all tests and linters
./scripts/smoke-test.ps1  # end-to-end check
```

## Where things live

- **Gateway (Go):** `gateway/internal/<package>`. Tests go in `gateway/test/` as black-box tests.
- **Agents (Python):** `agents/src/cubicle_agents/<area>`. Tests go in `agents/tests/`. All prompts are in `staff/prompts.py`.
- **Desktop (C#):** API code goes in `desktop/src/Cubicle.Client` (unit-tested in `desktop/tests`), and UI code in `desktop/src/Cubicle.Desktop`.

## Conventions

- Commits follow [Conventional Commits](https://www.conventionalcommits.org): `feat(agents): …`, `fix(gateway): …`, `docs: …`.
- Go: `gofmt` and `go vet`. Python: `ruff check`. C#: builds with zero warnings.
- Add or update tests with every behaviour change.
- Never log or commit secrets. Treat model output and task text as untrusted input.

## Good first contributions

- New default employees or better prompts (`agents/src/cubicle_agents/staff/`)
- New LLM providers (`agents/src/cubicle_agents/llm/`), using the official SDK for each provider
- UI polish (`desktop/src/Cubicle.Desktop/Views/`)
- Docs and translations

By contributing you agree that your contributions are licensed under the MIT License.
