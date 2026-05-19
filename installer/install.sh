#!/usr/bin/env bash
# install.sh — grandma-proof curl-pipe-sh installer for iogrid.
#
# Detects host OS (macOS / Linux), arch (arm64 / x86_64), distro on Linux
# (apt / dnf / pacman / apk). Installs Docker if missing (via each OS's
# canonical signed channel), drops the daemon binary, registers the
# service, mints a one-time pairing code, opens the browser to the
# onboarding URL.
#
# Two paths to identical end state:
#   1. curl -fsSL https://iogrid.org/install/mac   | sh
#   2. curl -fsSL https://iogrid.org/install/linux | sudo sh
#
# Env knobs (all optional):
#   IOGRID_VERSION=v0.1.0       pin a release; default = "latest"
#   IOGRID_PREFIX=/usr/local    install root (Mac uses /usr/local/iogrid;
#                               Linux uses /usr/local/bin)
#   IOGRID_BASE_URL=https://app.iogrid.org   onboard URL host
#   IOGRID_RELEASE_URL=https://releases.iogrid.org   binary CDN
#   IOGRID_NO_DOCKER=1          skip Docker install (already managed)
#   IOGRID_NO_OPEN=1            don't auto-open the browser; print URL
#   IOGRID_PAIR_CODE=ABCDEF     pre-supplied pairing code (CI / scripted)
#   IOGRID_HEADLESS=1           force headless mode (no X11 / Aqua)
#
# Exit codes:
#   0  success
#   1  user-cancelled or unsupported host
#   2  download / verification failure
#   3  service registration failure
#   4  pairing failure
#
# This script is sourced into POSIX sh on stdin via curl-pipe; keep it
# portable to dash + bash. Avoid bashisms in top-level (subshells of
# functions may use bash if shebanged accordingly).

set -eu

# ---------------------------------------------------------------------------
# Constants + defaults
# ---------------------------------------------------------------------------

IOGRID_VERSION="${IOGRID_VERSION:-latest}"
IOGRID_BASE_URL="${IOGRID_BASE_URL:-https://app.iogrid.org}"
IOGRID_RELEASE_URL="${IOGRID_RELEASE_URL:-https://releases.iogrid.org}"
IOGRID_NO_DOCKER="${IOGRID_NO_DOCKER:-0}"
IOGRID_NO_OPEN="${IOGRID_NO_OPEN:-0}"
IOGRID_HEADLESS="${IOGRID_HEADLESS:-0}"
IOGRID_PAIR_CODE="${IOGRID_PAIR_CODE:-}"

# ANSI colours — disabled if stdout is not a TTY (curl-pipe-sh sets that).
if [ -t 1 ]; then
    C_BOLD=$(printf '\033[1m')
    C_DIM=$(printf '\033[2m')
    C_RED=$(printf '\033[31m')
    C_GREEN=$(printf '\033[32m')
    C_YELLOW=$(printf '\033[33m')
    C_BLUE=$(printf '\033[34m')
    C_RESET=$(printf '\033[0m')
else
    C_BOLD=""
    C_DIM=""
    C_RED=""
    C_GREEN=""
    C_YELLOW=""
    C_BLUE=""
    C_RESET=""
fi

log() {
    printf "%s[iogrid]%s %s\n" "$C_BLUE" "$C_RESET" "$*"
}

ok() {
    printf "%s[iogrid]%s %s\n" "$C_GREEN" "$C_RESET" "$*"
}

warn() {
    printf "%s[iogrid]%s %s\n" "$C_YELLOW" "$C_RESET" "$*" >&2
}

die() {
    printf "%s[iogrid]%s %s\n" "$C_RED" "$C_RESET" "$*" >&2
    exit 1
}

banner() {
    printf "\n%s================================================================%s\n" "$C_DIM" "$C_RESET"
    printf "%s  iogrid — distributed compute mesh  %s\n" "$C_BOLD" "$C_RESET"
    printf "%s  install path %s · target %s%s\n" "$C_DIM" "$IOGRID_VERSION" "$1" "$C_RESET"
    printf "%s================================================================%s\n\n" "$C_DIM" "$C_RESET"
}

# ---------------------------------------------------------------------------
# Host detection
# ---------------------------------------------------------------------------

