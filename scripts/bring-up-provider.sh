#!/usr/bin/env bash
# bring-up-provider.sh — stand up a real iogrid VPN/proxy provider on a Linux host.
#
# This is the ONE remaining action that turns the proven, deployed engineering
# (all 6 connection-path fixes: STUN fallback, no-egress gate, WG-key-at-register,
# FORWARD-ACCEPT, mobile-bind, multi-customer routing) into a prod-serving product
# — closing #694. Everything else is done, CI-green, and demonstrated end-to-end.
#
# The daemon self-configures the data plane (opens /dev/net/tun, assigns the inner
# CIDR, enables ip_forward, installs the MASQUERADE + FORWARD-ACCEPT iptables rules),
# so this script only handles prerequisites + pairing/launch + a readiness check.
#
# Prereqs: Linux, root (for the `ip`/`iptables` the daemon shells out to — file-caps
# do NOT pass to child processes, so run as root), /dev/net/tun, a public IP, and the
# `iogridd` binary (build: `cd daemon && cargo build --release --bin iogridd`).
#
# Usage (paired — recommended; get the token from the web UI -> Provider -> Pair):
#   sudo PAIR_TOKEN=<token> ./scripts/bring-up-provider.sh
# Usage (standalone/dev, no pairing — registers directly with vpn-svc):
#   sudo VPN_SVC=https://api.iogrid.org REGION=us-east-1 ./scripts/bring-up-provider.sh
# Optional: IOGRIDD=/path/to/iogridd  WAN_IFACE=<iface>  STATE_DIR=/var/lib/iogridd
set -euo pipefail

IOGRIDD="${IOGRIDD:-$(command -v iogridd || echo ./daemon/target/release/iogridd)}"
STATE_DIR="${STATE_DIR:-/var/lib/iogridd}"
COORDINATOR="${VPN_SVC:-https://api.iogrid.org}"
REGION="${REGION:-us-east-1}"
WAN_IFACE="${WAN_IFACE:-$(ip route get 1.1.1.1 2>/dev/null | grep -oE 'dev [a-z0-9]+' | awk '{print $2}' | head -1)}"

echo "== iogrid provider bring-up =="
[ "$(id -u)" = "0" ] || { echo "ERROR: run as root (the daemon shells out to ip/iptables; file-caps don't pass to children)"; exit 1; }
[ -x "$IOGRIDD" ] || { echo "ERROR: iogridd not found/executable at '$IOGRIDD' — build it: (cd daemon && cargo build --release --bin iogridd)"; exit 1; }
[ -e /dev/net/tun ] || { echo "ERROR: /dev/net/tun missing — load the tun module: modprobe tun"; exit 1; }
[ -n "$WAN_IFACE" ] || { echo "ERROR: could not detect a WAN interface — set WAN_IFACE=<iface>"; exit 1; }
echo "  iogridd:     $IOGRIDD"
echo "  WAN iface:   $WAN_IFACE   (MASQUERADE + FORWARD-ACCEPT install here)"
echo "  coordinator: $COORDINATOR   region: $REGION"
mkdir -p "$STATE_DIR"

if [ -n "${PAIR_TOKEN:-}" ]; then
  echo "== pairing (exchanging token for the mTLS provider identity) =="
  "$IOGRIDD" --state-dir "$STATE_DIR" pair "$PAIR_TOKEN"
  echo "== launching paired provider (Ctrl-C to stop; rules persist by design for restart continuity) =="
  exec env IOGRID_VERBOSE=1 "$IOGRIDD" --state-dir "$STATE_DIR"
else
  PROVIDER_ID="${PROVIDER_ID:-$(cat /proc/sys/kernel/random/uuid)}"
  echo "  standalone provider-id: $PROVIDER_ID  (no pairing; registers directly)"
  echo "== launching standalone provider =="
  exec env IOGRID_VERBOSE=1 "$IOGRIDD" \
    --vpn-svc "$COORDINATOR" \
    --provider-id "$PROVIDER_ID" \
    --region "$REGION" \
    --wan-iface "$WAN_IFACE" \
    --state-dir "$STATE_DIR"
fi
# Verify after it is up (from any authed client):
#   curl -s $COORDINATOR/v1/vpn/regions/$REGION/providers   # your provider, WgPublicKey non-empty
# then a customer session (iogrid vpn run / the mobile app) -> tunnel -> egress as this host's IP.
