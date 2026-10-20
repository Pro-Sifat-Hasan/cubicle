# cubicle-agents

The Python half of [Cubicle](../README.md): your AI staff. The worker connects to the
Cubicle gateway, clocks in your employees, and each one works at its own "desk":
claiming tasks, calling the model, asking you questions when stuck, and reporting results.

```
src/cubicle_agents/
├── cli.py              # `cubicle-agents run | roster | init-staff`
├── core/
│   ├── settings.py     # CUBICLE_* environment settings
│   └── gateway.py      # async client for the gateway's /internal/v1 API
├── llm/
│   ├── base.py         # LLM protocol + Completion
│   ├── anthropic_llm.py# Claude via the official Anthropic SDK
│   └── demo_llm.py     # offline demo provider (no API key needed)
├── staff/
│   ├── roster.py       # default staff + custom staff.json loading
│   └── prompts.py      # all prompt templates in one place
└── office/
    ├── desk.py         # one long-polling work loop per employee
    ├── handlers.py     # plan / do / wrap-up logic
    └── office.py       # clocks everyone in and runs the desks
tests/                  # pytest suite (fake gateway + demo LLM, no network)
```

## Develop

```powershell
uv sync                    # install deps
uv run pytest              # tests
uv run ruff check .        # lint
uv run cubicle-agents run  # needs the gateway running (see repo README)
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `CUBICLE_GATEWAY_URL` | `http://127.0.0.1:7878` | Gateway address |
| `CUBICLE_WORKER_TOKEN` | read from `<home>/worker.token` | Worker credential |
| `CUBICLE_PROVIDER` | `auto` | `anthropic`, `demo`, or `auto` (Claude if `ANTHROPIC_API_KEY` is set) |
| `CUBICLE_MODEL` | `claude-opus-5-5` | Default model for all employees |
| `CUBICLE_EFFORT` | `medium` | `low` … `max` |
| `CUBICLE_OWNER_NAME` | `the owner` | How your staff refer to you |
