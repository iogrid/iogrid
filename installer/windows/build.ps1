# build.ps1 — build (and optionally sign) the iogrid Windows .msi.
#
# Prerequisites (CI installs these):
#   dotnet tool install --global wix             # WiX 4 toolset
#   wix extension add -g WixToolset.UI.wixext
#   wix extension add -g WixToolset.Util.wixext
#
# Inputs:
#   -DaemonExe   path to a built iogridd.exe (e.g. from daemon-ci artifact)
#   -Arch        x64 | arm64 (default x64)
#   -Version     SemVer string; default 0.1.0
#   -OutDir      where to drop the .msi; default dist/
#
# Signing (optional — gated on $env:WINDOWS_CERT_PFX_BASE64 + PASS):
#   $env:WINDOWS_CERT_PFX_BASE64   pfx file base64-encoded (CI secret)
#   $env:WINDOWS_CERT_PASSWORD     password to unlock the pfx
#
# CI runs this with no signing env vars → unsigned .msi artifact is
# produced. Releases set the env vars → signed .msi.

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$DaemonExe,

    [ValidateSet('x64','arm64')]
    [string]$Arch = 'x64',

    [string]$Version = '0.1.0',

    [string]$OutDir = 'dist'
)

$ErrorActionPreference = 'Stop'

function Section { param([string]$m) Write-Host ('=' * 60) -ForegroundColor DarkGray; Write-Host "[wix] $m" -ForegroundColor Cyan; Write-Host ('=' * 60) -ForegroundColor DarkGray }

Section "Stage payload"
if (-not (Test-Path $DaemonExe)) { throw "Daemon exe not found: $DaemonExe" }

$stage = Join-Path $PSScriptRoot 'staging'
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Path $stage | Out-Null

Copy-Item -Path $DaemonExe -Destination (Join-Path $stage 'iogridd.exe') -Force
Set-Content -Path (Join-Path $stage 'LICENSE.txt') -Value @"
iogrid client - Apache License 2.0
https://github.com/iogrid/iogrid/blob/main/LICENSE
"@

# WiX needs RTF. Smallest valid RTF that renders in WixUI_Minimal.
@"
{\rtf1\ansi\deff0
{\fonttbl{\f0 \fswiss Helvetica;}}
\f0\fs20
By installing iogrid you agree to the iogrid Terms of Service at iogrid.org/legal.\par
The iogrid client is licensed under the Apache License 2.0.\par
}
"@ | Set-Content -Path (Join-Path $stage 'License.rtf') -Encoding ASCII

Section "Locate wix.exe"
# GitHub-hosted `windows-latest` runners ship a pre-installed WiX 7 that
# appears earlier in $env:PATH than the dotnet-tools dir where our pinned
# WiX 4.0.6 lands. `Get-Command wix` therefore resolves to v7, which
# refuses to run unattended without accepting the Open Source Maintenance
# Fee EULA (error WIX7015) and breaks the build. Bypass PATH entirely:
# resolve the dotnet-tools wix.exe directly so we always invoke 4.0.6.
$dotnetToolsDir = if ($env:DOTNET_TOOLS) {
    $env:DOTNET_TOOLS
} else {
    Join-Path $env:USERPROFILE '.dotnet\tools'
}
$wixExe = Join-Path $dotnetToolsDir 'wix.exe'
if (-not (Test-Path $wixExe)) {
    # Fallback: look on PATH but warn loudly if it resolves to v5+ so the
    # failure is obvious.
    $cmd = Get-Command wix -ErrorAction SilentlyContinue
    if (-not $cmd) {
        throw "WiX 4 toolset not found at $wixExe and no 'wix' on PATH. Install with: dotnet tool install --global wix --version 4.0.6"
    }
    $wixExe = $cmd.Path
    Write-Host "WARN: falling back to PATH-resolved wix: $wixExe" -ForegroundColor Yellow
}
Write-Host "Using WiX: $wixExe"
& $wixExe --version

Section "Add WiX extensions"
# Idempotent; ignore non-zero exit (e.g. "already installed"). Pin to
# 4.0.6 to avoid pulling WiX 5+ which now requires accepting the
# Open Source Maintenance Fee EULA on every invocation (incompatible
# with unattended CI). 4.0.x is the last fully MIT-licensed line.
# Invoke via the absolute path so we don't accidentally hit the
# runner's pre-installed WiX 7 (see "Locate wix.exe" above).
& $wixExe extension add -g WixToolset.UI.wixext/4.0.6 2>$null
& $wixExe extension add -g WixToolset.Util.wixext/4.0.6 2>$null

Section "Build .msi"
$absOutDir = if ([System.IO.Path]::IsPathRooted($OutDir)) {
    $OutDir
} else {
    Join-Path (Get-Location).Path $OutDir
}
New-Item -ItemType Directory -Path $absOutDir -Force | Out-Null
$msi = Join-Path $absOutDir "iogrid-$Version-$Arch.msi"

# WiX resolves File@Source paths relative to its CURRENT WORKING
# DIRECTORY, not relative to the .wxs file. Our .wxs references
# `staging/iogridd.exe`; the staging dir lives next to the .wxs
# (installer/windows/staging/). Push-Location into that dir so the
# relative paths resolve.
Push-Location $PSScriptRoot
try {
    # Use $wixExe (absolute path to the dotnet-tools wix 4.0.6) instead
    # of bare `wix` so the runner's pre-installed WiX 7 on PATH is never
    # consulted. See "Locate wix.exe" above for the rationale.
    & $wixExe build 'iogrid.wxs' `
        -arch $Arch `
        -d "DaemonExeArch=$Arch" `
        -d "Version=$Version" `
        -ext WixToolset.UI.wixext/4.0.6 `
        -ext WixToolset.Util.wixext/4.0.6 `
        -out $msi
    if ($LASTEXITCODE -ne 0) { throw "wix build failed" }
} finally {
    Pop-Location
}
Write-Host "Built: $msi" -ForegroundColor Green

Section "Sign (if cert provided)"
if ($env:WINDOWS_CERT_PFX_BASE64 -and $env:WINDOWS_CERT_PASSWORD) {
    $pfx = Join-Path $env:TEMP 'iogrid-signing.pfx'
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_CERT_PFX_BASE64))
    try {
        & signtool sign `
            /fd SHA256 `
            /f $pfx `
            /p $env:WINDOWS_CERT_PASSWORD `
            /tr http://timestamp.digicert.com `
            /td SHA256 `
            $msi
        if ($LASTEXITCODE -ne 0) { throw "signtool failed" }
        Write-Host "Signed: $msi" -ForegroundColor Green
    } finally {
        Remove-Item -Force $pfx -ErrorAction SilentlyContinue
    }
} else {
    Write-Host "WINDOWS_CERT_PFX_BASE64 not set — skipping signing (CI artifact will be unsigned)" -ForegroundColor Yellow
}
