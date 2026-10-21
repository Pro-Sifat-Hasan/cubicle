<#
.SYNOPSIS
  Checks everything that is about to be pushed. Cubicle is a public project, so a mistake here is public too.

.DESCRIPTION
  Runs from the git pre-push hook (install it once with scripts/install-hooks.ps1). It stops the push when:
    1. a commit being pushed adds a file that must never be public (.env, databases, keys, token files),
    2. gitleaks finds a secret or API key in the commits being pushed,
    3. a dependency the project imports has a known vulnerability (Go, Python, .NET),
    4. the linters or tests fail (set CUBICLE_SKIP_PREPUSH_TESTS=1 to skip only this step).
  Missing tools stop the push too, with how to install them, so the check is never silently skipped.
  An emergency bypass is `git push --no-verify`; CI runs the same secret scan and will still catch it.
#>
param([string]$Remote = "", [string]$Url = "")

$ErrorActionPreference = "Continue"  # native tools write progress to stderr; their exit codes decide
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
$zero = "0" * 40
$problems = @()

function Step($text) { Write-Host "`n==> $text" -ForegroundColor Yellow }
function Fail($text) { $script:problems += $text; Write-Host "  x $text" -ForegroundColor Red }
function Need($tool, $how) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Fail "$tool is not installed. $how"; return $false }
    return $true
}

# What is being pushed: git gives one line per ref on stdin: <local ref> <local sha> <remote ref> <remote sha>.
$ranges = @()
foreach ($line in @($input)) {
    $parts = "$line".Trim() -split "\s+"
    if ($parts.Count -lt 4 -or $parts[1] -eq $zero) { continue }   # deleting a branch pushes nothing
    if ($parts[3] -eq $zero) { $ranges += "$($parts[1]) --not --remotes=$Remote" }  # a new branch: everything the remote lacks
    else { $ranges += "$($parts[3])..$($parts[1])" }
}
if (-not $ranges.Count) { $ranges += "@{u}..HEAD" }              # run by hand: what is ahead of upstream

# ---- 1. files that must never be public ----
Step "Files that must not be pushed"
$forbidden = '(^|/)(\.env(\..*)?|.*\.(db|sqlite|sqlite3|pem|key|pfx|p12|jks|keystore)|id_(rsa|ed25519|ecdsa)|admin\.token|worker\.token|secrets\.key|secrets\.json)$'
$allowedNames = '\.env\.example$'
foreach ($r in $ranges) {
    $revArgs = $r -split " "
    $added = git log @revArgs --diff-filter=AM --name-only --pretty=format: 2>$null | Where-Object { $_ } | Sort-Object -Unique
    foreach ($f in $added) {
        if ($f -match $forbidden -and $f -notmatch $allowedNames) { Fail "a commit adds $f, which must never be public" }
    }
    $big = git log @revArgs --diff-filter=AM --name-only --pretty=format: 2>$null | Where-Object { $_ } | Sort-Object -Unique |
        Where-Object { (Test-Path $_) -and ((Get-Item $_).Length -gt 5MB) }
    foreach ($f in $big) { Fail "$f is larger than 5 MB; large binaries do not belong in the repository" }
}
if (-not $problems.Count) { Write-Host "  ok" -ForegroundColor Green }

# ---- 2. secrets and API keys in the commits being pushed ----
Step "Secrets and API keys (gitleaks)"
if (Need "gitleaks" "Install it: go install github.com/zricethezav/gitleaks/v8@latest  (or scoop install gitleaks)") {
    foreach ($r in $ranges) {
        gitleaks git . --config .gitleaks.toml --redact --exit-code 1 --log-opts="$r" | Out-Host
        if ($LASTEXITCODE) { Fail "gitleaks found a secret in what you are pushing. Remove it from the commit (and rotate the key if it was real)." }
    }
    # The working tree too: a key sitting in a file you have not committed yet is one `git add .` away.
    gitleaks dir . --config .gitleaks.toml --redact --exit-code 1 | Out-Host
    if ($LASTEXITCODE) { Fail "gitleaks found a secret in your working folder." }
}

# ---- 3. the dependencies the project imports ----
Step "Dependencies with known vulnerabilities"
if (Need "govulncheck" "Install it: go install golang.org/x/vuln/cmd/govulncheck@latest") {
    Push-Location (Join-Path $Root "gateway")
    govulncheck ./... | Out-Host
    if ($LASTEXITCODE) { Fail "govulncheck reports a vulnerable Go dependency or standard library version." }
    Pop-Location
}
if (Need "uv" "Install it: https://docs.astral.sh/uv/") {
    Push-Location (Join-Path $Root "agents")
    $req = Join-Path ([IO.Path]::GetTempPath()) "cubicle-requirements.txt"
    uv export --no-hashes --no-emit-project --quiet -o $req | Out-Null
    uvx --quiet pip-audit --requirement $req --disable-pip --no-deps | Out-Host
    if ($LASTEXITCODE) { Fail "pip-audit reports a vulnerable Python dependency." }
    Remove-Item $req -ErrorAction SilentlyContinue
    Pop-Location
}
if (Need "dotnet" "Install the .NET SDK.") {
    $out = dotnet list (Join-Path $Root "desktop/Cubicle.slnx") package --vulnerable --include-transitive | Out-String
    if ($out -match "has the following vulnerable packages") { Write-Host $out; Fail "a .NET package has a known vulnerability." }
    else { Write-Host "  ok" -ForegroundColor Green }
}

# ---- 4. linters and tests ----
if ($env:CUBICLE_SKIP_PREPUSH_TESTS -eq "1") {
    Write-Host "`n(tests skipped: CUBICLE_SKIP_PREPUSH_TESTS=1)" -ForegroundColor DarkYellow
} else {
    Step "Linters and tests"
    & (Join-Path $PSScriptRoot "test.ps1")
    if ($LASTEXITCODE) { Fail "linters or tests failed." }
}

if ($problems.Count) {
    Write-Host "`nPush stopped: $($problems.Count) problem(s)." -ForegroundColor Red
    $problems | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    exit 1
}
Write-Host "`nAll pre-push checks passed." -ForegroundColor Green
