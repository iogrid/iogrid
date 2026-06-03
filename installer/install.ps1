# install.ps1 — grandma-proof Windows installer for iogrid.
#
# Detects Windows version + arch, installs Docker Desktop if missing,
# drops the daemon binary to Program Files, registers it as a Windows
# Service, and opens the browser to the onboarding URL with a one-time
# pairing code.
#
# Two paths to identical end state:
#   1. iwr -useb https://iogrid.org/install/win | iex
#   2. Right-click install.ps1 -> Run with PowerShell
#
# Env knobs (all optional):
#   $env:IOGRID_VERSION       pin release; default = "latest"
#   $env:IOGRID_BASE_URL      onboarding host (default iogrid.org)
#   $env:IOGRID_RELEASE_URL   binary CDN (default releases.iogrid.org)
#   $env:IOGRID_NO_DOCKER=1   skip Docker install
#   $env:IOGRID_NO_OPEN=1     don't auto-launch browser
#
# Requires: PowerShell 5.1+ (default on Win10+). Will self-elevate via
# Start-Process -Verb RunAs if not running as Administrator. Service
# registration and Program Files write require admin.

#Requires -Version 5.1

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------------------
# Constants + defaults
# ---------------------------------------------------------------------------

$Script:IOGRID_VERSION     = if ($env:IOGRID_VERSION)     { $env:IOGRID_VERSION }     else { 'latest' }
$Script:IOGRID_BASE_URL    = if ($env:IOGRID_BASE_URL)    { $env:IOGRID_BASE_URL }    else { 'https://iogrid.org' }
$Script:IOGRID_RELEASE_URL = if ($env:IOGRID_RELEASE_URL) { $env:IOGRID_RELEASE_URL } else { 'https://releases.iogrid.org' }
$Script:IOGRID_NO_DOCKER   = if ($env:IOGRID_NO_DOCKER)   { [bool][int]$env:IOGRID_NO_DOCKER } else { $false }
$Script:IOGRID_NO_OPEN     = if ($env:IOGRID_NO_OPEN)     { [bool][int]$env:IOGRID_NO_OPEN }   else { $false }
$Script:IOGRID_PAIR_CODE   = $env:IOGRID_PAIR_CODE

$Script:InstallDir = 'C:\Program Files\iogrid'
$Script:ServiceName = 'iogridd'

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

function Write-Log    { param([string]$m) Write-Host "[iogrid] $m" -ForegroundColor Blue }
function Write-Ok     { param([string]$m) Write-Host "[iogrid] $m" -ForegroundColor Green }
function Write-Warn   { param([string]$m) Write-Host "[iogrid] $m" -ForegroundColor Yellow }
function Throw-Fatal  { param([string]$m) Write-Host "[iogrid] $m" -ForegroundColor Red; throw $m }

function Write-Banner {
    param([string]$Target)
    Write-Host ''
    Write-Host '================================================================' -ForegroundColor DarkGray
    Write-Host '  iogrid — distributed compute mesh' -ForegroundColor White
    Write-Host "  install path $Script:IOGRID_VERSION · target $Target" -ForegroundColor DarkGray
    Write-Host '================================================================' -ForegroundColor DarkGray
    Write-Host ''
}

# ---------------------------------------------------------------------------
# Self-elevation
# ---------------------------------------------------------------------------

