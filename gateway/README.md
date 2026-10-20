# cubicle-gateway

The Go control plane of [Cubicle](../README.md). It is a single static binary with no runtime dependencies.

```
cubicle-gateway [serve]                          run in the foreground (default)
cubicle-gateway service install|uninstall|start|stop   Windows service (as Administrator)
cubicle-gateway version
```

## Develop

```powershell
go run ./cmd/cubicle-gateway       # http://127.0.0.1:7878
go vet ./... ; go test ./...       # tests live in ./test
```

## Configuration

Settings are resolved in this order: defaults, then `<home>/config.json`, then environment variables.
`<home>` is `%LOCALAPPDATA%\Cubicle` (or `%ProgramData%\Cubicle` for the Windows service, and `~/.cubicle` elsewhere), and can be overridden with `CUBICLE_HOME`.

| config.json | Environment | Default |
|---|---|---|
| `addr` | `CUBICLE_ADDR` | `127.0.0.1:7878` |
| `tls_cert`, `tls_key` | `CUBICLE_TLS_CERT`, `CUBICLE_TLS_KEY` | (none) |
| `allow_insecure_remote` | `CUBICLE_ALLOW_INSECURE_REMOTE` | `false` |
| `daily_token_budget` | `CUBICLE_DAILY_TOKEN_BUDGET` | `0` (unlimited) |
| `worker_command`, `worker_args` | `CUBICLE_WORKER_COMMAND` | bundled `cubicle-agents` next to the binary |
| `disable_worker` | `CUBICLE_DISABLE_WORKER` | `false` |
| `rate_limit_per_minute` | | `600` |
| | `CUBICLE_ADMIN_TOKEN`, `CUBICLE_WORKER_TOKEN` | generated on first start (min. 32 chars) |

See [docs/API.md](../docs/API.md) for the HTTP API.
