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

exit 0
