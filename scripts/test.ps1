<#
.SYNOPSIS
  Runs every test suite and linter (same checks as CI).
#>
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$failed = @()

function Run($name, $dir, [scriptblock]$block) {
    Write-Host "`n==> $name" -ForegroundColor Yellow
    Push-Location (Join-Path $Root $dir)
    try { & $block; if ($LASTEXITCODE) { $script:failed += $name } }
    catch { $script:failed += $name; Write-Host $_ -ForegroundColor Red }
    finally { Pop-Location }
}

Run "gateway: vet + test" "gateway" { go vet ./...; if (-not $LASTEXITCODE) { go test ./... } }
Run "agents: lint"        "agents"  { uv run ruff check . }
Run "agents: test"        "agents"  { uv run pytest -q }
Run "desktop: test"       "desktop" { dotnet test Cubicle.slnx }

if ($failed.Count) { Write-Host "`nFAILED: $($failed -join ', ')" -ForegroundColor Red; exit 1 }
Write-Host "`nAll checks passed." -ForegroundColor Green
