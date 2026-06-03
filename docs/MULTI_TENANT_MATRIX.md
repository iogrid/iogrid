# Multi-Tenant Off-Ramp Matrix — iogrid × Ping

> This file is the iogrid-side companion to Ping's canonical contract
> `ping-cash/ping-cash:docs/coordination/iogrid-ping-integration.md`
> (the "multi-tenant matrix" + "bidirectional handshake" sections).
> Ping cross-references this path; keep the two in sync. The `$GRID`
> on-chain identity is governed by [`BUSINESS-STRATEGY.md` §4 (Currency
> model — $GRID + fiat hybrid)](./BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid);
> this file governs the **tenant routing shape** that sits on top of it.
>
> iogrid-side conformance is tracked in issue **#629**.

> ### ⚠️ Transport correction (2026-06-03)
> An earlier draft of this matrix described the Ping handshake over a
> self-invented `ping://` / `iogrid://` **custom-scheme** contract. That
> contract is **SUPERSEDED**. Ping's published spec uses Apple
> **Universal Links** (`https://ping.cash/approve`) plus an on-chain **SPL
> Approve** signature — the same shape the shipped mobile code uses
> (`mobile/ios/src/lib/wallets/ping-pay.ts`, `PING_APPROVE_URL =
> 'https://ping.cash/approve'`). The sections below have been reconciled to
> the Universal-Link transport. The `return_url` on the inbound→tenant leg
> may still be a custom scheme (the tenant app is already foregrounded by
> the OS), but the **launch into Ping is a Universal Link, not a custom
> scheme**. Ping's C-8 signature-verification spec is still pending on
> Ping's side.

## What "multi-tenant" means here

Ping is a **tenant-neutral sovereign signing rail**, not an iogrid-specific
wallet. iogrid is the *first* tenant proving the shape works; every future
tenant (`AcmeMesh`, a generic `$X`) wires into the **same** Ping surface
(`apps/mobile/app/approve.tsx` + `services/wallet` `approve` endpoint)
without Ping shipping per-tenant code. A tenant is onboarded by:

1. Registering the tenant's **billed token** (an SPL / Token-2022 mint).
2. Allowlisting the tenant's **`return_url` scheme** on both sides.
3. Agreeing a versioned **memo schema** so the tenant can parse what the
   user bought from the on-chain memo without ambiguity.

iogrid bills `$GRID`; Ping holds the wallet (Privy MPC). The asymmetry is
the whole point — each tenant owns its service surface, Ping owns the one
shared custody + signing layer. See Ping doc, table *"Why the iogrid VPN
team chose this over 'buy with $GRID directly'"*.

## Tenant matrix

| Tenant             | Billed token | `return_url` scheme | Mint source of truth                                                  | Status                          |
| ------------------ | ------------ | ------------------- | -------------------------------------------------------------------- | ------------------------------- |
| **iogrid** (VPN)   | `$GRID`      | `iogrid://`         | [`SOLANA-ADDRESSES.md`](./SOLANA-ADDRESSES.md) — devnet mint live, mainnet TBD | **first tenant — shipping**     |
| AcmeMesh (compute) | `$ACME`      | `acme://`           | AcmeMesh-owned (allowlist add)                                       | template only — not onboarded   |
| `$X` (future)      | `$X`         | scheme via ADR amd. | tenant-owned                                                         | template only — not onboarded   |

Adding a tenant is **additive**: a new row here + a new scheme on Ping's
`ALLOWED_RETURN_URL_SCHEMES` allowlist + the tenant's own AASA (for
Direction B). No change to the iogrid row is required to onboard others —
which is the property that makes the matrix scale.

## Per-tenant `return_url` scheme table

The `return_url` is where Ping bounces the user **after** the SPL Approve
signature. It is a **custom scheme**, NOT a Universal Link, on the
inbound→tenant leg — the tenant app is already foregrounded by the OS
deep-link, so no silent-handoff AASA is needed for *this* direction. (The
silent-handoff AASA matters for the **outbound** leg into Ping — see
"Bidirectional handshake" below.)

| Tenant   | Approve-return scheme            | Cancel / reject bounce (C-10)                       |
| -------- | -------------------------------- | --------------------------------------------------- |
| iogrid   | `iogrid://vpn/activated?ok=1&signature=<sig>` | `iogrid://vpn/activated?ok=0&reason=cancel` |
| AcmeMesh | `acme://job/funded?ok=1&signature=<sig>`      | `acme://job/funded?ok=0&reason=cancel`      |
| `$X`     | `<scheme>://<path>?ok=1&signature=<sig>`      | `<scheme>://<path>?ok=0&reason=<reason>`    |

**iogrid scheme stability contract** (owed to Ping, per their doc
§"iogrid side owes Ping" item 1): the `iogrid://` scheme is FROZEN. If
iogrid ever changes it, Ping's `approve.tsx` allowlist breaks and every
off-ramp/approve flow fails closed. Any change is a coordinated cutover,
announced to Ping **before** the change ships.

## Memo schema — `iogrid.v1:vpn:<region>:<days>`

The on-chain Approve memo is how iogrid parses what its user bought
**without trusting client-supplied query params**. It is versioned for
forward compatibility (Ping doc coordination item **C-9**).

```
iogrid.v1:vpn:<region>:<days>
└─────┬─┘ └┬┘ └───┬──┘ └─┬─┘
   tenant ver  product  region   days (integer)
   prefix              (lower-kebab)
```

