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

### Office and work

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/status` | | version, task counts, open questions, breaker state, tokens today |
| `POST /api/v1/assignments` | `{title, description?, agent_id?}` | the new assignment (`201`). Without `agent_id` it goes to the main agent, who plans it with the team |
| `GET /api/v1/tasks?status=&parent_id=&limit=` | | tasks, newest first (`parent_id=` with an empty value returns top-level work, without chat replies) |
| `GET /api/v1/tasks/{id}` | | task + `children` + `inbox` |
| `POST /api/v1/tasks/{id}/cancel` | | cancelled task and children |
| `GET /api/v1/inbox?status=open` | | requests from your staff: `kind` is `question` or `approval`; `conversation_id` (a chat) or `task_title` (an assignment) says where it came from |
| `POST /api/v1/inbox/{id}/answer` | `{answer?, decision?}` | answered item; the task resumes. A question needs `answer`. An approval needs `decision` (`approve` or `decline`) and takes an optional note in `answer` |
| `GET /api/v1/activity?limit=&task_id=&conversation_id=` | | activity log, newest first; with `task_id`, one assignment's trail (its subtasks included), and with `conversation_id`, the trail of every reply in a chat, both oldest first. Messages starting with `Tool: ` are tool calls |
| `GET /api/v1/steps?task_id=&conversation_id=` | | what agents did while working, oldest first: `[{id, task_id, conversation_id, agent_id, kind, name, input, output, status, created_at, updated_at}]`. `kind` is `thinking`, `note` (what the agent said between tool calls) or `tool` (`name`, its `input` and `output`); `status` is `running`, `done` or `error`. With `task_id`, subtasks are included |
| `POST /api/v1/breaker/reset` | | `204` |
| `GET /api/v1/events` | | Server-Sent Events stream |

### Agents

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/agents` | | the roster (main agent first) with live `status` |
| `POST /api/v1/agents` | `{template_id}` or agent fields, `join_team?` (default true) | new agent (`201`); it joins the main agent's team |
| `GET /api/v1/agents/{id}` | | agent |
| `PUT /api/v1/agents/{id}` | any agent fields (omitted fields are kept) | updated agent |
| `DELETE /api/v1/agents/{id}` | | `204`; `409` for the main agent. Queued work moves to the main agent |
| `POST /api/v1/agents/{id}/main` | | makes it the main agent; the previous main agent joins its team |
| `GET /api/v1/catalog` | | built-in `tools`, agent templates (`agents`), `mcp_servers`, `skills` and `plugins` |

Agent fields: `name`, `title`, `avatar`, `description` (what it is good at; the main agent routes
work by it), `instructions`, `model` and `effort` (empty = office default), `tools` (built-in tool
ids: `files`, `web`, `memory`, `assignments`), `mcp_servers`, `skills` and `team` (ids), `enabled`.
Turning an agent off hands its queued work to the main agent.

