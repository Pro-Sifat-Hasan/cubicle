<#
.SYNOPSIS
  Builds every Cubicle component and the Windows installer.

.DESCRIPTION
  Output (dist\ holds only the latest build; older versions are removed):
    dist\Cubicle-Setup-<ver>.exe                 installer (needs Inno Setup 6: ISCC.exe)
    dist\Cubicle-Portable-<ver>-win-x64.zip      portable copy, runs without installing

  Intermediate files go to build\package\:
    stage\                          everything that gets installed
      Cubicle.exe                   desktop app (self-contained .NET, single file)
      cubicle-gateway.exe           Go gateway (static, no runtime needed)
      cubicle-agents\               Python agent worker (PyInstaller one-folder)
    portable\Cubicle\               the unzipped portable copy

.EXAMPLE
  ./scripts/build.ps1 -Version 0.2.0
  ./scripts/build.ps1 -SkipInstaller
#>
[CmdletBinding()]
param(
    [string]$Version = "0.2.0",
    [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Dist = Join-Path $Root "dist"
$Work = Join-Path $Root "build\package"
$Stage = Join-Path $Work "stage"
$Commit = "local"
try { $c = & git -C $Root rev-parse --short HEAD 2>$null; if ($LASTEXITCODE -eq 0 -and $c) { $Commit = $c } } catch { }

function Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Yellow }

# Runs a native tool and fails on a non-zero exit code. Windows PowerShell 5.1 turns
# anything a tool writes to stderr (uv and pyinstaller log there) into an error when
# output is redirected, so stderr must not stop the build; the exit code decides.
function Invoke-Native([string]$what, [scriptblock]$command) {
    $ErrorActionPreference = "Continue"
    & $command
    if ($LASTEXITCODE) { throw "$what failed (exit code $LASTEXITCODE)" }
}

# Removes everything in dist\ except this build's deliverables, so only the latest
# installer and portable zip are ever there. Runs only after a successful build.
function Clear-OldBuilds([string[]]$keep) {
    Get-ChildItem $Dist -Force | Where-Object { $keep -notcontains $_.Name } | ForEach-Object {
        Write-Host "  removing old $($_.Name)"
        Remove-Item -Recurse -Force $_.FullName
    }
}

if (Test-Path $Stage) { Remove-Item -Recurse -Force $Stage }
New-Item -ItemType Directory -Force $Stage, $Dist | Out-Null

Step "Gateway (Go)"
Push-Location (Join-Path $Root "gateway")
try {
    $env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = "amd64"
    $ldflags = "-s -w -X github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version.Version=$Version -X github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version.Commit=$Commit"
    Invoke-Native "go build" { go build -trimpath -ldflags $ldflags -o (Join-Path $Stage "cubicle-gateway.exe") ./cmd/cubicle-gateway }
} finally { Pop-Location }

Step "Agents (Python -> PyInstaller)"
Push-Location (Join-Path $Root "agents")
try {
    Invoke-Native "uv sync" { uv sync --group build }
    Invoke-Native "pyinstaller" { uv run --group build pyinstaller packaging/entry.py `
        --name cubicle-agents --onedir --console --noconfirm --clean `
        --collect-submodules cubicle_agents `
        --collect-submodules pydantic_ai.models --collect-submodules pydantic_ai.providers `
        --collect-submodules pydantic_ai.capabilities --collect-submodules fastmcp.client --collect-submodules mcp.client `
        --copy-metadata pydantic_ai_slim --copy-metadata fastmcp_slim --copy-metadata mcp --copy-metadata genai_prices `
        --distpath $Stage `
        --workpath (Join-Path $Root "build\pyinstaller") `
        --specpath (Join-Path $Root "build\pyinstaller") }
} finally { Pop-Location }

Step "Desktop app (C# / .NET)"
Invoke-Native "dotnet publish" { dotnet publish (Join-Path $Root "desktop\src\Cubicle.Desktop\Cubicle.Desktop.csproj") `
    -c Release -r win-x64 --self-contained true `
    -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true -p:EnableCompressionInSingleFile=true `
    -p:Version=$Version -p:DebugType=none `
    -o $Stage }

Copy-Item (Join-Path $Root "LICENSE") $Stage

Step "Portable zip"
$portableRoot = Join-Path $Work "portable"
$portableApp = Join-Path $portableRoot "Cubicle"
if (Test-Path $portableRoot) { Remove-Item -Recurse -Force $portableRoot }
New-Item -ItemType Directory -Force $portableApp | Out-Null
Copy-Item -Recurse (Join-Path $Stage "*") $portableApp
@"
Cubicle portable mode.

While this file sits next to Cubicle.exe, Cubicle keeps all of its data in the
'data' folder here and uses port $(7879), so it runs alongside an installed copy
without touching it. Delete this file to use the normal per-user data folder.
"@ | Set-Content -Encoding utf8 (Join-Path $portableApp "portable.txt")
$zipName = "Cubicle-Portable-$Version-win-x64.zip"
$zip = Join-Path $Dist $zipName
if (Test-Path $zip) { Remove-Item -Force $zip }
# System.IO.Compression is faster and more reliable than Compress-Archive on large trees.
Add-Type -AssemblyName System.IO.Compression.FileSystem
[System.IO.Compression.ZipFile]::CreateFromDirectory($portableRoot, $zip, [System.IO.Compression.CompressionLevel]::Optimal, $false)

if ($SkipInstaller) {
    Clear-OldBuilds @($zipName)
    Write-Host "`nDone: $zip (staged build in $Stage)" -ForegroundColor Green
    exit 0
}

Step "Installer (Inno Setup)"
$iscc = (Get-Command iscc -ErrorAction SilentlyContinue).Source
if (-not $iscc) {
    $candidates = @("${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe", "$env:ProgramFiles\Inno Setup 6\ISCC.exe", "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe")
    $iscc = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $iscc) { throw "ISCC.exe not found. Install Inno Setup 6 (winget install JRSoftware.InnoSetup) or pass -SkipInstaller." }
Invoke-Native "ISCC" { & $iscc "/DAppVersion=$Version" "/DStageDir=$Stage" "/O$Dist" (Join-Path $Root "installer\cubicle.iss") }

$setupName = "Cubicle-Setup-$Version.exe"
Clear-OldBuilds @($setupName, $zipName)
Write-Host "`nDone:`n  $(Join-Path $Dist $setupName)`n  $zip" -ForegroundColor Green
