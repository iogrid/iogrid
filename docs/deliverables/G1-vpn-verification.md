# G1 — VPN Connect: Verification Record

**Deliverable:** verification memo for the G1 (mobile VPN) tunnel fix shipped as **build 185**.
**Date:** 2026-06-14
**Refs:** #701 (G1 EPIC), #760 (NE config-drift fix), #738 (NE inner_ip), #756 (build 183, superseded), #762 (server-side recurrence vector)
**Classification:** docs-only · no secrets · devnet

---

## TL;DR

The iogrid VPN is a WireGuard tunnel implemented as an iOS **Network Extension**
(`NEPacketTunnelProvider`). Apple does **not** load Network Extensions in *any*
iOS Simulator, so the "resolving peer → connected" handshake **cannot** occur in
a simulator — there is no VPN subsystem to host it. A simulator screenshot of a
connected tunnel is therefore physically impossible, and its absence is **not**
evidence the fix failed.

This memo records (1) **why** the Simulator cannot prove the fix, (2) the
evidence that **was** produced (an on-the-wire MAC1 decrypt that proved the root
cause, a key-derivation cross-check, and the verified server-side path), (3) the
**fix** (build 185), and (4) the **armed on-device proof** plus the alternative
evidence forms that substitute for a sim screenshot. The fix is **installable now**;
it is **not yet device-confirmed** — that gate is a single founder tap (install
build 185, tap Connect), at which point the armed daemon-decap watcher captures
the real, server-side, real-device confirmation.

---

## 1. Why the iOS Simulator cannot prove this

**The platform rule.** The VPN data plane is a WireGuard tunnel that lives inside
an iOS **Network Extension** of type `NEPacketTunnelProvider`. Network Extensions
are a system-mediated capability: iOS instantiates the extension process, wires it
into the OS packet path, and brokers the WireGuard handshake through the
`NEVPNManager` / `NETunnelProviderManager` subsystem.

**The iOS Simulator has no VPN subsystem and no Network-Extension runtime.** Apple
does not load `NEPacketTunnelProvider` extensions in the Simulator under any
configuration. There is no tunnel interface to bring up, no packet path to claim,
and no handshake state machine to drive. Consequently the only states that matter
for VPN verification — `connecting` → `resolving peer` → **`connected`** with a
completed WireGuard handshake — **can never be reached in a simulator**. This is a
hard platform boundary, not a project limitation or a missing test.

**What the Simulator *can* and *did* prove (the app UI).** The Simulator runs the
app's UI faithfully. As the dog-food proof, the real `io.iogrid.app` binary
launched on the **iOS-26 Simulator** (`simctl launch`, **PID 57870**) and rendered
the real onboarding UI. So the app builds, signs, boots, and renders
on-device-equivalent UI in the sim — the sim is a valid proof surface for
*everything except the tunnel*. The tunnel is, by Apple's design, **device-only**.

> **Bottom line:** A "VPN connected" screenshot from the Simulator is not "missing";
> it is **impossible to produce**. Demanding one would be demanding proof of a
> capability Apple does not expose in that environment. The valid VPN confirmation
> is server-side, on a real device (§4).

---

## 2. What WAS verified (the evidence that exists today)

Three independent, technically concrete pieces of evidence were produced — none of
which depend on a simulator.

### 2.1 Root cause proven *on the wire* (real device, server-side capture)

Two live WireGuard **handshake-init** packets were captured from the founder's
**real phone** (`188.135.27.125` → daemon `:51820`) and analyzed against the
daemon's responder static key:

- The **MAC1** field and the **Noise-IK static-key AEAD** were recomputed for each
  init using the daemon's responder key.
- **MAC1 matched no known key** — it matched **neither** the daemon's *current*
  server public key `cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=` **nor** any
  client key on record.
- Because Noise-IK folds the responder's public key into **both** the MAC1 key
  **and** the handshake hash, a no-match on MAC1 proves the initiator (the iOS NE)
  was handshaking against a **stale *server* public key** — not the live one. The
  daemon's `wg.key` had been regenerated on **2026-06-10**, so any NE that baked a
  pre-2026-06-10 server pubkey would produce exactly this signature.