detect_os() {
    case "$(uname -s)" in
        Darwin)  echo "mac"   ;;
        Linux)   echo "linux" ;;
        MINGW*|MSYS*|CYGWIN*) die "Windows detected. Run install.ps1 instead:\n  iwr -useb https://iogrid.org/install/win | iex" ;;
        *)       die "Unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        arm64|aarch64) echo "arm64" ;;
        x86_64|amd64)  echo "amd64" ;;
        *)             die "Unsupported architecture: $(uname -m)" ;;
    esac
}

detect_linux_distro() {
    if [ -r /etc/os-release ]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        echo "$ID"
        return
    fi
    if command -v lsb_release >/dev/null 2>&1; then
        lsb_release -si | tr '[:upper:]' '[:lower:]'
        return
    fi
    if command -v apt-get >/dev/null 2>&1; then echo "debian"; return; fi
    if command -v dnf >/dev/null 2>&1;     then echo "fedora"; return; fi
    if command -v pacman >/dev/null 2>&1;  then echo "arch";   return; fi
    if command -v apk >/dev/null 2>&1;     then echo "alpine"; return; fi
    echo "unknown"
}

require_cmds() {
    for c in "$@"; do
        command -v "$c" >/dev/null 2>&1 || die "Missing required command: $c"
    done
}

# ---------------------------------------------------------------------------
# Download + verify
# ---------------------------------------------------------------------------

download() {
    # download <url> <out>
    # Returns 0 on success, non-zero on failure. Callers that want
    # to abort on failure should `die` themselves; callers that have
    # a fallback path (e.g. missing .sha256) just check the return
    # code. We deliberately do NOT call `die` here.
    url="$1"; out="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 --retry 3 -o "$out" "$url"
        return $?
    elif command -v wget >/dev/null 2>&1; then
        wget -q --tries=3 -O "$out" "$url"
        return $?
    else
        die "Need curl or wget to download $url"
    fi
}

verify_sha256() {
    # verify_sha256 <file> <expected-hex>
    f="$1"; expected="$2"
    if [ -z "$expected" ]; then
        warn "No checksum supplied for $f — skipping verification (DEV ONLY)"
        return 0
    fi
    if command -v shasum >/dev/null 2>&1; then
        got=$(shasum -a 256 "$f" | awk '{print $1}')
    elif command -v sha256sum >/dev/null 2>&1; then
        got=$(sha256sum "$f" | awk '{print $1}')
    else
        warn "No sha256 tool found, skipping verification (DEV ONLY)"
        return 0
    fi
    if [ "$got" != "$expected" ]; then
        die "Checksum mismatch for $f: got $got, expected $expected"
    fi
    ok "Checksum verified: $f"
}

# ---------------------------------------------------------------------------
# macOS branch
# ---------------------------------------------------------------------------

mac_check_docker() {
    if [ "$IOGRID_NO_DOCKER" = "1" ]; then
        log "Skipping Docker check (IOGRID_NO_DOCKER=1)"
        return 0
    fi
    if [ -d "/Applications/Docker.app" ]; then
        ok "Docker Desktop already installed"
        return 0
    fi
    log "Docker Desktop missing — downloading signed .dmg from docker.com"
    arch=$(detect_arch)
    case "$arch" in
        arm64) dmg_url="https://desktop.docker.com/mac/main/arm64/Docker.dmg" ;;
        amd64) dmg_url="https://desktop.docker.com/mac/main/amd64/Docker.dmg" ;;
    esac
    tmp_dmg=$(mktemp -t iogrid-docker).dmg
    download "$dmg_url" "$tmp_dmg" || die "Failed to download Docker.dmg from $dmg_url"
    log "Mounting Docker.dmg ..."
    mnt=$(hdiutil attach -nobrowse -readonly -mountrandom /tmp "$tmp_dmg" \
        | grep "/Volumes/" | awk '{$1=$2=""; sub(/^[ \t]+/, ""); print}' | head -n1)
    [ -n "$mnt" ] || die "Failed to mount Docker.dmg"
    log "Copying Docker.app to /Applications (you may be prompted for password)"
    if [ -w "/Applications" ]; then
        cp -R "$mnt/Docker.app" "/Applications/"
    else
        sudo cp -R "$mnt/Docker.app" "/Applications/"
    fi
    hdiutil detach -quiet "$mnt" || true
    rm -f "$tmp_dmg"
    log "Launching Docker.app for first-time setup (you'll need to accept the license)"
    open -a /Applications/Docker.app || true
    ok "Docker Desktop installed. iogrid will use it once it finishes initialising."
}

