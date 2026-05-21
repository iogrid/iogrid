# build-nupkg.ps1 — pack the iogridd Windows binary into a Squirrel.Windows
# .nupkg suitable for delta-update consumption by Update.exe.
#
# Pipeline:
#   1. Stage iogridd.exe + Update.exe (from fetch-squirrel.ps1) + LICENSE
#      under installer\windows\squirrel\staging\
#   2. Render nuspec.template.xml -> nuspec.xml with __VERSION__ + __ARCH__
#      substituted in.
#   3. Invoke `nuget.exe pack nuspec.xml` to produce iogridd.<ver>.nupkg
#   4. Rename to the Squirrel convention iogridd-<ver>-full.nupkg, drop
#      under <OutDir>\Releases\
#   5. Compute SHA-1 + file size and write a RELEASES line for use by
#      generate-releases.ps1
#   6. Authenticode-sign the .nupkg if WINDOWS_CERT_PFX_BASE64 + PASSWORD
#      are present (same gate the .msi build uses in build.ps1)
#
# Usage:
#   .\build-nupkg.ps1 -DaemonExe ..\..\daemon\target\release\iogridd.exe `
#                     -Version 0.1.0 `
#                     -Arch x64 `
#                     -OutDir ..\dist
#
# Optional env (CI sets these; absent = unsigned artifact):
#   $env:WINDOWS_CERT_PFX_BASE64   pfx file base64-encoded
#   $env:WINDOWS_CERT_PASSWORD     password to unlock the pfx
#   $env:SQUIRREL_TARBALL_SHA256   forwarded to fetch-squirrel.ps1

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$DaemonExe,

    [ValidateSet('x64','arm64')]
    [string]$Arch = 'x64',

    [Parameter(Mandatory=$true)]
    [string]$Version,

    [string]$OutDir = 'dist'
)

$ErrorActionPreference = 'Stop'
function Section { param([string]$m) Write-Host ('=' * 60) -ForegroundColor DarkGray; Write-Host "[squirrel] $m" -ForegroundColor Cyan; Write-Host ('=' * 60) -ForegroundColor DarkGray }

if (-not (Test-Path $DaemonExe)) { throw "Daemon exe not found: $DaemonExe" }

$scriptDir   = Split-Path -Parent $MyInvocation.MyCommand.Path
$stage       = Join-Path $scriptDir 'staging'
$nuspecOut   = Join-Path $stage 'iogridd.nuspec'
$nuspecIn    = Join-Path $scriptDir 'nuspec.template.xml'

Section "Stage payload under $stage"
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Path $stage | Out-Null

Copy-Item -Path $DaemonExe -Destination (Join-Path $stage 'iogridd.exe') -Force
Set-Content -Path (Join-Path $stage 'LICENSE.txt') -Value @"
iogrid client - Apache License 2.0
https://github.com/iogrid/iogrid/blob/main/LICENSE
"@
Set-Content -Path (Join-Path $stage 'version.txt') -Value $Version

Section "Fetch + stage Squirrel runtime"
# Drops Squirrel.exe + Update.exe into $stage. Update.exe will be packed
# into the .nupkg as Squirrel.exe (the convention Squirrel.Update expects
# when staging app-<version>\ subdirs at install time).
& (Join-Path $scriptDir 'fetch-squirrel.ps1') -OutDir $stage
if ($LASTEXITCODE -ne 0) { throw "fetch-squirrel.ps1 failed (exit $LASTEXITCODE)" }

Section "Render nuspec.xml (version=$Version arch=$Arch)"
$nuspecText = Get-Content $nuspecIn -Raw
$nuspecText = $nuspecText.Replace('__VERSION__', $Version).Replace('__ARCH__', $Arch)
Set-Content -Path $nuspecOut -Value $nuspecText -Encoding UTF8
Write-Host "Wrote $nuspecOut"

