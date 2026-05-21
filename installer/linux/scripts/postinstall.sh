#!/bin/sh
# nfpm postinstall — runs AFTER files have been laid down.
# Idempotent: re-runs on upgrade. The package only installs a SYSTEM
# unit; install.sh (the curl-pipe path) is what stamps the user-mode
# unit. .deb/.rpm/.apk consumers should run `systemctl --user ...`
# manually for the user-mode unit (see /usr/share/iogrid/systemd/iogridd.service.tmpl).

set -e

# Reload systemd & enable+start the system unit. We don't enable
# user-mode automatically because:
#   - we don't know who the human user is from a postinstall ctx
#   - users on headless servers actively want the system unit, not user

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    # Don't auto-enable; we want operators to opt in via:
    #   sudo systemctl enable --now iogridd
    # ... which install.sh does on their behalf when running the curl
    # pipe. Direct .deb/.rpm installs are usually server / power-user
    # installs and they may want to inspect config before starting.
    echo "[iogrid] installed. Enable + start with: sudo systemctl enable --now iogridd"
fi

# Open-rc (Alpine etc.).
if command -v rc-update >/dev/null 2>&1; then
    if [ -f /etc/init.d/iogridd ]; then
        rc-update add iogridd default || true
    fi
fi

# ----------------------------------------------------------------------
# Phase 2 of EPIC #348 — register the iogrid apt/yum/apk repo so that
# `apt upgrade iogridd` / `dnf upgrade iogridd` / `apk upgrade iogridd`
# rolls the daemon forward on the next unattended-upgrades window.
#
# Gated on:
#   - $IOGRID_NO_AUTO_REPO != "1" — operators who manage updates via
#     Ansible / their own config-mgmt can opt out.
#   - The pubkey is present at /usr/share/iogrid/iogrid.gpg.asc (shipped
#     by nfpm.yaml's `contents:` block).
#
# Idempotent: re-running the postinstall on upgrade is a no-op once the
# source list / repo file / apk repositories entry is in place.
# ----------------------------------------------------------------------
if [ "${IOGRID_NO_AUTO_REPO:-0}" != "1" ]; then
    PUBKEY_SRC="/usr/share/iogrid/iogrid.gpg.asc"
    if [ -f "$PUBKEY_SRC" ]; then
        # Debian/Ubuntu — apt with the modern signed-by keyring layout.
        if command -v apt-get >/dev/null 2>&1 && [ -d /etc/apt ]; then
            install -m 0755 -d /etc/apt/keyrings
            if command -v gpg >/dev/null 2>&1; then
                gpg --dearmor < "$PUBKEY_SRC" > /etc/apt/keyrings/iogrid.gpg
                chmod 0644 /etc/apt/keyrings/iogrid.gpg
            else
                # Fall back to /etc/apt/trusted.gpg.d on ancient apt.
                cp "$PUBKEY_SRC" /etc/apt/trusted.gpg.d/iogrid.gpg.asc
            fi
            ARCH="$(dpkg --print-architecture 2>/dev/null || echo amd64)"
            cat > /etc/apt/sources.list.d/iogrid.list <<EOF
# iogrid Linux package repo (auto-registered by iogrid postinstall).
# Disable with: IOGRID_NO_AUTO_REPO=1 dpkg -i iogrid_<ver>_<arch>.deb
# Repo URL switches from GitHub Releases to releases.iogrid.org once
# https://github.com/iogrid/iogrid/issues/392 lands; clients are
# unaffected because /etc/apt/sources.list.d/iogrid.list is updated by
# the next .deb upgrade.
deb [arch=$ARCH signed-by=/etc/apt/keyrings/iogrid.gpg] https://releases.iogrid.org/deb stable main
EOF
            echo "[iogrid] registered apt repo: /etc/apt/sources.list.d/iogrid.list"
        fi

        # RHEL/Fedora/Rocky/Alma — yum/dnf with repo_gpgcheck=1.
        if command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
            mkdir -p /etc/yum.repos.d
            command -v rpm >/dev/null 2>&1 && rpm --import "$PUBKEY_SRC" || true
            cat > /etc/yum.repos.d/iogrid.repo <<'EOF'
[iogrid]
name=iogrid Linux package repo
baseurl=https://releases.iogrid.org/rpm/$basearch/
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=https://releases.iogrid.org/rpm/iogrid.gpg.asc
EOF
            echo "[iogrid] registered yum/dnf repo: /etc/yum.repos.d/iogrid.repo"
        fi

        # Alpine — apk with /etc/apk/keys + /etc/apk/repositories.
        if command -v apk >/dev/null 2>&1 && [ -d /etc/apk ]; then
            mkdir -p /etc/apk/keys
            # The GPG pubkey is dropped for cross-distro consistency
            # only — apk-tools uses RSA-PSS via abuild-sign, so the
            # actual apk trust anchor is a separate .rsa.pub shipped
            # alongside (see installer/linux/repo/README.md).
            cp "$PUBKEY_SRC" /etc/apk/keys/iogrid.gpg.asc 2>/dev/null || true
            APK_ARCH="$(apk --print-arch 2>/dev/null || echo x86_64)"
            REPO_URL="https://releases.iogrid.org/apk/$APK_ARCH/"
            if ! grep -Fq "$REPO_URL" /etc/apk/repositories 2>/dev/null; then
                echo "$REPO_URL" >> /etc/apk/repositories
                echo "[iogrid] registered apk repo: $REPO_URL"
            fi
        fi
    else
        # Soft-fail — daemon installs cleanly, just no auto-update path.
        echo "[iogrid] note: $PUBKEY_SRC not found — skipping auto-repo registration."
        echo "[iogrid]       see installer/linux/repo/README.md for manual setup."
    fi
fi

exit 0
