# Gateway HTTP API

Base URL: `http://127.0.0.1:7878` by default. All bodies are JSON with `snake_case` fields.

There are two authenticated surfaces with **separate tokens**:

| Surface | Prefix | Token file | Used by |
|---|---|---|---|
| Owner API | `/api/v1` | `<home>/admin.token` | Desktop app, scripts, you |
| Worker API | `/internal/v1` | `<home>/worker.token` | The agent worker |

Send `Authorization: Bearer <token>`. `GET /healthz` needs no token.

```powershell
$t = Get-Content "$env:LOCALAPPDATA\Cubicle\admin.token"
Invoke-RestMethod http://127.0.0.1:7878/api/v1/status -Headers @{ Authorization = "Bearer $t" }
```

## Owner API

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/status` | | version, task counts, open questions, breaker state, tokens today |
| `GET /api/v1/employees` | | roster with live status |
| `POST /api/v1/assignments` | `{title, description?}` | the new assignment (`201`) |
| `GET /api/v1/tasks?status=&parent_id=&limit=` | | tasks, newest first (`parent_id=` with an empty value returns top-level assignments) |
| `GET /api/v1/tasks/{id}` | | task + `children` + `inbox` |
| `POST /api/v1/tasks/{id}/cancel` | | cancelled task and children |
| `GET /api/v1/inbox?status=open` | | questions from your staff |
| `POST /api/v1/inbox/{id}/answer` | `{answer}` | answered item; the task resumes |
| `GET /api/v1/routines` | | routines |
| `POST /api/v1/routines` | `{title, description?, schedule}` | new routine (`201`) |
| `POST /api/v1/routines/{id}/enable` / `disable` | | routine |
| `DELETE /api/v1/routines/{id}` | | `204` |
| `GET /api/v1/activity?limit=` | | activity log, newest first |
| `GET /api/v1/secrets` | | secret **names** only |
| `PUT /api/v1/secrets/{NAME}` | `{value}` | `204`; the worker restarts with the new value |
| `DELETE /api/v1/secrets/{NAME}` | | `204` |
| `POST /api/v1/breaker/reset` | | `204` |
| `GET /api/v1/events` | | Server-Sent Events stream |

Schedules: `daily@09:00`, `weekdays@08:30`, `every:30m`, `every:2h` (minimum 5 minutes, local time).

Event types: `task.updated`, `assignment.done`, `inbox.new`, `inbox.answered`, `employee.updated`,
`employees.synced`, `employees.offline`, `routine.updated`, `routine.deleted`, `activity`,
`breaker.tripped`, `breaker.reset`. Each frame is `data: {"type": "...", "data": {...}, "at": "..."}`.

## Worker API

| Method & path | Body | Notes |
|---|---|---|
| `PUT /internal/v1/employees` | `[{id, name, title, role, avatar, model, skills}]` | register the roster (1–50) |
| `PATCH /internal/v1/employees/{id}` | `{status, current_task_id}` | `idle` / `working` / `waiting` / `offline` |
| `POST /internal/v1/tasks/claim` | `{employee_ids: []}` | long-poll ≤ 25 s; `200` with `{task, parent, dependency_results, human_answers}`, `204` if none, `503` if the breaker is open |
| `POST /internal/v1/tasks/{id}/delegate` | `{subtasks: [{title, description, assignee, depends_on: [index]}], synthesis: {title, description, assignee}}` | assignment → plan |
| `POST /internal/v1/tasks/{id}/complete` | `{result}` | |
| `POST /internal/v1/tasks/{id}/fail` | `{error}` | fails the parent and cancels siblings |
| `POST /internal/v1/tasks/{id}/ask` | `{employee_id, question}` | creates an inbox item |
| `POST /internal/v1/usage` | `{task_id, employee_id, input_tokens, output_tokens}` | feeds the circuit breaker |
| `POST /internal/v1/activity` | `{employee_id, task_id, message}` | activity log line |

Errors are returned as `{"error": "message"}` with `400`, `401`, `404`, `409` (the task is in the wrong state),
`429` (rate limited), or `503` (the breaker is open).