function Test-IsAdmin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p = New-Object Security.Principal.WindowsPrincipal($id)
    return $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Invoke-SelfElevate {
    if (Test-IsAdmin) { return }
    Write-Warn 'Restarting elevated (UAC prompt incoming)'
    $scriptPath = $MyInvocation.PSCommandPath
    if (-not $scriptPath) {
        # Running via iwr|iex — re-execute from the same URL.
        $reExec = "iwr -useb https://iogrid.org/install/win | iex"
        Start-Process -FilePath 'powershell.exe' `
            -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', $reExec `
            -Verb RunAs -Wait
    } else {
        Start-Process -FilePath 'powershell.exe' `
            -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$scriptPath`"" `
            -Verb RunAs -Wait
    }
    exit 0
}

# ---------------------------------------------------------------------------
# Host detection
# ---------------------------------------------------------------------------

function Get-Arch {
    $a = (Get-CimInstance Win32_Processor | Select-Object -First 1).Architecture
    # 9 = x64, 12 = arm64 per Win32_Processor MSDN docs.
    switch ($a) {
        9  { 'amd64' }
        12 { 'arm64' }
        default { 'amd64' }
    }
}

function Get-WindowsVersion {
    return [System.Environment]::OSVersion.Version.ToString()
}

# ---------------------------------------------------------------------------
# Docker Desktop
# ---------------------------------------------------------------------------

function Test-DockerInstalled {
    if (Get-Command docker -ErrorAction SilentlyContinue) { return $true }
    if (Test-Path 'C:\Program Files\Docker\Docker\Docker Desktop.exe') { return $true }
    return $false
}

function Install-DockerDesktop {
    if ($Script:IOGRID_NO_DOCKER) {
        Write-Log 'Skipping Docker install (IOGRID_NO_DOCKER set)'
        return
    }
    if (Test-DockerInstalled) {
        Write-Ok 'Docker Desktop already installed'
        return
    }

    $arch = Get-Arch
    $installerUrl = switch ($arch) {
        'amd64' { 'https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe' }
        'arm64' { 'https://desktop.docker.com/win/main/arm64/Docker%20Desktop%20Installer.exe' }
    }

    Write-Log 'Downloading signed Docker Desktop installer ...'
    $dest = Join-Path $env:TEMP 'DockerDesktopInstaller.exe'
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $installerUrl -OutFile $dest
    } catch {
        Throw-Fatal "Failed to download Docker Desktop: $_"
    }

    Write-Log 'Running Docker Desktop installer (silent + WSL2 backend) ...'
    # --backend=wsl-2 is supported on Win10/11 amd64; arm64 also defaults
    # to WSL2 on Windows 11. install --quiet exits 0 on success, 3010 on
    # success-needs-reboot.
    $p = Start-Process -FilePath $dest -ArgumentList 'install', '--quiet', '--accept-license' -Wait -PassThru
    if ($p.ExitCode -ne 0 -and $p.ExitCode -ne 3010) {
        Throw-Fatal "Docker Desktop installer exited with code $($p.ExitCode)"
    }
    Write-Ok 'Docker Desktop installed (a reboot may be required to finish WSL2 setup)'
}

# ---------------------------------------------------------------------------
# Daemon binary
# ---------------------------------------------------------------------------

function Install-DaemonBinary {
    $arch = Get-Arch
    $binUrl = "$Script:IOGRID_RELEASE_URL/$Script:IOGRID_VERSION/iogridd-windows-$arch.exe"
    $sumUrl = "$binUrl.sha256"
    $target = Join-Path $Script:InstallDir 'iogridd.exe'

    if (-not (Test-Path $Script:InstallDir)) {
        New-Item -ItemType Directory -Path $Script:InstallDir -Force | Out-Null
    }

    Write-Log "Downloading iogridd ($arch) ..."
    $tmpBin = Join-Path $env:TEMP "iogridd-$([System.IO.Path]::GetRandomFileName()).exe"
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $binUrl -OutFile $tmpBin
    } catch {
        Throw-Fatal "Failed to download iogridd binary: $_"
    }

    # Best-effort checksum verification.
    try {
        $sumResp = Invoke-WebRequest -UseBasicParsing -Uri $sumUrl -ErrorAction Stop
        $expected = ($sumResp.Content -split '\s+')[0].Trim()
        $got = (Get-FileHash -Algorithm SHA256 $tmpBin).Hash.ToLower()
        if ($got -ne $expected.ToLower()) {
            Throw-Fatal "Checksum mismatch for iogridd.exe: got $got, expected $expected"
        }
        Write-Ok "Checksum verified"
    } catch {
        Write-Warn "No .sha256 published — skipping checksum (continuing)"
    }

    # If service running, stop it before replacement.
    $svc = Get-Service -Name $Script:ServiceName -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -eq 'Running') {
        Write-Log 'Stopping running iogridd service to swap binary'
        Stop-Service -Name $Script:ServiceName -Force
    }

    Move-Item -Path $tmpBin -Destination $target -Force
    Write-Ok "Installed iogridd to $target"
}

# ---------------------------------------------------------------------------
# Windows Service
# ---------------------------------------------------------------------------

function Register-Service {
    $exe = Join-Path $Script:InstallDir 'iogridd.exe'
    $cfgDir = Join-Path $env:ProgramData 'iogrid'
    if (-not (Test-Path $cfgDir)) {
        New-Item -ItemType Directory -Path $cfgDir -Force | Out-Null
    }

    $svc = Get-Service -Name $Script:ServiceName -ErrorAction SilentlyContinue
    if ($svc) {
        Write-Log 'Service already registered — updating binPath'
        & sc.exe config $Script:ServiceName binPath= "`"$exe`" run" start= auto | Out-Null
    } else {
        Write-Log 'Registering Windows Service'
        & sc.exe create $Script:ServiceName binPath= "`"$exe`" run" start= auto DisplayName= 'iogrid provider daemon' | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Throw-Fatal "sc.exe create failed with exit code $LASTEXITCODE"
        }
        # Recovery: restart on failure 3 times then give up.
        & sc.exe failure $Script:ServiceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null
        & sc.exe description $Script:ServiceName 'iogrid provider daemon — runs workloads, reports metrics' | Out-Null
    }

    Write-Log 'Starting service ...'
    Start-Service -Name $Script:ServiceName -ErrorAction Stop
    Write-Ok "Service '$Script:ServiceName' running"
}

# ---------------------------------------------------------------------------
# Pairing
# ---------------------------------------------------------------------------

function Get-PairingCode {
    if ($Script:IOGRID_PAIR_CODE) { return $Script:IOGRID_PAIR_CODE }

    $exe = Join-Path $Script:InstallDir 'iogridd.exe'
    # Daemon may take a moment to come up after the service start.
    for ($i = 0; $i -lt 20; $i++) {
        try {
            $code = & $exe pair --request 2>$null
            $code = ($code | Out-String).Trim()
            if ($code) { return $code }
        } catch {
            # ignore until retries exhausted
        }
        Start-Sleep -Milliseconds 500
    }
    # Fallback: read from disk.
    $pairFile = Join-Path $env:USERPROFILE '.iogrid\pairing-code.txt'
    if (Test-Path $pairFile) {
        return (Get-Content $pairFile -Raw).Trim()
    }
    return $null
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

function Invoke-Main {
    Invoke-SelfElevate
    Write-Banner "Windows $(Get-WindowsVersion) / $(Get-Arch)"

    Install-DockerDesktop
    Install-DaemonBinary
    Register-Service

    Write-Log 'Requesting pairing code from daemon ...'
    $code = Get-PairingCode
    if (-not $code) {
        Write-Warn 'Daemon did not produce a pairing code yet.'
        Write-Warn 'Run "iogridd.exe pair --request" once the service is up, then visit:'
        Write-Host "    $Script:IOGRID_BASE_URL/onboard"
        exit 4
    }

    $onboardUrl = "$Script:IOGRID_BASE_URL/onboard/$code"
    Write-Host ''
    Write-Ok "Your one-time pairing code: $code"
    Write-Ok "Onboarding URL: $onboardUrl"
    Write-Host ''

    if (-not $Script:IOGRID_NO_OPEN) {
        try {
            Start-Process $onboardUrl
            Write-Log 'Opened your browser. Complete sign-in there to finish setup.'
        } catch {
            Write-Log 'Could not auto-open browser. Visit this URL manually:'
            Write-Host "    $onboardUrl"
        }
    } else {
        Write-Log 'Visit this URL on a device with a browser to finish setup:'
        Write-Host "    $onboardUrl"
    }
    Write-Ok 'Install complete. The daemon will keep itself updated automatically.'
}

Invoke-Main
