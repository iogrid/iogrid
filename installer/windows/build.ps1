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
$wix = Get-Command wix -ErrorAction SilentlyContinue
if (-not $wix) { throw "WiX 4 toolset not found. Install with: dotnet tool install --global wix" }
Write-Host "Using WiX: $($wix.Path)"

Section "Add WiX extensions"
# Idempotent; ignore non-zero exit (e.g. "already installed"). Pin to
# 4.0.6 to avoid pulling WiX 5+ which now requires accepting the
# Open Source Maintenance Fee EULA on every invocation (incompatible
# with unattended CI). 4.0.x is the last fully MIT-licensed line.
wix extension add -g WixToolset.UI.wixext/4.0.6 2>$null
wix extension add -g WixToolset.Util.wixext/4.0.6 2>$null

Section "Build .msi"
New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
$msi = Join-Path $OutDir "iogrid-$Version-$Arch.msi"
$wxs = Join-Path $PSScriptRoot 'iogrid.wxs'
& wix build $wxs `
    -arch $Arch `
    -d "DaemonExeArch=$Arch" `
    -d "Version=$Version" `
    -ext WixToolset.UI.wixext `
    -ext WixToolset.Util.wixext `
    -out $msi
if ($LASTEXITCODE -ne 0) { throw "wix build failed" }
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