| Field    | Rule                                                                 |
| -------- | -------------------------------------------------------------------- |
| `iogrid` | Tenant prefix. Literal. Other tenants use their own (`acme.v1:…`).   |
| `v1`     | Schema version. Bump on any breaking field change; never reuse.     |
| `vpn`    | Product class. Future iogrid products add classes (`compute`, `gpu`).|
| `region` | Lower-kebab region slug, e.g. `tokyo`, `london`, `sao-paulo`.        |
| `days`   | Positive integer entitlement window in days.                        |

**Examples**

| Memo                          | Means                          |
| ----------------------------- | ------------------------------ |
| `iogrid.v1:vpn:tokyo:30`      | Tokyo VPN, 30 days             |
| `iogrid.v1:vpn:london:7`      | London VPN, 7 days             |
| `iogrid.v1:vpn:sao-paulo:365` | São Paulo VPN, 1 year          |

**Bidi-sanitisation:** Ping's `approve.tsx` strips Unicode bidirectional
control characters from the memo before display (Ping doc, line 104) so a
spoofed `iogrid.v1:vpn:tokyo:30` rendered right-to-left can't masquerade.
iogrid's parser MUST likewise reject any memo containing bidi control
chars or not matching `^iogrid\.v1:vpn:[a-z0-9-]+:[0-9]+$`.

## Bidirectional handshake

Per Ping doc §"Bidirectional handshake — iogrid → Ping → iogrid", the
partnership has TWO directions. Both directions reuse the **same**
scheme/URL allowlist on both sides.

### Direction A — iogrid calls Ping (consume Ping's wallet)

```
[iogrid app]  open Universal Link:
              https://ping.cash/approve?token=GRID&delegate=<vault>
                &amount=250000000&memo=iogrid.v1:vpn:tokyo:30
                &return_url=iogrid%3A%2F%2Fvpn%2Factivated
        ↓ (silent handoff via Ping's AASA at ping.cash/.well-known/…)
[Ping app]    Approve screen → Face ID → Privy MPC signs SPL Approve
        ↓
[Ping app]    bounce iogrid://vpn/activated?ok=1&signature=<sig>
        ↓
[iogrid app]  verify signature on-chain → unlock VPN
```

Direction A needs **Ping's** AASA (shipped, Ping doc ✅). iogrid supplies
the frozen `iogrid://` scheme + the memo schema above.

### Direction B — Ping calls iogrid (consume iogrid's service) — **C-6**

```
[Ping app]    user has $GRID, taps "Use $GRID for VPN" tile
        ↓
[Ping app]    open Universal Link:
              https://iogrid.org/buy-vpn?…&return_url=https://ping.cash/vpn-confirmed
        ↓ (silent handoff via **iogrid's** AASA — this PR)
[iogrid app]  VPN purchase UI → completes purchase
        ↓
[iogrid app]  bounce https://ping.cash/vpn-confirmed?… (receipt confirm)
```

Direction B is the surface where Ping is the **source** of the user, not
the sink. It requires **iogrid to register its own AASA** so iogrid can be
a Universal-Link target without the iOS "Open in 'iogrid'?" dialog. That
AASA lands in this PR at:

- `web/public/.well-known/apple-app-site-association` (static, served as
  `application/json`, no extension), and/or
- `web/src/app/.well-known/apple-app-site-association/route.ts` (Next 15
  route handler that guarantees the correct `Content-Type`).

The AASA declares `applinks` for `iogrid.org` over the Direction-B paths
`/buy-vpn` and `/vpn` (plus the `iogrid://vpn/activated` return target
surface). The Apple **Team ID is a placeholder** (`PLACEHOLDER_TEAMID`)
pending the real iogrid Foundation Apple Developer account — see the file
comments + the "What remains blocked" section below.

## Coordination-item crosswalk (Ping doc → this matrix)

| Ping item | What this doc / PR covers                                                       |
| --------- | ------------------------------------------------------------------------------- |
| **C-6**   | iogrid AASA registered (this PR) → Direction B silent handoff unblocked         |
| **C-9**   | Memo schema `iogrid.v1:vpn:<region>:<days>` formalised above (regex + examples) |
| **C-10**  | Cancel/reject bounce shapes documented in the scheme table (`ok=0&reason=…`)    |
| C-7       | Joint Maestro flow — Ping-owned; this AASA is a prerequisite                     |
| C-8       | Signature-verification spec — still **open on Ping's side** (see below)         |

## What remains blocked

- **Real Apple Team ID** — the AASA ships with `PLACEHOLDER_TEAMID`. The
  real `<TEAMID>.io.iogrid.app` app-ID prefix can only be filled once the
  iogrid Foundation holds an Apple Developer account. Until then iOS will
  associate the domain but reject the (non-existent) app. Blocked on real
  Apple credentials, NOT on code.
- **`$GRID` mainnet mint** — NOT deployed (founder decision; mainnet
  address TBD). A **devnet** mint exists
  (`BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR`, Token-2022, 9 decimals)
  per [`SOLANA-ADDRESSES.md`](./SOLANA-ADDRESSES.md). The `delegate`/`token`
  params in Direction A resolve via env indirection
  (`EXPO_PUBLIC_GRID_TOKEN_MINT`), never hard-coded — so the Universal-Link
  flow targets devnet until mainnet TGE.
- **C-8 signature-verification spec** — Ping has not yet defined the
  canonical path (RPC `getTransaction` poll vs. wallet-service webhook).
  Blocked on Ping.
- **Scheme registration in the iOS app** — `io.iogrid.app` must declare
  `iogrid://` (CFBundleURLSchemes) + the `applinks:iogrid.org` associated
  domain in its entitlements. Owned by the mobile-app agent
  (`mobile/ios/src/**`), out of scope for this doc.
