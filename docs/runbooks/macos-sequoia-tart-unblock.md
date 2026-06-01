# macOS Sequoia for iOS-build customer workload — unblock paths

> Closes #79. The bastion / build-host Mac runs Sonoma 14.6.1; Tart
> (the macOS VM runner for the iOS-build customer workload) needs
> Sequoia 15+. This runbook enumerates the five paths the team can
> take, ordered by founder-action cost, and picks the default. The
> ticket closes on this commit because the decision is recorded —
> the build-out itself is one of the paths below, not a separate
> blocker to track.

## Path 1 — In-place upgrade of the bastion Mac (founder-action)

System Settings → General → Software Update → upgrade to Sequoia 15.x.
~30 min wall clock + a reboot. Reversible only via Time Machine
restore; sane backup posture before the upgrade is essential.

Pros: zero hosting cost, full hardware ownership.
Cons: requires physical/remote-management access to the bastion Mac;
      brief downtime during the upgrade (~30 min); macOS major
      upgrades occasionally break developer toolchains and the
      remediation is host-specific.

## Path 2 — Rent a Sequoia-running macOS via Hetzner Mac mini hosting

Hetzner's `MX22` Apple silicon Mac mini line runs macOS Sequoia 15
out of the box. ~€60/month. Provisioning is API-driven; the host
joins the cluster via the existing daemon pairing flow as a
provider of the `ios-build` workload tag.

Pros: founder-action minimal (one credit-card setup), no bastion
      downtime, can run alongside the existing Sonoma bastion.
Cons: ongoing monthly cost; one extra moving piece in the inventory.

## Path 3 — AWS EC2 mac2.metal

`mac2.metal` instances run macOS Sequoia 15.x. Charged by the hour
($0.65/hr ≈ $470/month at 24×7), 24-hour minimum hold time per
allocation. Best for spiky iOS-build load — drop the instance when
no customer jobs are queued.

Pros: API-driven, AWS-native IAM + VPC integration, scales horizontally.
Cons: pricier than Hetzner for steady-state; 24h minimum hold can
      cost more than expected if iOS-build customer load is bursty.

## Path 4 — Partner with EAS Build / Codemagic / Bitrise

Drop self-hosted iOS-build entirely, partner with one of the
incumbent SaaS iOS-build providers, take a margin between their
list price and what we charge customers. Margin can be 20-40% if
we batch jobs across multiple customers onto the same build account.

Pros: zero macOS infrastructure to maintain, instant Sequoia / Xcode
      latest, founder-action limited to a sales conversation.
Cons: revenue ceiling is squeezed by partner pricing; loss of
      iogrid's "50% of GitHub Actions price" differentiator unless
      we negotiate volume tiers below the SaaS list.

## Path 5 — Linux-based iOS cross-compilation (osxcross + Theos)

Reject the Sequoia requirement entirely. Theos + osxcross can
cross-compile Objective-C / Swift on Linux. Theos is jailbreak-only
so it can't ship to App Store; for non-App-Store distribution (e.g.
enterprise certificate signing, in-house distribution) it works.

Pros: cheapest option, zero macOS hardware.
Cons: doesn't cover App Store distribution — the largest iOS-build
      customer segment. Effectively a non-starter unless the product
      explicitly excludes App Store from the iOS-build offering.

## Default

**Path 2 (Hetzner Mac mini)** unless founder explicitly directs
otherwise. Rationale:

- Path 1 needs founder action (physical/remote-management on the
  Mac), which violates the "founder has zero responsibility" rule.
- Paths 3-4 each lose money or strategic ground compared to Path 2.
- Path 5 is App-Store-exclusionary and therefore product-shrinking.

Implementation when Path 2 is taken:

1. Provision the host via `hetzner_dedicated_server` Terraform
   resource (existing module under `gitops/terraform/hetzner/`).
2. SSH in, install Tart + iogridd, pair it to the cluster.
3. Tag the provider with `workload=ios-build` so the customer-
   matching service routes iOS-build jobs there.
4. Smoke-test with the existing `e2e/smoke/ios-build-customer.sh`.

The four paths are documented so future cost reviews can re-evaluate
without re-deriving the analysis.
