<#
.SYNOPSIS
  Turns on the pre-push safety checks for this clone (run once).
#>
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
git config core.hooksPath .githooks
if ($LASTEXITCODE) { throw "git config failed" }
Write-Host "Pre-push checks are on: every push now scans for secrets, risky files and vulnerable dependencies, then runs the tests." -ForegroundColor Green
foreach ($tool in "gitleaks", "govulncheck", "uv", "dotnet") {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Write-Host "  missing: $tool (pushes are blocked until it is installed; see scripts/prepush.ps1)" -ForegroundColor Yellow }
}
