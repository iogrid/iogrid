#!/bin/sh
# nfpm preremove — stop service before files vanish.
set -e

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl stop iogridd.service >/dev/null 2>&1 || true
    systemctl disable iogridd.service >/dev/null 2>&1 || true
fi

if command -v rc-service >/dev/null 2>&1; then
    rc-service iogridd stop >/dev/null 2>&1 || true
    rc-update del iogridd default >/dev/null 2>&1 || true
fi

exit 0
