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

## 🔴 P1 — Reliability (the #691 outage proved this is urgent, not convenience)

### 1. Decide the node-capacity ceiling (#682)
**Why now:** the 110-pod cap on the single node just caused a **~55-min production outage** — a CoreDNS pod couldn't reschedule at the cap → DNS cascade → API + web down. The cluster **cannot self-heal** at the cap because recovery pods need headroom the cap denies. It currently runs with no DNS redundancy (CoreDNS 1/1) and two products' surfaces parked.
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
