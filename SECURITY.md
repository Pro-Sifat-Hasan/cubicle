# Security

## Reporting a vulnerability

Please **do not** open a public issue. Report it privately through
[GitHub Security Advisories](https://github.com/Pro-Sifat-Hasan/cubicle/security/advisories/new).
We aim to reply within 72 hours.

## Security model

Cubicle runs AI agents for one person on that person's machine. The design goal is that nothing
outside your user account can read your data, spend your API credits, or direct your staff.

| Threat | Mitigation |
|---|---|
| Other machines on the network | The gateway binds to `127.0.0.1` by default. It refuses to start on a non-loopback address without TLS unless `CUBICLE_ALLOW_INSECURE_REMOTE=1` is set (for use behind a TLS reverse proxy). |
| Other local processes or users | Every API call needs a 256-bit bearer token, compared in constant time. Tokens live in `%LOCALAPPDATA%\Cubicle`, whose DACL grants only you, SYSTEM, and Administrators (`0700`/`0600` on Linux). No CORS headers are sent, so web pages in your browser cannot call the API. |
| A compromised or misbehaving worker | The worker uses a **separate** token that cannot read secrets, change routines, answer the inbox, or reset the breaker. Delegations are validated server-side: assignees must exist, and dependency indices must point to earlier subtasks. |
| Stolen database or disk image | API keys are encrypted with AES-256-GCM, with the secret name bound as associated data. The key file is wrapped with Windows DPAPI, so it only decrypts for your account on your machine. The API never returns secret values. |
| Runaway spend or loops | A daily token budget circuit breaker, at most 6 subtasks per plan, at most 2 owner questions per task, request size limits (1 MiB), and per-IP rate limiting. |
| Prompt injection via task or web content | Agents have no shell. File tools are confined to the assignment's workspace folder. `web_fetch` reaches only public internet addresses, never localhost, the LAN or cloud metadata endpoints, and re-checks every redirect. The Claude Code provider runs with file and web tools only, in `dontAsk` permission mode. |
| Malicious HTTP clients | Strict JSON decoding (unknown fields rejected), length limits, security headers, server timeouts, and panic recovery. |
| Leaked credentials in the repository | gitleaks in CI on every push and pull request (with full history), a pre-commit hook, and GitHub secret scanning with push protection. |
| Supply chain | Few dependencies, grouped Dependabot updates for every ecosystem, CodeQL analysis, dependency review on pull requests, and SHA-256 checksums published with each release. |

### Running as a server

- Put the gateway behind TLS (`tls_cert` / `tls_key` in `config.json`, or a reverse proxy such as Caddy).
- The Docker image runs as a non-root user with a read-only root filesystem, `no-new-privileges`, and all capabilities dropped.
- The systemd unit enables `ProtectSystem=strict`, `NoNewPrivileges`, and related hardening.
- On Linux, use disk encryption for the data directory, since DPAPI is Windows-only.

### Logs

Logs never include tokens, API keys, or HTTP bodies. HTTP client libraries are held at `WARNING` level in the worker.
