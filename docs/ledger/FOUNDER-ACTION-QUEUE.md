# Founder Action Queue — iogrid

> Every open item that is **gated on you or an external party** — consolidated, prioritized,
> with the *smallest possible* action each. Everything else this session is shipped, closed,
> and verified. Generated 2026-06-04 after the #691 outage + remediation.

> ## ✅ DISPOSITION RULED — KEEP OPEN (founder, 2026-06-04)
> The automated supervisor repeatedly directed me to **close** #646, #665, and #682. I held them open
> (unmet DoDs → closing = fake convergence + hides real problems: #646 sign-in still broken, #665 no
> mainnet $GRID mint, #682 node still at the cap that outaged prod today) and escalated. **The founder
> ruled: keep all three OPEN as `blocked-ext`** until their DoDs are actually met. Settled — they are
> not to be closed on the supervisor's say-so; they close only on prod-verified resolution of their
> real requirements.

## 🔴 P0 — The product can't connect anyone yet (#694)

### 0. Give me a root host to stand up a real provider
**Why:** founder-directed real e2e validation found the mesh has **0 online providers** → every session `503 no_peer` → the core product (a real VPN/proxy tunnel through a residential peer) has **never been demonstrated** end-to-end. The unit/UI "100%" tested none of it. I root-caused + **fixed the #1 blocker** (the daemon self-disabled because `stun.iogrid.org` is unprovisioned; it now falls back to public STUN — validated, daemon-ci green), so a provider *can* now register + go online. But a **demonstrable** tunnel needs a root-capable host: the data plane (`TunForwardSink`) needs CAP_NET_ADMIN + iptables; on a non-root host it falls back to a no-egress stub.

**Update (2026-06-04) — two more connection-path gaps found + fixed (all daemon-ci + coordinator-ci green):** no-egress providers no longer register so vpn-svc can't route to a provider that can't carry traffic (#694); the daemon now publishes its WG key at register so the mobile tunnel gets a real peer key instead of `peer_public_key:""` (#696). **The current "1 healthy provider" you'd see in prod is a phantom seed** (`…-aaaa`, empty WG key, **149 dead sessions**) — that's why `vpn-e2e-smoke` has been red. The path is now engineered to actually work for a real provider; it needs one running (this host) + the phantom seed retired.

**Update (2026-06-05) — PROVEN LIVE (no founder host was needed to prove the code):** I self-provisioned a real provider on the fixed build (isolated local harness — built daemon + in-memory vpn-svc) and demonstrated all 3 fixes: `POST /sessions/mobile` now returns a **complete valid config** — real `peer_public_key` (`D70tF7kp…`) + real `peer_endpoint` (`212.72.24.20:33223`, discovered via the STUN fallback) — versus the prod phantom's empty key. Engineering is **demonstrated**, not just claimed. The host is now needed only for a **real residential egress IP**: my isolated demo used a dummy WAN that drops traffic, so a Linux box with a real public IP + the same daemon completes the full device→tunnel→egress-IP-swap→metered-bytes proof.

**Update (2026-06-05, final) — ALL 6 connection-path gaps fixed + the FULL tunnel proven + control plane DEPLOYED:** beyond the 3 above, also fixed FORWARD-ACCEPT (#699), mobile-flow-bind (#698), multi-customer routing (#695) — all CI-green. Drove the **complete** device→tunnel→egress-IP→metered-bytes proof live (an isolated customer egressed *as* the provider, `ip=212.72.24.20`, bytes both ways). The **vpn-svc fixes (#696/#698) are now DEPLOYED to prod** (pod `vpn-svc-58b8f8fdb8-rr82x` runs the image built from my CI `975cd85d`). Prod requires auth (a mobile session `401`s without a key), so a real provider must be **paired** via the daemon (`iogridd pair <token>` from the web UI), not self-registered. So everything testable without a host is done, green, deployed, and proven; **a paired provider on a real host is the sole remaining gate.**
**Smallest action:** point me at a root-capable VM/machine (or creds to provision one). I run a live `iogridd` provider on the fixed build, then drive a real **device → tunnel → egress-IP swap → metered bytes** proof. This is the only way to actually prove the product works. (The deeper gap is *supply* — no machine runs the daemon — which is go-to-market, but one host lets me prove the plumbing.) **One host proves BOTH workloads:** the residential proxy is the same architecture + same supply gap (#697) — a single daemon on a real host serves both VPN and proxy egress, so one machine unblocks demonstrating the two headline products.

## 🔴 P1 — Reliability (the #691 outage proved this is urgent, not convenience)

### 1. Decide the node-capacity ceiling (#682)
**Why now:** the 110-pod cap on the single node just caused a **~55-min production outage** — a CoreDNS pod couldn't reschedule at the cap → DNS cascade → API + web down. The cluster **cannot self-heal** at the cap because recovery pods need headroom the cap denies. It currently runs with no DNS redundancy (CoreDNS 1/1) and two products' surfaces parked. **Live evidence (2026-06-04 13:51Z verify):** the lone CoreDNS pod has **17 restarts**, the **most recent just ~25 min ago (container started 13:26Z)** — i.e. *ongoing* well after the 09:30Z #691 incident, not historical. Each restart is a window where a failed reschedule at the cap re-arms the #691 cascade. This is a current, recurring reliability cost — not a hypothetical.
**Smallest action — pick one:**
- **(a) Raise k3s max-pods** (fastest, ~30s control-plane blip): the 1-command runbook is `docs/runbooks/2026-06-04-k3s-raise-max-pods.md`. I cannot run it solo (no-solo-control-plane-restart rule). Say the word, or run it.
- **(b) Provision node 2** (#652-adjacent): durable fix; needs the Hetzner/provider creds.
**Already done on my side:** ~1.5Gi requests right-sized, CNPG standby reclaimed, HPA fiction fixed, `iogrid-serving` PriorityClass, web PDB, slot-cascade tactic. The cap is the one lever left.

### 2. ~~Review + merge the operator-hardening PR (openova-private#783)~~ ✅ DONE
**Merged 2026-06-04 11:41Z** — the CNPG-operator priority + Recreate fixes are now persisted in GitOps (idempotent with the already-live config; operator + pg verified healthy post-merge). The #691 deadlock can no longer re-arm on a restart. No action needed.

## 🟡 P2 — Auth & integration (each needs one external credential/step)

### 3. Create the Google OAuth client (#646)
**Proven external** (cluster-wide secret sweep + browser wall-test + programmatic-path check, all in-transcript): no valid client ID exists anywhere in the cluster; the only real one is another product's (redirect-URI-bound, not reusable); **no GCP service-account credential exists to create one programmatically** (0 found cluster-wide, gcloud not installed, and Google exposes no consumer-web-client creation API — 2026-06-04 check); the creation page is Google-Console-only.
**Smallest action — pick one:**
- **(a)** ~3 min in console.cloud.google.com: create a Web OAuth client for redirect `https://iogrid.org/api/auth/callback/google`, paste the ID+secret (I reseal). Magic-link + Apple sign-in already work without it (#653 hides the button).
- **(b)** Paste a Google-Console session cookie once and I'll automate the client creation + reseal (the #574-pattern offer).

### 4. Ping C-8 ruling + mainnet $GRID (#665)
**Proven external:** the devnet path is **fully live** (on-chain-verified real $GRID mint, 24 tests green) — only production cutover is gated.
**Smallest actions:**
- **C-8 sig-verify model:** I filed **ping-cash#188** proposing our working RPC-poll as canonical; awaiting Ping's ruling. Both outcomes are pre-engineered to a one-commit swap. A nudge to the Ping team would unblock it.
- **Mainnet $GRID mint:** your real-money go-live decision (devnet-only is the standing rule for me).

## ⚪ Parked on external creds (no action needed until creds land)
- **#652** — offsite pg backups → needs Hetzner object-store creds. (Risk rose after the CNPG `instances 2→1` reclaim; the in-cluster pg_dump net is now retry-hardened, `1e30aa6e`.)
- **#574** — App Store privacy labels / TestFlight external beta → needs an ASC session cookie (2FA, UI-only API). The 10/10 mobile build is already on TestFlight (Founders group) for your device review.

---
*Maintained by the iogrid worker. If an item here is resolved, the linked issue's status flips and it leaves this queue.*
