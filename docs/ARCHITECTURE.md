# Architecture

Cubicle has three processes, each written in the language that fits its job.

| Process | Language | Responsibility |
|---|---|---|
| `Cubicle.exe` | C# (.NET 10, WPF) | The owner's window into the office: tray icon, dashboard, inbox, routines, settings. Holds no state. |
| `cubicle-gateway(.exe)` | Go | The control plane and single source of truth: SQLite state, owner API, worker API, routine scheduler, circuit breaker, encrypted secrets, and supervision of the agent worker. |
| `cubicle-agents(.exe)` | Python | The AI staff: one async "desk" per employee that claims work, calls the model, and reports results. |

## Lifecycle on a desktop

1. `Cubicle.exe` starts (at sign-in with `--background`, or from the Start menu).
2. If `GET /healthz` on `127.0.0.1:7878` fails, it launches the bundled `cubicle-gateway.exe` as a detached, windowless process.
3. On first start the gateway creates `%LOCALAPPDATA%\Cubicle\` (DACL: you, SYSTEM, Administrators), `admin.token`, `worker.token`, `master.key` (DPAPI-wrapped) and `cubicle.db`.
4. The gateway starts `cubicle-agents.exe` with `CUBICLE_GATEWAY_URL`, `CUBICLE_WORKER_TOKEN`, and the decrypted secrets (for example `ANTHROPIC_API_KEY`) as environment variables, and restarts it with exponential backoff if it exits.
5. The worker registers the roster (`PUT /internal/v1/employees`) and opens one long-poll per employee.
6. The desktop app reads `admin.token`, loads state, and subscribes to `GET /api/v1/events` (Server-Sent Events) for live updates and notifications.

Closing the window hides it. The gateway and worker keep running, so routines still fire. "Stop staff and quit" in the tray stops everything.

## The work model

```
assignment (you → manager)
 ├── subtask (→ specialist)          ┐ run in parallel unless depends_on
 ├── subtask (→ specialist, deps: 0) ┘ says otherwise
 └── synthesis (→ manager, deps: all subtasks)
```

| Step | Who | What happens |
|---|---|---|
| Plan | Manager | Claims the `assignment` and asks the model for a JSON plan (structured output). The plan is sanitised: unknown assignees are remapped, forward or self dependencies are dropped, and at most 6 subtasks are kept. It is then posted to `/delegate`. The gateway creates the subtasks plus a `synthesis` task atomically and marks the assignment `delegated`. |
| Work | Specialists | Each claims a subtask only when all its dependencies are `done`. The claim response includes dependency results and any answers you gave. |
| Ask | Anyone | A reply that starts with `ASK_OWNER:` becomes an inbox item. The task moves to `waiting_human` and goes back to `queued` when you answer. Each task can ask at most 2 questions. |
| Wrap up | Manager | Claims the `synthesis` task and merges the results. Completing it completes the parent assignment. With a single subtask, the result passes straight through and no extra tokens are spent. |
| Failure | Gateway | A failed subtask fails its assignment and cancels unfinished siblings, so nothing waits forever. |

Task statuses: `queued → in_progress → (delegated | waiting_human) → done | failed | cancelled`.

### Claiming and concurrency

`POST /internal/v1/tasks/claim` is a 25-second long-poll. The claim is one SQLite transaction: it picks the oldest `queued` task for the employee whose dependencies are all `done`, then flips it to `in_progress`. Any state change wakes waiting pollers through an in-memory broadcast, so work is picked up within milliseconds without busy polling. SQLite runs in WAL mode with a single connection, which serialises writes and removes `SQLITE_BUSY` races.

### Routines

Schedules are `daily@HH:MM`, `weekdays@HH:MM`, or `every:<duration>` (minimum 5 minutes), in the machine's local time. Every 30 seconds the gateway turns due routines into manager assignments in one transaction and computes the next run from *now*. A PC that was asleep for three days runs a routine once, not three times.

### Circuit breaker

The worker reports token usage after every model call. If `daily_token_budget` is set and today's usage reaches it, the breaker opens: claims return `503`, desks pause, and you get a notification. It closes at midnight or when you click **Resume work**.

## Gateway package map

```
gateway/
├── cmd/cubicle-gateway/   CLI: serve | service install|uninstall|start|stop | version
└── internal/
    ├── api/          HTTP handlers (owner.go, worker.go), middleware, scheduler, breaker
    ├── store/        SQLite persistence: tasks, office (employees, inbox, activity, usage, secrets), routines
    ├── secrets/      AES-256-GCM, with the key wrapped by DPAPI on Windows
    ├── supervisor/   start, log, and restart the agent worker
    ├── events/       SSE fan-out and long-poll wake-ups
    ├── config/       defaults → config.json → CUBICLE_* env, plus token bootstrap
    ├── fsutil/       owner-only directories and files (Windows DACL / POSIX modes)
    └── winsvc/       Windows service integration
```

## Agents package map

```
agents/src/cubicle_agents/
├── cli.py            run | roster | init-staff
├── core/             settings (env), gateway client (+ Gateway protocol for fakes)
├── llm/              LLM protocol, AnthropicLLM (official SDK), DemoLLM (offline)
├── staff/            roster (default staff + staff.json), prompts (all templates in one file)
└── office/           office (clock-in), desk (work loop), handlers (plan, work, wrap up)
```

The Anthropic provider uses `claude-opus-5-5` by default with adaptive thinking, `effort` (default `medium`), structured outputs for plans, and the server-side refusal fallback (`fallbacks: "default"`), so a declined request is retried on a suitable model instead of failing silently.

## Desktop project map

```
desktop/
├── src/Cubicle.Client/     UI-free: models, GatewayClient (REST + SSE), schedules, paths
├── src/Cubicle.Desktop/
│   ├── App.xaml(.cs)       single instance, tray, hide-on-close, --background
│   ├── Views/              MainWindow (Today · Inbox · Routines · Activity · Settings)
│   ├── ViewModels/         MainViewModel (CommunityToolkit.Mvvm) + item view models
│   ├── Services/           GatewayHost, AutoStart, SingleInstance, TrayIcon
│   ├── Converters/         status → colour or label, visibility helpers
│   └── Themes/Brand.xaml   brand palette and shared styles over the Fluent theme
└── tests/Cubicle.Client.Tests/
```