### Tools: MCP servers, skills, plugins

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/mcp-servers` | | MCP servers |
| `POST /api/v1/mcp-servers` | `{template_id, args?, name?, url?, env?}` or server fields | new server (`201`) |
| `PUT /api/v1/mcp-servers/{id}` | server fields | updated server |
| `DELETE /api/v1/mcp-servers/{id}` | | `204`; detached from every agent |
| `POST /api/v1/mcp-servers/{id}/test` | | `{tools: [{name, description}], error?}`: starts or connects to the server through the worker |
| `GET /api/v1/skills` / `GET /api/v1/skills/{id}` | | skills |
| `POST /api/v1/skills` | `{template_id}` or `{name, description, content}` | new skill (`201`) |
| `PUT /api/v1/skills/{id}` | `{name, description, content, enabled}` | updated skill |
| `DELETE /api/v1/skills/{id}` | | `204`; detached from every agent |
| `POST /api/v1/integrations/{id}/connect` | `{values, agent_ids}` | `{server, attached}`: connects an app from the catalog in one step. `values` fills the preset's `fields` (secret fields are stored encrypted, others fill `{placeholders}`); `agent_ids` get the app. Reconnecting reuses the server, and an empty secret keeps the stored key |
| `POST /api/v1/skills/discover` | `{source}` | `{source, skills: [{name, description, path, installed}]}`: reads a skills.sh link, an `npx skills add` command, `owner/repo[/skill]` or a GitHub link (public repositories only) |
| `POST /api/v1/skills/install` | `{source, paths?}` | `{installed, skipped}`: installs the chosen skills (all when `paths` is empty). They belong to no agent until attached |
| `GET /api/v1/plugins` | | installed plugins with the ids of what they added |
| `POST /api/v1/plugins` | `{catalog_id}` or `{bundle: {name, description, version, agents, mcp_servers, skills}}` | installed plugin (`201`) |
| `DELETE /api/v1/plugins/{id}` | | `204`; removes everything the plugin added |

App presets (`GET /api/v1/catalog`, `mcp_servers`) carry `category`, `icon`, `fields` (what to ask the
owner: `key`, `label`, `kind` of `secret`, `text` or `folder`, `secret` name, `placeholder`, `help`),
`agents` (who benefits most) and `link` (where to get a key).

MCP server fields: `name`, `description`, `transport` (`stdio` or `http`), `command`, `args`,
`env` (plain values), `secret_env` (names of secrets passed to a stdio server), `url`, `headers`,
`auth_secret` (name of a secret sent as `Authorization: Bearer`), `enabled`. Secret **values** are
never part of a server; store them with `PUT /api/v1/secrets/{NAME}`.

In a plugin bundle, agents may refer to the bundle's own servers and skills by the ids used inside
the bundle; they are rewritten to the ids actually created.

### Chat

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/conversations` | | conversations, most recent first, with `last_message` and `busy` |
| `POST /api/v1/conversations` | `{agent_id, title?}` | new conversation (`201`) |
| `GET /api/v1/conversations/{id}` | | conversation + `messages` (oldest first; `role` is `owner` or `agent`, `status` is `done` or `failed`) |
| `POST /api/v1/conversations/{id}/messages` | `{content}` | your message (`201`); the reply arrives as a `conversation.updated` event |
| `DELETE /api/v1/conversations/{id}` | | `204`; pending replies are cancelled |

### Automations

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/routines` | | automations with `next_run_at`, `last_run_at`, `last_status` |
| `POST /api/v1/routines` | `{title, description?, schedule, agent_id?}` | new automation (`201`) |
| `PUT /api/v1/routines/{id}` | `{title, description?, schedule, agent_id?}` | updated automation (a changed schedule restarts its clock) |
| `POST /api/v1/routines/{id}/run` | | the run's task (`201`); the schedule is unchanged |
| `GET /api/v1/routines/{id}/runs?limit=` | | recent runs, newest first |
| `POST /api/v1/routines/{id}/enable` / `disable` | | automation |
| `DELETE /api/v1/routines/{id}` | | `204` |

Schedules (local time): `daily@09:00`, `weekdays@08:30`, `weekly@mon,thu@08:30`, `every:30m`,
`every:2h` (minimum 5 minutes), `once@2026-10-21T09:00` (turns itself off after running). An empty
`agent_id` sends the work to the main agent; an agent that was removed or turned off is replaced by
the main agent.

### Onboarding and profile

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/onboarding` | | `{profile, passions, professions}`: the built-in packs and whether setup is done |
| `POST /api/v1/onboarding` | `{agent_name, owner_name?, profession, passion, custom_passion?, persona?, stack?}` | `{profile, hired, routines, starter_missions}` |
| `GET /api/v1/profile` | | `{profile, starter_missions}` |
| `POST /api/v1/persona` | `{passion}` | `202 {task_id}`: the main agent writes a persona for a typed passion |
| `GET /api/v1/persona/{task_id}` | | `{status, persona?, error?}`; `status` is `queued`, `in_progress`, `done` or `failed` |

A **passion** pack (`id`, `title`, `emoji`, `system_prompt_style`, `greeting_templates`,
`daily_feed_queries`, `accent_color`, `avatar_set`) sets the main agent's voice and look. A
**profession** pack (`id`, `title`, `emoji`, `description`, `team`, `routines`, `starter_missions`,
`stack_suggestions`) sets the team, routines and missions. Both are JSON files embedded in the
gateway (`internal/catalog/packs`).

Onboarding renames the main agent and appends the passion's style to its instructions (a second run
replaces it), hires the profession's team (agents that already exist are kept), creates its routines
(only the ones marked `enabled` are switched on), and saves the profession, stack and passion as
memories. `passion` is a pack id or `custom`; for `custom`, `custom_passion` (up to 80 characters)
is required and `persona` is the generated one, which is optional: without it a plain fallback persona
is used. Model-written personas are clamped (lengths, counts, a valid `#RRGGBB` accent) before they
are stored. Persona tasks run on the main agent and never appear in `/api/v1/tasks`.

### Settings, providers, secrets and memory