mac_install_daemon() {
    arch=$(detect_arch)
    install_dir="/usr/local/iogrid"
    bin_url="$IOGRID_RELEASE_URL/$IOGRID_VERSION/iogridd-darwin-$arch"
    sum_url="$bin_url.sha256"

    log "Downloading iogridd ($arch) ..."
    tmp_bin=$(mktemp -t iogridd)
    tmp_sum=$(mktemp -t iogridd-sum)
    download "$bin_url" "$tmp_bin" || die "Failed to download $bin_url"

    if download "$sum_url" "$tmp_sum" 2>/dev/null; then
        expected=$(awk '{print $1}' "$tmp_sum")
        verify_sha256 "$tmp_bin" "$expected"
    else
        warn "No .sha256 published — skipping checksum (continuing anyway)"
    fi

    chmod +x "$tmp_bin"
    if [ -w "$(dirname "$install_dir")" ]; then
        mkdir -p "$install_dir"
        mv "$tmp_bin" "$install_dir/iogridd"
    else
        sudo mkdir -p "$install_dir"
        sudo mv "$tmp_bin" "$install_dir/iogridd"
        sudo chmod +x "$install_dir/iogridd"
    fi

    # Symlink for PATH convenience.
    if [ ! -e "/usr/local/bin/iogridd" ]; then
        if [ -w "/usr/local/bin" ]; then
            ln -s "$install_dir/iogridd" "/usr/local/bin/iogridd" || true
        else
            sudo ln -s "$install_dir/iogridd" "/usr/local/bin/iogridd" || true
        fi
    fi

    ok "Installed iogridd to $install_dir/iogridd"
}

mac_register_launchagent() {
    plist_dir="$HOME/Library/LaunchAgents"
    plist_path="$plist_dir/org.iogrid.daemon.plist"
    mkdir -p "$plist_dir"

    log_dir="$HOME/Library/Logs/iogrid"
    cfg_dir="$HOME/Library/Application Support/iogrid"
    mkdir -p "$log_dir" "$cfg_dir"

    cat > "$plist_path" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>org.iogrid.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/iogrid/iogridd</string>
        <string>run</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>IOGRID_CONFIG</key>
        <string>$cfg_dir/config.toml</string>
        <key>RUST_LOG</key>
        <string>info</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
        <key>NetworkState</key>
        <true/>
    </dict>
    <key>ProcessType</key>
    <string>Background</string>
    <key>StandardOutPath</key>
    <string>$log_dir/iogridd.out.log</string>
    <key>StandardErrorPath</key>
    <string>$log_dir/iogridd.err.log</string>
    <key>WorkingDirectory</key>
    <string>$HOME</string>
</dict>
</plist>
EOF
    ok "Wrote LaunchAgent: $plist_path"

    uid=$(id -u)
    # Best-effort: ignore "already loaded" / "service exists" errors. The
    # bootstrap+enable+kickstart sequence is idempotent.
    launchctl bootout "gui/$uid/org.iogrid.daemon" 2>/dev/null || true
    launchctl bootstrap "gui/$uid" "$plist_path" 2>/dev/null \
        || warn "launchctl bootstrap returned non-zero (already loaded?)"
    launchctl enable "gui/$uid/org.iogrid.daemon" 2>/dev/null || true
    launchctl kickstart -k "gui/$uid/org.iogrid.daemon" 2>/dev/null \
        || warn "launchctl kickstart returned non-zero (daemon may already be running)"
    ok "LaunchAgent registered + started"
}

mac_install() {
    banner "macOS / $(detect_arch)"
    require_cmds curl
    mac_check_docker
    mac_install_daemon
    mac_register_launchagent
}

# ---------------------------------------------------------------------------
# Linux branch
# ---------------------------------------------------------------------------

