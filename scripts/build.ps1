<#
.SYNOPSIS
  Builds every Cubicle component and the Windows installer.

.DESCRIPTION
  Output:
    dist\stage\                     everything that gets installed
      Cubicle.exe                   desktop app (self-contained .NET, single file)
      cubicle-gateway.exe           Go gateway (static, no runtime needed)
      cubicle-agents\               Python agent worker (PyInstaller one-folder)
    dist\Cubicle-Setup-<ver>.exe    installer (needs Inno Setup 6: ISCC.exe)

.EXAMPLE
  ./scripts/build.ps1 -Version 0.1.0
  ./scripts/build.ps1 -SkipInstaller
#>
[CmdletBinding()]
param(
    [string]$Version = "0.1.0",
    [switch]$SkipInstaller
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Dist = Join-Path $Root "dist"
$Stage = Join-Path $Dist "stage"
$Commit = "local"
try { $c = & git -C $Root rev-parse --short HEAD 2>$null; if ($LASTEXITCODE -eq 0 -and $c) { $Commit = $c } } catch { }

function Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Yellow }

if (Test-Path $Stage) { Remove-Item -Recurse -Force $Stage }
New-Item -ItemType Directory -Force $Stage | Out-Null

Step "Gateway (Go)"
Push-Location (Join-Path $Root "gateway")
try {
    $env:CGO_ENABLED = "0"; $env:GOOS = "windows"; $env:GOARCH = "amd64"
    $ldflags = "-s -w -X github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version.Version=$Version -X github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/version.Commit=$Commit"
    go build -trimpath -ldflags $ldflags -o (Join-Path $Stage "cubicle-gateway.exe") ./cmd/cubicle-gateway
    if ($LASTEXITCODE) { throw "go build failed" }
} finally { Pop-Location }

Step "Agents (Python -> PyInstaller)"
Push-Location (Join-Path $Root "agents")
try {
    uv sync --group build
    if ($LASTEXITCODE) { throw "uv sync failed" }
    uv run --group build pyinstaller packaging/entry.py `
        --name cubicle-agents --onedir --console --noconfirm --clean `
        --collect-submodules cubicle_agents `
        --distpath $Stage `
        --workpath (Join-Path $Root "build\pyinstaller") `
        --specpath (Join-Path $Root "build\pyinstaller")
    if ($LASTEXITCODE) { throw "pyinstaller failed" }
} finally { Pop-Location }

Step "Desktop app (C# / .NET)"
dotnet publish (Join-Path $Root "desktop\src\Cubicle.Desktop\Cubicle.Desktop.csproj") `
    -c Release -r win-x64 --self-contained true `
    -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true -p:EnableCompressionInSingleFile=true `
    -p:Version=$Version -p:DebugType=none `
    -o $Stage
if ($LASTEXITCODE) { throw "dotnet publish failed" }

Copy-Item (Join-Path $Root "LICENSE") $Stage

if ($SkipInstaller) {
    Write-Host "`nStaged build in $Stage" -ForegroundColor Green
    exit 0
}

Step "Installer (Inno Setup)"
$iscc = (Get-Command iscc -ErrorAction SilentlyContinue).Source
if (-not $iscc) {
    $candidates = @("${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe", "$env:ProgramFiles\Inno Setup 6\ISCC.exe", "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe")
    $iscc = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $iscc) { throw "ISCC.exe not found. Install Inno Setup 6 (winget install JRSoftware.InnoSetup) or pass -SkipInstaller." }
& $iscc "/DAppVersion=$Version" "/DStageDir=$Stage" "/O$Dist" (Join-Path $Root "installer\cubicle.iss")
if ($LASTEXITCODE) { throw "ISCC failed" }

Write-Host "`nDone: $(Join-Path $Dist "Cubicle-Setup-$Version.exe")" -ForegroundColor Green
