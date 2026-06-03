# Per-incident playbooks

> **WHAT:** One-off, incident-specific playbooks (e.g. "what to do if X regional LB falls over on the 14th of October").
> **AUTHORITY:** Tactical. Generic operator how-tos live in the canonical [`../RUNBOOKS.md`](../RUNBOOKS.md).

## How to add a playbook

Filename convention: `<YYYY-MM-DD>-<incident-slug>.md` (date = first-trigger date, not creation date). Open with a 1-sentence summary of the failure mode + a TL;DR remediation block, then walk through the diagnosis steps in order.

If a playbook here gets re-used across 2+ incidents, **lift it into [`../RUNBOOKS.md`](../RUNBOOKS.md)** and `git rm` the dated file (or move to [`../archive/`](../archive/)).

## Index

### Operational runbooks (reusable)

| Runbook | Covers |
|---|---|
| [`billing-squads-rollout.md`](./billing-squads-rollout.md) | Migrating billing-svc payouts to a Squads 2-of-3 multisig |
| [`jwt-keypair-rotation.md`](./jwt-keypair-rotation.md) | Rotating the JWT signing keypair without logging users out |
| [`macos-sequoia-tart-unblock.md`](./macos-sequoia-tart-unblock.md) | Unblocking Tart VMs on macOS Sequoia for iOS-build providers |
| [`mobile-ios-testflight-bootstrap.md`](./mobile-ios-testflight-bootstrap.md) | Bootstrapping the mobile iOS app to TestFlight |
| [`vpn/`](./vpn/) | VPN subsystem: architecture, customer onboarding/connect, DERP relay deploy, paired-daemon ops, DoD |

### Dated, one-off playbooks

| Playbook | First trigger |
|---|---|
| [`2026-05-23-ghcr-pull-rotator-walkthrough.md`](./2026-05-23-ghcr-pull-rotator-walkthrough.md) | ghcr pull-secret rotation |
| [`2026-05-24-admin-auth-bootstrap.md`](./2026-05-24-admin-auth-bootstrap.md) | admin.iogrid.org auth bootstrap |
| [`2026-05-24-harbor-mirror-bypass.md`](./2026-05-24-harbor-mirror-bypass.md) | Harbor mirror bypass |
| [`2026-05-24-solana-local-validator.md`](./2026-05-24-solana-local-validator.md) | Local Solana validator for devnet work |

> Deploy reminder: iogrid is **not** Flux-wired. Roll images with [`scripts/reroll-iogrid-deployments.sh`](../../scripts/reroll-iogrid-deployments.sh) (image-only). Do not `apply -k` the prod overlay — the manifests have drifted from live and a full apply crashloops core services and mutates the DB.
