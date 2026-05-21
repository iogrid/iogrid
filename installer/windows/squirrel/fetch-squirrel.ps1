# fetch-squirrel.ps1 — download + SHA-256-verify the Squirrel.Windows
# runtime release matching SQUIRREL_VERSION, extracting Update.exe and
# Squirrel.exe into a target staging directory.
#
# Mirrors installer/macos/sparkle/fetch-sparkle.sh — same intent
# (download a 3rd-party update runtime tarball, pin its checksum,
# stage it into the package payload), different OS toolchain.
#
# Usage:
#   .\fetch-squirrel.ps1 -OutDir installer\windows\squirrel\staging
#
# Optional env vars (CI sets these to enforce verification):
#   $env:SQUIRREL_TARBALL_SHA256   pinned SHA-256 of the upstream .zip
#                                   from https://github.com/Squirrel/
#                                   Squirrel.Windows/releases. Empty
#                                   string => WARN-but-continue (local
#                                   dev only — CI must pin).
#
# Exit codes:
#   0   ok
#   2   SHA-256 mismatch (FAIL HARD when pin is set)
#   3   download failed

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$OutDir
)

$ErrorActionPreference = 'Stop'

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$versionFile = Join-Path $scriptDir 'SQUIRREL_VERSION'
if (-not (Test-Path $versionFile)) {
    throw "SQUIRREL_VERSION pin file not found at $versionFile"
}
$version = (Get-Content $versionFile -Raw).Trim()
if (-not ($version -match '^\d+\.\d+\.\d+$')) {
    throw "SQUIRREL_VERSION '$version' does not look like a SemVer"
}

Write-Host "[squirrel] pin version: $version"

# The Squirrel.Windows release artifact is a .zip named
# `<version>.zip` containing Update.exe + Squirrel.exe +
# .NuGet helpers. The download URL pattern is the GitHub release-asset
# convention; mirror via GHCR cache if rate-limiting becomes an issue.
$assetUrl = "https://github.com/Squirrel/Squirrel.Windows/releases/download/$version/$version.zip"
$tmpZip = Join-Path $env:TEMP "squirrel-$version.zip"
$tmpExtract = Join-Path $env:TEMP "squirrel-$version-extract"

if (Test-Path $tmpZip)     { Remove-Item -Force $tmpZip }
if (Test-Path $tmpExtract) { Remove-Item -Recurse -Force $tmpExtract }

Write-Host "[squirrel] downloading $assetUrl"
try {
    Invoke-WebRequest -Uri $assetUrl -OutFile $tmpZip -UseBasicParsing
} catch {
    Write-Error "[squirrel] download failed: $_"
    exit 3
}

# SHA-256 verification — fail hard when the pin is set, warn when empty
# (Phase 2 ships with the pin empty in CI; platform team fills it in
# follow-up #389-windows-squirrel-pin once the binary is reviewed).
$pinned = $env:SQUIRREL_TARBALL_SHA256
$actual = (Get-FileHash -Algorithm SHA256 $tmpZip).Hash.ToLowerInvariant()
Write-Host "[squirrel] downloaded sha256: $actual"
if ($pinned) {
    $pinned = $pinned.Trim().ToLowerInvariant()
    if ($pinned -ne $actual) {
        Write-Error "[squirrel] SHA-256 mismatch! pinned=$pinned actual=$actual"
        exit 2
    }
    Write-Host "[squirrel] sha256 OK (matches pinned)"
} else {
    Write-Host "[squirrel] WARN: SQUIRREL_TARBALL_SHA256 unset — running unpinned. Pin before first real release." -ForegroundColor Yellow
}

Write-Host "[squirrel] extracting -> $tmpExtract"
Expand-Archive -Path $tmpZip -DestinationPath $tmpExtract -Force

# Squirrel.Windows release zips lay out:
#   Squirrel.exe        — the package-builder CLI (.releasify entry point)
#   Update.exe          — the runtime installed alongside the app
#   NuGet.exe + .dll    — helpers used by Squirrel.exe
# Copy the two we need into $OutDir; leave NuGet helpers behind (we use
# the standalone nuget.exe in build-nupkg.ps1).
New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
foreach ($name in @('Squirrel.exe', 'Update.exe')) {
    $src = Get-ChildItem -Path $tmpExtract -Filter $name -Recurse | Select-Object -First 1
    if (-not $src) { throw "[squirrel] $name not found in extracted runtime" }
    Copy-Item -Path $src.FullName -Destination (Join-Path $OutDir $name) -Force
    Write-Host "[squirrel] staged: $name"
}

Remove-Item -Force $tmpZip
Remove-Item -Recurse -Force $tmpExtract
Write-Host "[squirrel] done"