Section "Locate nuget.exe"
# Squirrel.Windows .releasify path historically depended on nuget.exe being
# on PATH. GitHub windows-latest runners ship nuget already; the absolute
# path lookup below is a best-effort to handle local dev too.
$nuget = (Get-Command nuget.exe -ErrorAction SilentlyContinue)?.Path
if (-not $nuget) {
    # On windows-latest the chocolatey-installed nuget lives here:
    $candidate = 'C:\ProgramData\chocolatey\bin\nuget.exe'
    if (Test-Path $candidate) { $nuget = $candidate }
}
if (-not $nuget) {
    # Fallback: download the standalone nuget bootstrapper.
    $nuget = Join-Path $env:TEMP 'nuget.exe'
    Write-Host "[squirrel] nuget.exe not on PATH — downloading to $nuget"
    Invoke-WebRequest -Uri 'https://dist.nuget.org/win-x86-commandline/latest/nuget.exe' `
                      -OutFile $nuget -UseBasicParsing
}
Write-Host "Using nuget: $nuget"

Section "Pack .nupkg"
$absOutDir = if ([System.IO.Path]::IsPathRooted($OutDir)) {
    $OutDir
} else {
    Join-Path (Get-Location).Path $OutDir
}
$releasesDir = Join-Path $absOutDir 'Releases'
New-Item -ItemType Directory -Path $releasesDir -Force | Out-Null

Push-Location $stage
try {
    & $nuget pack 'iogridd.nuspec' -Version $Version -OutputDirectory $releasesDir -NoPackageAnalysis
    if ($LASTEXITCODE -ne 0) { throw "nuget pack failed (exit $LASTEXITCODE)" }
} finally {
    Pop-Location
}

# nuget.exe emits iogridd.<Version>.nupkg; Squirrel expects iogridd-<Version>-full.nupkg.
$nugetOut = Join-Path $releasesDir "iogridd.$Version.nupkg"
$squirrelOut = Join-Path $releasesDir "iogridd-$Version-full.nupkg"
if (-not (Test-Path $nugetOut)) {
    # Some nuget versions emit a different casing; locate by glob.
    $nugetOut = Get-ChildItem -Path $releasesDir -Filter "iogridd.$Version.nupkg" |
                Select-Object -First 1 -ExpandProperty FullName
    if (-not $nugetOut) { throw "nuget didn't produce a .nupkg under $releasesDir" }
}
if (Test-Path $squirrelOut) { Remove-Item -Force $squirrelOut }
Move-Item -Path $nugetOut -Destination $squirrelOut
Write-Host "Packed: $squirrelOut" -ForegroundColor Green

Section "Sign (if cert provided)"
if ($env:WINDOWS_CERT_PFX_BASE64 -and $env:WINDOWS_CERT_PASSWORD) {
    $pfx = Join-Path $env:TEMP 'iogrid-signing-squirrel.pfx'
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_CERT_PFX_BASE64))
    try {
        # Sign the .nupkg using nuget's native sign command (Authenticode
        # under the hood). Update.exe is signed at .msi install time
        # by build.ps1's signtool block already, so we don't double-sign
        # the staged Update.exe here.
        & $nuget sign $squirrelOut `
            -CertificatePath $pfx `
            -CertificatePassword $env:WINDOWS_CERT_PASSWORD `
            -Timestamper http://timestamp.digicert.com
        if ($LASTEXITCODE -ne 0) { throw "nuget sign failed (exit $LASTEXITCODE)" }
        Write-Host "Signed: $squirrelOut" -ForegroundColor Green
    } finally {
        Remove-Item -Force $pfx -ErrorAction SilentlyContinue
    }
} else {
    Write-Host "WINDOWS_CERT_PFX_BASE64 not set — skipping signing (CI artifact will be unsigned)" -ForegroundColor Yellow
}

Section "Emit RELEASES manifest line"
# Squirrel RELEASES file format: one line per .nupkg, three space-separated fields:
#   <SHA-1 hex (uppercase)>  <filename>  <bytes>
# generate-releases.ps1 reads these per-build lines + concatenates them into
# the final RELEASES index at publish time.
$sha1 = (Get-FileHash -Algorithm SHA1 $squirrelOut).Hash.ToUpperInvariant()
$size = (Get-Item $squirrelOut).Length
$relFile = Split-Path -Leaf $squirrelOut
$lineFile = Join-Path $releasesDir "$relFile.RELEASES-line"
$line = "$sha1 $relFile $size"
Set-Content -Path $lineFile -Value $line -Encoding ASCII -NoNewline
Write-Host "RELEASES line: $line"
Write-Host "Wrote: $lineFile"
exit 0
