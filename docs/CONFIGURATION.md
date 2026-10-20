# Configuration

Most people never need this page: everything in the desktop app's **Settings** is stored by the
gateway and applies right away. This page covers file and environment configuration, and running
Cubicle as a service or on a server.

## Data folder

| Platform | Location |
|---|---|
| Windows (desktop) | `%LOCALAPPDATA%\Cubicle` |
| Windows service | `%ProgramData%\Cubicle` |
| Linux / macOS | `~/.cubicle` |

Set `CUBICLE_HOME` to use a different folder. Its contents are:

| Path | Contents |
|---|---|
| `cubicle.db` | Tasks, routines, inbox, memory, settings and encrypted secrets (SQLite) |
| `admin.token`, `worker.token` | API credentials, created on first start |
| `master.key` | Encryption key for secrets (wrapped with DPAPI on Windows) |
| `workspace/<assignment>/` | Files your staff produced |
| `staff.json` | Optional custom roster (`cubicle-agents init-staff` creates one) |
| `config.json` | Optional gateway settings (below) |
| `logs/gateway.log` | Gateway and agent logs |

## Gateway settings

Settings are applied in this order: defaults, then `config.json`, then environment variables.

| `config.json` | Environment variable | Default |
|---|---|---|
| `addr` | `CUBICLE_ADDR` | `127.0.0.1:7878` |
| `tls_cert`, `tls_key` | `CUBICLE_TLS_CERT`, `CUBICLE_TLS_KEY` | — |
| `allow_insecure_remote` | `CUBICLE_ALLOW_INSECURE_REMOTE` | `false` |
| `daily_token_budget` | `CUBICLE_DAILY_TOKEN_BUDGET` | `0` (unlimited; the app setting takes precedence) |
| `worker_command`, `worker_args` | `CUBICLE_WORKER_COMMAND` | the bundled `cubicle-agents` |
| `disable_worker` | `CUBICLE_DISABLE_WORKER` | `false` |
| `rate_limit_per_minute` | — | `600` |
| — | `CUBICLE_ADMIN_TOKEN`, `CUBICLE_WORKER_TOKEN` | generated (minimum 32 characters) |

The gateway refuses to listen on a non-loopback address without TLS unless
`allow_insecure_remote` is set. Only set it behind a TLS-terminating reverse proxy.

## Staff

Run `cubicle-agents init-staff` to write the default roster to `staff.json`. In that file you can
rename employees, change their skills and personas, give each one its own `model`, or add new
specialists. The roster must contain exactly one employee with `"role": "manager"`. Restart
Cubicle after editing it.

## Desktop app

| Option | Purpose |
|---|---|
| `--background` | Start in the tray without opening the window (used by "Start with Windows") |
| `CUBICLE_GATEWAY_URL` | Connect to a gateway other than `http://127.0.0.1:7878` |
| `CUBICLE_GATEWAY_EXE` | Gateway binary to launch when none is running |
| `CUBICLE_INSTANCE` | Run a second, independent copy (for example, a development build next to the installed app) |

The appearance (Light, Dark or System) is saved in `desktop.json` in the data folder.

## Portable mode

The `Cubicle-Portable-<version>-win-x64.zip` release asset runs without installing. Unzip it
anywhere and start `Cubicle.exe`. While `portable.txt` sits next to the exe, Cubicle:

- keeps all its data in the `data` folder beside the exe,
- serves the office on port `7879`, and
- uses its own instance lock, so it runs alongside an installed copy without touching it.

Delete `portable.txt` to make that copy use the normal data folder and port.

## Windows service

To keep the office running without anyone signed in, open an Administrator terminal in the install folder:

```powershell
cubicle-gateway service install   # automatic start, restarts on failure
cubicle-gateway service start
```

The service keeps its data in `%ProgramData%\Cubicle`. To point the desktop app at it, set
`CUBICLE_HOME=%ProgramData%\Cubicle` for your user.

## Docker

```bash
docker compose -f deploy/docker/compose.yml up -d --build
docker compose -f deploy/docker/compose.yml exec cubicle cat /data/admin.token
```

The container publishes the API on the host's loopback interface only. For remote access, put
it behind a TLS reverse proxy such as Caddy or nginx. Configure the provider and keys through the
[HTTP API](API.md) (`PUT /api/v1/settings`, `PUT /api/v1/secrets/{NAME}`).

## systemd

See [`deploy/systemd/cubicle.service`](../deploy/systemd/cubicle.service). It is a hardened unit
that runs the gateway as an unprivileged `cubicle` user.