| Method & path | Body | Returns |
|---|---|---|
| `GET /api/v1/settings` | | provider, model, effort, base URL, owner name, web access, daily budget |
| `PUT /api/v1/settings` | `{provider, model, effort, base_url, owner_name, web_access, daily_token_budget}` | saved settings; the worker restarts |
| `GET /api/v1/providers` | | provider catalog with `configured` and `active` flags |
| `GET /api/v1/providers/{id}/models?base_url=` | | `{models, live, error?}`: a live list from the provider, or suggestions |
| `GET /api/v1/secrets` | | secret **names** only |
| `PUT /api/v1/secrets/{NAME}` | `{value}` | `204`; the worker restarts with the new value |
| `DELETE /api/v1/secrets/{NAME}` | | `204` |
| `GET /api/v1/memories` | | what the staff remember, newest first |
| `POST /api/v1/memories` | `{content}` | the new memory (`201`) |
| `DELETE /api/v1/memories/{id}` | | `204` |

### Events

Each frame is `data: {"type": "...", "data": {...}, "at": "..."}`. Types: `task.updated`,
`task.step` (a step as listed by `/api/v1/steps`, sent when it starts and again when it changes), `task.progress` (live `{task_id, conversation_id, agent_id, status, text}` while an agent works; not
stored), `assignment.done`, `inbox.new`, `inbox.answered`, `agent.status`, `agents.updated`,
`employees.offline`, `mcp.updated`, `skills.updated`, `plugins.updated`, `conversation.updated`,
`conversation.deleted`, `routine.updated`, `routine.deleted`, `activity`, `breaker.tripped`,
`breaker.reset`, `settings.updated`, `profile.updated`, `memory.added`, `memory.deleted`.

## Worker API

| Method & path | Body | Notes |
|---|---|---|
| `POST /internal/v1/worker/start` | | called once on start: tasks left `in_progress` by a previous worker are queued again; returns `{requeued}` |
| `GET /internal/v1/agents?after=` | | `{revision, agents}`: enabled agents with resolved `mcp` servers and `skill_docs`. With `after=<revision>` it long-polls (≤ 25 s, `204` if unchanged) |
| `PATCH /internal/v1/agents/{id}/status` | `{status, current_task_id}` | `idle` / `working` / `waiting` / `offline` |
| `POST /internal/v1/tasks/claim` | `{agent_ids: [], lane}` | `lane` is `work`, `chat` or empty. Long-poll ≤ 25 s; `200` with `{task, parent, dependency_results, human_answers, history}`, `204` if none, `503` if the breaker is open |
| `POST /internal/v1/tasks/{id}/delegate` | `{subtasks: [{title, description, assignee, depends_on: [index]}], synthesis: {title, description, assignee}}` | assignment → plan; assignees must be enabled agents |
| `POST /internal/v1/tasks/{id}/complete` | `{result}` | a chat task's result becomes the agent's reply |
| `POST /internal/v1/tasks/{id}/fail` | `{error}` | fails the parent and cancels siblings; a failed chat task becomes a failed reply |
| `POST /internal/v1/tasks/{id}/ask` | `{employee_id, question, kind?}` | creates an inbox item and parks the task (an assignment step or a chat reply); `kind` is `question` (default) or `approval`. The answer arrives in `human_answers` when the task is claimed again |
| `POST /internal/v1/tasks/{id}/progress` | `{status?, text?}` | relayed to the app as `task.progress` |
| `POST /internal/v1/tasks/{id}/steps` | `{id, agent_id?, kind, name?, input?, output?, status?}` | records a step and relays it as `task.step`; the same `id` again updates it (a tool call gaining its output). Long fields are cut, a task keeps at most 400 steps, and steps still `running` are marked `done` when the task finishes or pauses |
| `POST /internal/v1/assignments` | `{title, description, agent_id, source_task_id?}` | an agent starts an assignment for the main agent (from a chat); `source_task_id` ties it to the reply that started it |
| `POST /internal/v1/usage` | `{task_id, employee_id, input_tokens, output_tokens}` | feeds the circuit breaker |
| `POST /internal/v1/activity` | `{employee_id, task_id, message}` | activity log line |
| `GET /internal/v1/memories?limit=` | | facts (with ids); the worker picks the ones that fit each request |
| `DELETE /internal/v1/memories/{id}` | | `204`; an agent forgets a wrong fact |
| `POST /internal/v1/memories` | `{content, employee_id}` | saves a learned fact |

Errors are returned as `{"error": "message"}` with `400`, `401`, `404`, `409` (wrong state, or the
main agent cannot be removed), `429` (rate limited), or `503` (the breaker is open).
