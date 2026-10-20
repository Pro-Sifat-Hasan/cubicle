<#
.SYNOPSIS
  Runs Cubicle from source: gateway (Go), agent worker (Python) and the desktop app (C#),
  each in its own window so you can watch the logs.

.EXAMPLE
  ./scripts/dev.ps1            # all three
  ./scripts/dev.ps1 -NoDesktop # backend only
#>
param([switch]$NoDesktop)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

# The gateway would normally supervise the bundled worker; in dev we run it ourselves.
Start-Process powershell -WorkingDirectory (Join-Path $Root "gateway") -ArgumentList @(
    "-NoExit", "-Command", "`$env:CUBICLE_DISABLE_WORKER='1'; go run ./cmd/cubicle-gateway serve"
)

# Wait for the gateway so the worker token exists.
$deadline = (Get-Date).AddSeconds(60)
while ((Get-Date) -lt $deadline) {
    try { Invoke-RestMethod http://127.0.0.1:7878/healthz -TimeoutSec 2 | Out-Null; break } catch { Start-Sleep -Milliseconds 500 }
}

Start-Process powershell -WorkingDirectory (Join-Path $Root "agents") -ArgumentList @(
    "-NoExit", "-Command", "uv run cubicle-agents run"
)

if (-not $NoDesktop) {
    Start-Process powershell -WorkingDirectory (Join-Path $Root "desktop") -ArgumentList @(
        "-NoExit", "-Command", "dotnet run --project src/Cubicle.Desktop"
    )
}
Write-Host "Cubicle dev environment starting. Gateway: http://127.0.0.1:7878" -ForegroundColor Green
