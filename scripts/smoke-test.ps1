<#
.SYNOPSIS
  End-to-end smoke test: real gateway + real agent worker (demo provider).

  Starts the gateway on a spare port with a throwaway home directory, lets it
  supervise `uv run cubicle-agents`, submits an assignment through the owner API
  and waits until the staff deliver the result.
#>
param([int]$Port = 7979, [int]$TimeoutSeconds = 90)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Home_ = Join-Path ([IO.Path]::GetTempPath()) ("cubicle-smoke-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force $Home_ | Out-Null
$Base = "http://127.0.0.1:$Port"

$gatewayExe = Join-Path $Home_ "cubicle-gateway.exe"
Push-Location (Join-Path $Root "gateway"); go build -o $gatewayExe ./cmd/cubicle-gateway; Pop-Location
if ($LASTEXITCODE) { throw "go build failed" }

# Let the gateway supervise the worker straight from source.
@{
    worker_command = "uv"
    worker_args    = @("run", "--project", (Join-Path $Root "agents"), "cubicle-agents")
} | ConvertTo-Json | Set-Content -Encoding ascii (Join-Path $Home_ "config.json")

$env:CUBICLE_HOME = $Home_
$env:CUBICLE_ADDR = "127.0.0.1:$Port"
Remove-Item Env:ANTHROPIC_API_KEY -ErrorAction SilentlyContinue  # no keys stored: the office runs in demo mode

$proc = Start-Process $gatewayExe -ArgumentList "serve" -PassThru -WindowStyle Hidden `
    -RedirectStandardError (Join-Path $Home_ "stderr.log") -RedirectStandardOutput (Join-Path $Home_ "stdout.log")
try {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try { Invoke-RestMethod "$Base/healthz" -TimeoutSec 2 | Out-Null; break } catch { Start-Sleep -Milliseconds 300 }
    }
    $token = (Get-Content (Join-Path $Home_ "admin.token") -Raw).Trim()
    $h = @{ Authorization = "Bearer $token" }

    # Wait for the staff to clock in.
    do {
        Start-Sleep -Milliseconds 500
        $staff = Invoke-RestMethod "$Base/api/v1/agents" -Headers $h
    } while ((Get-Date) -lt $deadline -and @($staff | Where-Object status -ne "offline").Count -eq 0)
    Write-Host "Staff online: $(@($staff).Count)"

    $body = @{ title = "Plan my week"; description = "Smoke test assignment" } | ConvertTo-Json
    $task = Invoke-RestMethod "$Base/api/v1/assignments" -Method Post -Headers $h -Body $body -ContentType "application/json"
    Write-Host "Assignment $($task.id) created"

    do {
        Start-Sleep -Seconds 1
        $detail = Invoke-RestMethod "$Base/api/v1/tasks/$($task.id)" -Headers $h
        Write-Host "  status: $($detail.status) ($(@($detail.children).Count) subtasks)"
    } while ((Get-Date) -lt $deadline -and $detail.status -notin @("done", "failed", "cancelled"))

    if ($detail.status -ne "done") { throw "assignment ended as '$($detail.status)': $($detail.error)" }
    Write-Host "`nRESULT:`n$($detail.result)`n" -ForegroundColor Green

    # A new agent from a template is picked up by the running worker without a restart.
    $new = Invoke-RestMethod "$Base/api/v1/agents" -Method Post -Headers $h -ContentType "application/json" `
        -Body (@{ template_id = "travel-planner" } | ConvertTo-Json)
    do {
        Start-Sleep -Milliseconds 500
        $agent = Invoke-RestMethod "$Base/api/v1/agents/$($new.id)" -Headers $h
    } while ((Get-Date) -lt $deadline -and $agent.status -eq "offline")
    if ($agent.status -eq "offline") { throw "new agent $($new.id) was not picked up by the worker" }
    Write-Host "New agent online without a restart: $($agent.name) ($($agent.status))"

    # Chat with it.
    $conv = Invoke-RestMethod "$Base/api/v1/conversations" -Method Post -Headers $h -ContentType "application/json" `
        -Body (@{ agent_id = $new.id } | ConvertTo-Json)
    Invoke-RestMethod "$Base/api/v1/conversations/$($conv.id)/messages" -Method Post -Headers $h -ContentType "application/json" `
        -Body (@{ content = "Plan a weekend in Lisbon" } | ConvertTo-Json) | Out-Null
    do {
        Start-Sleep -Milliseconds 500
        $thread = Invoke-RestMethod "$Base/api/v1/conversations/$($conv.id)" -Headers $h
    } while ((Get-Date) -lt $deadline -and @($thread.messages).Count -lt 2)
    $reply = @($thread.messages)[-1]
    if ($reply.role -ne "agent" -or $reply.status -ne "done") { throw "no chat reply: $($thread | ConvertTo-Json -Depth 5)" }
    Write-Host "Chat reply: $($reply.content.Substring(0, [Math]::Min(80, $reply.content.Length)))…"

    # An MCP server is started and listed through the worker (the app's Test button).
    $python = Join-Path $Root "agents\.venv\Scripts\python.exe"
    $mcp = Invoke-RestMethod "$Base/api/v1/mcp-servers" -Method Post -Headers $h -ContentType "application/json" -Body (@{
        name = "Echo"; transport = "stdio"; command = $python; args = @("-I", (Join-Path $Root "agents\tests\mcp_echo_server.py"))
    } | ConvertTo-Json)
    $test = Invoke-RestMethod "$Base/api/v1/mcp-servers/$($mcp.id)/test" -Method Post -Headers $h -TimeoutSec 120
    if ($test.error -or @($test.tools).Count -lt 1) { throw "MCP test failed: $($test.error)" }
    Write-Host "MCP tools via worker: $((@($test.tools) | ForEach-Object name) -join ', ')"

    # Live model listing runs the worker in "models" mode; with no Ollama server it must report a clean error.
    $models = Invoke-RestMethod "$Base/api/v1/providers/ollama/models" -Headers $h -TimeoutSec 90
    if (-not ($models.live -or $models.error)) { throw "model listing did not reach the worker" }
    Write-Host "Model listing via worker: live=$($models.live) error='$($models.error)'"
    # Onboarding: the main agent writes a persona for a custom passion, then setup applies it.
    $pt = Invoke-RestMethod "$Base/api/v1/persona" -Method Post -Headers $h -ContentType "application/json" `
        -Body (@{ passion = "urban foxes" } | ConvertTo-Json)
    $deadline = (Get-Date).AddSeconds(60); $persona = $null
    while ((Get-Date) -lt $deadline) {
        $ps = Invoke-RestMethod "$Base/api/v1/persona/$($pt.task_id)" -Headers $h
        if ($ps.status -eq "done") { $persona = $ps.persona; break }
        if ($ps.status -in "failed", "cancelled") { throw "persona failed: $($ps.error)" }
        Start-Sleep -Milliseconds 500
    }
    if (-not $persona) { throw "persona was not written in time" }
    $ob = Invoke-RestMethod "$Base/api/v1/onboarding" -Method Post -Headers $h -ContentType "application/json" `
        -Body (@{ agent_name = "Atlas"; profession = "software-engineer"; passion = "custom"; custom_passion = "urban foxes"; persona = $persona } | ConvertTo-Json -Depth 5)
    if (-not $ob.profile.onboarded -or @($ob.hired).Count -ne 4) { throw "onboarding did not hire the team" }
    Write-Host "Onboarding: $($ob.profile.agent_name) hired $(@($ob.hired).Count) teammates, accent $($ob.profile.passion.accent_color)"
    Write-Host "SMOKE TEST PASSED" -ForegroundColor Green
}
catch {
    Write-Host "SMOKE TEST FAILED: $_" -ForegroundColor Red
    Get-Content (Join-Path $Home_ "stderr.log") -Tail 40 -ErrorAction SilentlyContinue
    exit 1
}
finally {
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
    Get-Process cubicle-agents, python -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "*agents*" } | Stop-Process -Force -ErrorAction SilentlyContinue
}