linux_install_docker() {
    if [ "$IOGRID_NO_DOCKER" = "1" ]; then
        log "Skipping Docker check (IOGRID_NO_DOCKER=1)"
        return 0
    fi
    if command -v docker >/dev/null 2>&1; then
        ok "Docker already installed"
        return 0
    fi

    distro="$1"
    log "Installing Docker for $distro via official channel"
    case "$distro" in
        ubuntu|debian)
            require_cmds curl
            $SUDO apt-get update
            $SUDO apt-get install -y ca-certificates curl gnupg
            $SUDO install -m 0755 -d /etc/apt/keyrings
            curl -fsSL "https://download.docker.com/linux/$distro/gpg" \
                | $SUDO gpg --dearmor -o /etc/apt/keyrings/docker.gpg
            $SUDO chmod a+r /etc/apt/keyrings/docker.gpg
            release=$(. /etc/os-release && echo "$VERSION_CODENAME")
            echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$distro $release stable" \
                | $SUDO tee /etc/apt/sources.list.d/docker.list >/dev/null
            $SUDO apt-get update
            $SUDO apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            ;;
        fedora|rhel|centos|rocky|almalinux)
            $SUDO dnf -y install dnf-plugins-core
            $SUDO dnf config-manager --add-repo "https://download.docker.com/linux/fedora/docker-ce.repo"
            $SUDO dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
            $SUDO systemctl enable --now docker
            ;;
        arch|manjaro)
            $SUDO pacman -Sy --noconfirm docker
            $SUDO systemctl enable --now docker
            ;;
        alpine)
            $SUDO apk add --no-cache docker
            $SUDO rc-update add docker boot
            $SUDO rc-service docker start
            ;;
        *)
            warn "Unknown distro '$distro' — please install Docker manually then re-run"
            return 1
            ;;
    esac

    # Add invoking user to docker group (so they can `docker run` without sudo).
    if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
        $SUDO usermod -aG docker "$SUDO_USER" || true
        warn "Added $SUDO_USER to docker group — log out + back in to take effect"
    fi
    ok "Docker installed"
}

linux_install_daemon() {
    arch=$(detect_arch)
    bin_url="$IOGRID_RELEASE_URL/$IOGRID_VERSION/iogridd-linux-$arch"
    sum_url="$bin_url.sha256"
    target="/usr/local/bin/iogridd"

    log "Downloading iogridd ($arch) ..."
    tmp_bin=$(mktemp /tmp/iogridd.XXXXXX)
    tmp_sum=$(mktemp /tmp/iogridd-sum.XXXXXX)
    download "$bin_url" "$tmp_bin" || die "Failed to download $bin_url"

    if download "$sum_url" "$tmp_sum" 2>/dev/null; then
        expected=$(awk '{print $1}' "$tmp_sum")
        verify_sha256 "$tmp_bin" "$expected"
    else
        warn "No .sha256 published — skipping checksum"
    fi

    chmod +x "$tmp_bin"
    $SUDO mv "$tmp_bin" "$target"
    $SUDO chmod +x "$target"
    ok "Installed iogridd to $target"
}

linux_register_systemd() {
    # We register as a USER unit (per docs/TECH.md): daemon should run
    # as a regular user, not root. On a headless server the operator
    # may instead want a system unit; that's documented in
    # installer/README.md.
    target_user="${SUDO_USER:-$USER}"
    if [ "$target_user" = "root" ]; then
        # Genuine root user — fall back to system unit so the daemon
        # actually starts (linger isn't always practical on root).
        warn "Running as root — using system unit instead of user unit"
        linux_register_systemd_system
        return
    fi

    user_home=$(getent passwd "$target_user" | cut -d: -f6)
    unit_dir="$user_home/.config/systemd/user"
    log_dir="$user_home/.local/share/iogrid/logs"
    cfg_dir="$user_home/.config/iogrid"
    $SUDO -u "$target_user" mkdir -p "$unit_dir" "$log_dir" "$cfg_dir"

    unit_path="$unit_dir/iogridd.service"
    $SUDO -u "$target_user" tee "$unit_path" >/dev/null <<EOF
[Unit]
Description=iogrid provider daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/iogridd run
Environment=IOGRID_CONFIG=$cfg_dir/config.toml
Environment=RUST_LOG=info
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=$cfg_dir $log_dir

[Install]
WantedBy=default.target
EOF
    ok "Wrote systemd user unit: $unit_path"

    # Enable lingering so the unit runs without an active login session.
    $SUDO loginctl enable-linger "$target_user" 2>/dev/null || true

    target_uid=$(id -u "$target_user")
    runtime_dir="/run/user/$target_uid"
    # daemon-reload may fail in CI containers where the user's DBus
    # session isn't wired; we tolerate it (the unit file is still on
    # disk so a later boot picks it up).
    XDG_RUNTIME_DIR="$runtime_dir" \
        $SUDO -u "$target_user" systemctl --user daemon-reload 2>/dev/null \
        || warn "systemctl --user daemon-reload failed (no user DBus session?)"
    XDG_RUNTIME_DIR="$runtime_dir" \
        $SUDO -u "$target_user" systemctl --user enable --now iogridd.service 2>/dev/null \
        || warn "systemctl --user enable returned non-zero — check 'systemctl --user status iogridd'"
    ok "systemd user unit enabled + started"
}

