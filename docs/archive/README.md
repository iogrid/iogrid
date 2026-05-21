# Archive — frozen / historical

> **WHAT:** Superseded canonical docs, Phase-0 runbooks no longer in active rotation, rejected proposals, frozen one-off session artifacts.
> **AUTHORITY:** None. Read for context only. NEVER edit a file here after it lands.

## Conventions

Filename: `<YYYY-MM-DD>-<original-slug>.md` (the date is when the file was archived, not when it was originally written).

## Current contents

- `2026-05-21-phase0-setup.md` — Phase 0 bastion setup (superseded; mothership lifecycle now lives in operator's runbook collection).
- `2026-05-21-phase0-unblock.md` — Phase 0 unblock procedure (superseded).
- `2026-05-21-phase0-first-customer.md` — Phase 0 first-customer onboarding playbook (superseded).
- `2026-05-21-whitepaper-v1.md` — initial public-facing whitepaper, frozen. Canonical product narrative now lives in [`../BUSINESS-STRATEGY.md`](../BUSINESS-STRATEGY.md).

If you find yourself needing to update an archived file, instead:
1. Lift the relevant content into the appropriate canonical keeper (`../ARCHITECTURE.md` / `../BUSINESS-STRATEGY.md` / `../SECURITY.md` / `../RUNBOOKS.md`).
2. Add a `> Source: previously docs/archive/<file> (merged here on YYYY-MM-DD)` attribution at the merge point.
3. Leave the archived file untouched.
