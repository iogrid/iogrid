#!/usr/bin/env bash
# End-to-end WG handshake A/B proof for #781.
#
# Stands up the fixed iogrid BoringTun harness (examples/wg_handshake_proof.rs)
# on a veth host IP, runs a REAL `wg` kernel client in an isolated netns
# against it with the limiter PRELOADED past under-load, and checks whether
# the handshake COMPLETES.
#
#   ARM A (NO_RESET=1): legacy pre-#781 pump (own limiter, never reset)
#                       => expected: NO handshake, 0 B received (cookie latch)
#   ARM B (fixed):      reset_count() pumped 1/s => handshake COMPLETES,
#                       inner ICMP echo answered, transfer received > 0
#
# Fully self-cleaning: netns + veth + harness torn down on exit. Spare port
# 51821, throwaway keys only. NEVER touches prod. Refs #781.
set -uo pipefail

PORT=51821
NS=wg781
VHOST=wgt-host
VNS=wgt-ns
HOST_IP=10.200.0.1
NS_IP=10.200.0.2
WG_IF=wg781
DAEMON_INNER=10.99.0.1   # answered by IcmpEchoSink
CLIENT_INNER=10.99.0.2
HARNESS="$1"             # path to the compiled example binary
EVIDENCE="${2:-/tmp/wg781-evidence.txt}"

HPID=""
cleanup() {
  set +e
  [ -n "$HPID" ] && kill "$HPID" 2>/dev/null
  sudo ip netns del "$NS" 2>/dev/null
  sudo ip link del "$VHOST" 2>/dev/null
}
trap cleanup EXIT

run_arm() {
  local label="$1" no_reset_env="$2"
  echo "================ ARM: $label ================"

  # Fresh throwaway keys per arm.
  local DPRIV DPUB CPRIV CPUB
  DPRIV=$(wg genkey)
  DPUB=$(printf '%s' "$DPRIV" | wg pubkey)
  CPRIV=$(wg genkey)
  CPUB=$(printf '%s' "$CPRIV" | wg pubkey)

  # (Re)create the netns + veth.
  sudo ip netns del "$NS" 2>/dev/null
  sudo ip link del "$VHOST" 2>/dev/null
  sudo ip netns add "$NS"
  sudo ip link add "$VHOST" type veth peer name "$VNS"
  sudo ip link set "$VNS" netns "$NS"
  sudo ip addr add "$HOST_IP/24" dev "$VHOST"
  sudo ip link set "$VHOST" up
  sudo ip netns exec "$NS" ip addr add "$NS_IP/24" dev "$VNS"
  sudo ip netns exec "$NS" ip link set "$VNS" up
  sudo ip netns exec "$NS" ip link set lo up

  # Start the harness (binds 0.0.0.0:$PORT, reachable on $HOST_IP).
  # $no_reset_env is either empty (fixed arm) or "NO_RESET=1" (legacy arm).
  sleep 0.3
  env DAEMON_PRIV_B64="$DPRIV" $no_reset_env "$HARNESS" "$CPUB" "$PORT" \
    >"/tmp/wg781-harness-$label.log" 2>&1 &
  HPID=$!
  sleep 1.5
  if ! kill -0 "$HPID" 2>/dev/null; then
    echo "HARNESS FAILED TO START:"; cat "/tmp/wg781-harness-$label.log"; return 1
  fi
  echo "harness pid=$HPID (DPUB=$DPUB)"; sed -n '1,6p' "/tmp/wg781-harness-$label.log"

  # Bring up the real wg KERNEL client inside the netns.
  sudo ip netns exec "$NS" ip link add "$WG_IF" type wireguard
  sudo ip netns exec "$NS" ip addr add "$CLIENT_INNER/24" dev "$WG_IF"
  local CFG; CFG=$(mktemp)
  cat >"$CFG" <<EOF
[Interface]
PrivateKey = $CPRIV

[Peer]
PublicKey = $DPUB
AllowedIPs = $DAEMON_INNER/32
Endpoint = $HOST_IP:$PORT
PersistentKeepalive = 3
EOF
  sudo ip netns exec "$NS" wg setconf "$WG_IF" "$CFG"
  sudo ip netns exec "$NS" ip link set "$WG_IF" up
  rm -f "$CFG"

  # Drive handshakes for ~12s: keepalive + inner ICMP to the daemon's
  # inner IP (IcmpEchoSink answers, giving rx).
  for i in $(seq 1 6); do
    sudo ip netns exec "$NS" ping -c1 -W1 "$DAEMON_INNER" >/dev/null 2>&1
    sleep 2
  done

  echo "---- wg show ($label) ----"
  local OUT
  OUT=$(sudo ip netns exec "$NS" wg show "$WG_IF")
  echo "$OUT"
  {
    echo "================ ARM: $label ================"
    echo "DPUB=$DPUB CPUB=$CPUB endpoint=$HOST_IP:$PORT"
    echo "$OUT"
    echo
  } >>"$EVIDENCE"

  # Verdict: handshake completed iff a 'latest handshake' line is present
  # AND received bytes > 0.
  local HS RX
  HS=$(echo "$OUT" | grep -c 'latest handshake')
  RX=$(echo "$OUT" | grep 'transfer' | grep -oE '[0-9.]+ (B|KiB|MiB) received' | head -1)
  echo "VERDICT[$label]: latest_handshake_lines=$HS  received='$RX'"
  echo "VERDICT[$label]: latest_handshake_lines=$HS  received='$RX'" >>"$EVIDENCE"

  kill "$HPID" 2>/dev/null; HPID=""
  sudo ip netns del "$NS" 2>/dev/null
  sudo ip link del "$VHOST" 2>/dev/null
  echo
}

: >"$EVIDENCE"
echo "#781 WG handshake A/B proof — $(date -u +%FT%TZ)" | tee -a "$EVIDENCE"
echo "boringtun rev 253f7afb2b, real wg kernel client, limiter preloaded under-load" | tee -a "$EVIDENCE"
echo | tee -a "$EVIDENCE"

run_arm "A-legacy-broken" "NO_RESET=1"
run_arm "B-fixed" ""

echo "================ SUMMARY ================" | tee -a "$EVIDENCE"
grep 'VERDICT' "$EVIDENCE"