linux_register_systemd_system() {
    unit_path="/etc/systemd/system/iogridd.service"
    cfg_dir="/etc/iogrid"
    log_dir="/var/log/iogrid"
    $SUDO mkdir -p "$cfg_dir" "$log_dir"
    $SUDO tee "$unit_path" >/dev/null <<EOF
[Unit]
Description=iogrid provider daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/iogridd run
Environment=IOGRID_CONFIG=$cfg_dir/config.toml
Environment=RUST_LOG=info
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF
    $SUDO systemctl daemon-reload
    $SUDO systemctl enable --now iogridd.service
    ok "systemd system unit registered + started"
}

linux_install() {
    distro=$(detect_linux_distro)
    banner "Linux / $distro / $(detect_arch)"

    # Resolve $SUDO once. If we're already root, the wrapper is empty.
    if [ "$(id -u)" -eq 0 ]; then
        SUDO=""
    else
        require_cmds sudo
        SUDO="sudo"
    fi

    require_cmds curl
    linux_install_docker "$distro"
    linux_install_daemon
    linux_register_systemd
}

# ---------------------------------------------------------------------------
# Pairing handshake
# ---------------------------------------------------------------------------

pair_request() {
    # Ask the daemon for a one-time pairing code. The daemon writes
    # the code to ~/.iogrid/pairing-code.txt and prints it on stdout.
    if [ -n "$IOGRID_PAIR_CODE" ]; then
        echo "$IOGRID_PAIR_CODE"
        return 0
    fi

    # Daemon may take a moment to come up after launchctl/systemctl
    # start. Retry for up to ~10s.
    code=""
    i=0
    while [ "$i" -lt 20 ]; do
        if code=$(iogridd pair --request 2>/dev/null); then
            if [ -n "$code" ]; then
                echo "$code"
                return 0
            fi
        fi
        i=$((i + 1))
        sleep 0.5
    done

    # Fallback: read from the well-known file (the daemon may have
    # written it before we managed to invoke the CLI subcommand).
    pair_file="$HOME/.iogrid/pairing-code.txt"
    if [ -r "$pair_file" ]; then
        cat "$pair_file"
        return 0
    fi

    warn "Could not retrieve pairing code from daemon"
    return 1
}

open_browser() {
    url="$1"
    if [ "$IOGRID_NO_OPEN" = "1" ] || [ "$IOGRID_HEADLESS" = "1" ]; then
        return 1
    fi
    if [ "$(detect_os)" = "mac" ]; then
        open "$url" >/dev/null 2>&1 && return 0
    fi
    if command -v xdg-open >/dev/null 2>&1; then
        xdg-open "$url" >/dev/null 2>&1 && return 0
    fi
    # No DISPLAY → almost certainly headless.
    if [ -z "${DISPLAY:-}" ] && [ -z "${WAYLAND_DISPLAY:-}" ] && [ "$(detect_os)" = "linux" ]; then
        return 1
    fi
    return 1
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    os=$(detect_os)
    case "$os" in
        mac)   mac_install   ;;
        linux) linux_install ;;
    esac

    log "Requesting pairing code from daemon ..."
    if ! code=$(pair_request); then
        warn "Daemon did not produce a pairing code."
        warn "Run 'iogridd pair --request' once the daemon is up, then visit:"
        printf "    %s/onboard\n" "$IOGRID_BASE_URL"
        exit 4
    fi

    onboard_url="$IOGRID_BASE_URL/onboard/$code"
    printf "\n"
    ok "Your one-time pairing code: %s%s%s" "$C_BOLD" "$code" "$C_RESET"
    printf "\n"
    ok "Onboarding URL: %s%s%s" "$C_BOLD" "$onboard_url" "$C_RESET"
    printf "\n"

    if open_browser "$onboard_url"; then
        log "Opened your browser. Complete sign-in there to finish setup."
    else
        log "Open this URL on a device with a browser to finish setup:"
        printf "\n    %s\n\n" "$onboard_url"
    fi
    ok "Install complete. The daemon will keep itself updated automatically."
}

main "$@"
