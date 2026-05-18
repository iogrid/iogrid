#!/bin/sh
# nfpm postremove — cleanup. Honour purge vs upgrade: we only delete
# config + state on full purge.
set -e

ACTION="$1"  # "purge" | "remove" | "upgrade" (deb), or numeric arg on rpm

case "$ACTION" in
    purge|0)
        rm -rf /var/lib/iogrid /var/log/iogrid /etc/iogrid || true
        if getent passwd iogrid >/dev/null 2>&1; then
            userdel iogrid 2>/dev/null || true
        fi
        ;;
esac

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

exit 0
