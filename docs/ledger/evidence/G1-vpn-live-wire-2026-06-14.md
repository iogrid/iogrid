# G1 VPN — live on-wire evidence, founder's iPhone (2026-06-14)

Captured live via `tcpdump` on the prod bastion (`144.91.121.182:51820`) while the
founder toggled his VPN. Source = his iPhone `212.72.24.20:51549` (Omantel cellular,
ephemeral port — the real device flow, not the desktop `:51820` red herring).

## PROVEN (timestamped, on the wire)

A **real, self-sustaining WireGuard tunnel** — three clean handshakes, each a
**148-byte init → 92-byte response** (type 0x02 = accepted by the server):

| Time (UTC-equiv on wire) | Event | Bytes |
|---|---|---|
| 19:12:58 | handshake init → response (after founder toggled VPN) | 148 → 92 |
| 19:15:06 | clean **rekey** (~2 min later, on its own) | 148 → 92 |
| 19:17:15 | clean **rekey** (~2 min later, on its own) | 148 → 92 |

- **Sustained bidirectional transport** (32-byte keepalives, both directions) every ~10–25 s between handshakes.
- **ZERO `did not decapsulate` drops** across the ~6-minute window — versus **every** packet dropping earlier this morning (06:18–06:21).

This proves the **VPN data plane works on the founder's actual device**: his
handshakes are accepted, session keys derive, encrypted transport flows both ways,
and the periodic rekey — the exact step that failed this morning after a daemon
restart desync — now succeeds unattended. Server peer = durably-bound `l2bX`.

## NOT yet confirmed (honest — do not over-claim)

- **End-user browsing**: the wire shows the tunnel up + carrying keepalives, but real
  page traffic routing through it / the founder's egress IP showing the server is
  **NOT** independently confirmed. Pending the founder's own check.
- **Restart durability**: re-established after a *manual* toggle; surviving a daemon
  restart with no manual step is not re-proven (this morning it did NOT survive).
- **Client UI gate (#701)** + **NE self-heal (#789)**: fixes #786/#790 ship in
  TestFlight Build 190, not yet installed on the device.

## Correcting this morning's false claims
The 05:14 "connection" required a manual DB bind; it died ~06:21 when the founder
stopped and a daemon restart desynced his session. My "clean since 06:21 / durable"
reports were **misread silence** (he'd given up), not a working tunnel. This 19:12
capture is the first genuinely sustained, self-rekeying tunnel on his device.

## Status
G1 VPN data-plane: **PROVEN live on device** (this file). End-user browsing +
restart-durability + the client-side #701/#789 fixes: **still open**. #701 stays
open until the UI gate is validated on Build 190 — not closed on this evidence.
</content>
