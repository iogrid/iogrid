# generate-releases.ps1 — produce the Squirrel `RELEASES` index file.
#
# RELEASES is a flat newline-separated text file Squirrel's Update.exe
# fetches over HTTP. Each line is:
#
#   <SHA-1 hex (uppercase)>  <nupkg filename>  <bytes>
#
# Update.exe diffs the local installed-version line against the highest
# line in RELEASES, downloads the corresponding -full.nupkg (or, if
# `-delta` package is present, the per-version delta), verifies the
# SHA-1 + length, then stages app-<version>\ next to the existing one.
#
# This script:
#   1. Globs <ReleasesDir>\*.RELEASES-line files (one per .nupkg
#      emitted by build-nupkg.ps1)
#   2. Sorts them by SemVer parsed from the line
#   3. Writes the concatenated RELEASES index alongside
#   4. Idempotent — safe to re-run; existing RELEASES is overwritten
#
# Usage:
#   .\generate-releases.ps1 -ReleasesDir installer\windows\dist\Releases
#
# CI calls this after build-nupkg.ps1 has emitted one or more per-version
# lines; locally you may invoke it manually if you've packed multiple
# .nupkg into the same dir.

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$ReleasesDir
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path $ReleasesDir)) {
    throw "Releases dir not found: $ReleasesDir"
}

# Collect per-build RELEASES-line fragments emitted by build-nupkg.ps1.
$fragments = Get-ChildItem -Path $ReleasesDir -Filter '*.RELEASES-line' -ErrorAction SilentlyContinue
if (-not $fragments) {
    Write-Host "[squirrel-releases] WARN: no *.RELEASES-line fragments found under $ReleasesDir — RELEASES will be empty" -ForegroundColor Yellow
    Set-Content -Path (Join-Path $ReleasesDir 'RELEASES') -Value '' -Encoding ASCII
    exit 0
}

# Parse each fragment into (sha1, file, bytes, version) tuples and sort
# by SemVer ascending. Update.exe consumes the file top-down and treats
# the last line as the latest available version; ascending order means
# the highest version sits at the bottom.
$entries = foreach ($f in $fragments) {
    $line = (Get-Content $f -Raw).Trim()
    if (-not $line) { continue }
    $parts = $line -split '\s+'
    if ($parts.Count -ne 3) {
        Write-Host "[squirrel-releases] WARN: malformed line in $($f.Name): '$line' — skipping" -ForegroundColor Yellow
        continue
    }
    # Filename is iogridd-<version>-full.nupkg or iogridd-<version>-delta.nupkg.
    if ($parts[1] -match 'iogridd-(?<ver>\d+\.\d+\.\d+(?:-[\w\.]+)?)-(?<kind>full|delta)\.nupkg$') {
        $ver = [System.Version]($Matches.ver -replace '-.*$', '')
        $kind = $Matches.kind
    } else {
        Write-Host "[squirrel-releases] WARN: cannot parse version from '$($parts[1])' — sort order undefined" -ForegroundColor Yellow
        $ver = [System.Version]'0.0.0'
        $kind = 'unknown'
    }
    [pscustomobject]@{
        Sha1    = $parts[0]
        File    = $parts[1]
        Bytes   = [int]$parts[2]
        Version = $ver
        Kind    = $kind
        Line    = $line
    }
}

# Sort: ascending by version, full before delta within the same version
# (Squirrel applies the full snapshot first, then any deltas).
$sorted = $entries | Sort-Object @{Expression='Version'}, @{Expression={ if ($_.Kind -eq 'full') { 0 } else { 1 } }}

$releasesPath = Join-Path $ReleasesDir 'RELEASES'
# Newline-LF (Squirrel's parser accepts CRLF too but LF is canonical).
$content = ($sorted | ForEach-Object { $_.Line }) -join "`n"
Set-Content -Path $releasesPath -Value $content -Encoding ASCII
Write-Host "Wrote RELEASES ($($sorted.Count) entries) -> $releasesPath" -ForegroundColor Green
Get-Content $releasesPath
exit 0
