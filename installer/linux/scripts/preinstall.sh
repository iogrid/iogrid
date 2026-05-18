#!/bin/sh
# nfpm preinstall — runs BEFORE files are laid down on the target system.
# Idempotent: re-runs on upgrade.

set -e

# Create the iogrid system user. We use a system uid (<1000) and no
# login shell; the daemon mostly runs as the human user via the
# user-mode systemd unit, but the system unit (server installs) needs
# its own account.
if ! getent passwd iogrid >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin --user-group iogrid || true
    elif command -v adduser >/dev/null 2>&1; then
        adduser -S -D -H -s /sbin/nologin iogrid || true
    fi
fi

# State directory for keys / cached audit logs.
mkdir -p /var/lib/iogrid /var/log/iogrid
chown iogrid:iogrid /var/lib/iogrid /var/log/iogrid || true
chmod 0750 /var/lib/iogrid /var/log/iogrid || true

exit 0