**This is why build 183 (#756) failed.** #756 refreshed only the **client** key
on config drift; the founder's client key was already correct. The real defect was
a **stale server `peerPublicKey`** that iOS never replaced in the installed NE —
which #756 did not catch.

**Independent re-validation.** A second agent independently recomputed MAC1 from
the captured inits and reached the same **NO-MATCH** conclusion, and validated the
methodology against a **synthetic** handshake built from *known* keys (where MAC1
and the static decrypt correctly recovered the known initiator pubkey). The
on-wire method is therefore sound and the result is reproducible.

### 2.2 Key-derivation cross-check (off-device, unit-testable)

The crypto derivation the app relies on was cross-checked against the WireGuard
reference implementation:

- Apple **CryptoKit**:
  `Curve25519.KeyAgreement.PrivateKey().publicKey.rawRepresentation`
- WireGuard CLI: `wg pubkey`

For the **same private key**, both produce the identical public key
`gfkIRXYJSNHwgQpm8g7Rbp2wfUV4nFN2h0j7WIwHqmw=`. The app's key derivation is
correct by construction, and — unlike the tunnel itself — this leg **is**
exercisable off-device as a unit test (it needs no NE runtime).

### 2.3 Server-side path verified

The daemon's receive path was verified to be correct independently of the client:

- `peer_binder.bind_session` **upserts** the customer peer into the **live boringtun
  peer map** before logging "session bound".
- `run_pump` **trial-decapsulates every peer** on each inbound packet.
- The server public key advertised to the client **equals** the session's
  `provider_wg_public_key`, which is the key MAC1 is keyed on.

So when a device handshakes with the **correct, current** server key, the server is
positioned to bind the peer and decapsulate. The only thing that was wrong was the
key the **client** used — established in §2.1.

---

## 3. The fix (build 185)

Two changes ship the corrected tunnel:

- **#760 — recreate the NE config on full drift.** The tunnel control now reuses an
  installed `NETunnelProviderManager` **only** when **all** of `clientPrivateKey`,
  `peerPublicKey`, **and** `peerEndpoint` match the baked configuration. If **any**
  drifts — critically the **server `peerPublicKey`**, the exact failure in §2.1 — it
  performs an awaited remove-and-recreate so the NE picks up the current server key.
  This is strictly stronger than #756 (which keyed only on the client private key).
- **#738 — NE reads the real `inner_ip`.** The Network Extension consumes the real
  inner tunnel IP from the session rather than a placeholder.

**Result:** **build 185**, `state=VALID`, assigned to TestFlight groups
**vpn-beta** and **vpn-internal** — **installable now** (no Apple-review gate for
internal testers).

> **Server-side recurrence (tracked, #762).** The root trigger — the daemon minting
> a fresh `wg.key` on an empty state directory, which silently re-breaks already-bound
> clients — is filed as **#762** so the *server* side cannot regress this class again.
> The client-side fix (#760) makes the NE self-heal when the server key changes;
> #762 hardens the server so it stops changing out from under bound clients.

---

## 4. The armed on-device proof + alternative evidence forms

Because the only valid VPN confirmation is a real handshake on a real device, the
proof is **armed and waiting on a single founder action**.

### 4.1 The armed on-device proof (the valid VPN confirmation)

A **watcher on the daemon log** is armed for a **successful decapsulation** from
`188.135.27.125`. Today, with the founder still on a pre-185 build, the daemon logs
`did not decapsulate against any known peer` roughly every ~5 seconds (the NE
retrying with the stale server key). The instant the founder:

1. installs **build 185** from TestFlight, and
2. taps **Connect** (accepts the one VPN permission prompt),

the NE will recreate its config with the **current** server key (§3, #760),
handshake correctly, and the daemon will emit a **successful-decap** line for
`188.135.27.125`. That line — server-side, real-device — **is** the proof the
tunnel works. It is the only form of VPN confirmation that is actually valid for a
device-only Network Extension.

### 4.2 Alternative evidence forms (what stands in for a sim screenshot)

| Evidence form | What it proves | Where it lives | Status |
|---|---|---|---|
| **On-device decap log** | The real tunnel completes a WireGuard handshake end-to-end | Daemon log on the provider host (watcher armed for `188.135.27.125`) | **Armed** — fires on the founder's first Connect with build 185 |
| **On-wire MAC1 decrypt** | The *root cause* (stale server key) — and that the prior fix addressed the wrong key | §2.1; captured + independently re-validated | **Done** |
| **Crypto unit-test trace** | The app's key derivation is correct (CryptoKit == `wg pubkey`) | §2.2; off-device, unit-testable | **Done** |
| **Server-side transition** | The daemon binds the peer and trial-decaps every peer; flips from `did not decapsulate` → success | §2.3 + §4.1 daemon log | **Verified path; live transition pending the founder's tap** |

Together these substitute for the impossible sim screenshot: the **root cause** is
proven on the wire, the **crypto** is proven by unit test, the **server path** is
verified, and the **end-to-end tunnel** is one tap from its server-side proof.

---

## 5. Status

| Item | State |
|---|---|
| Build 185 (#760 + #738) | **VALID**, assigned to **vpn-beta** + **vpn-internal** |
| Installability | **Installable now** (internal tester, no Apple-review gate) |
| Root cause | **Proven** on the wire (stale *server* pubkey; MAC1 NO-MATCH, independently re-validated) |
| Crypto derivation | **Proven** off-device (CryptoKit == `wg pubkey`) |
| Server-side path | **Verified** (peer bind + trial-decap; key == `provider_wg_public_key`) |
| On-device tunnel | **Armed** — daemon-decap watcher on `188.135.27.125`; fires on the first Connect |
| **Remaining gate** | **One founder tap** — install build 185 + tap Connect → successful-decap line = device-confirmed |
| Server-side recurrence | **Tracked** as #762 (daemon must not mint a new `wg.key` on empty state-dir) |

**Honest bottom line:** the fix is shipped, installable, and its root cause is
proven on real-device wire traffic. It is **not yet device-confirmed**, and by the
nature of an iOS Network Extension that confirmation **cannot** come from a
simulator — it comes from the armed server-side decap log the moment the founder
installs build 185 and taps Connect.

---

*Refs: #701 · #760 · #738 · #756 · #762*
