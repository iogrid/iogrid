# Per-incident playbooks

> **WHAT:** One-off, incident-specific playbooks (e.g. "what to do if X regional LB falls over on the 14th of October").
> **AUTHORITY:** Tactical. Generic operator how-tos live in the canonical [`../RUNBOOKS.md`](../RUNBOOKS.md).

## How to add a playbook

Filename convention: `<YYYY-MM-DD>-<incident-slug>.md` (date = first-trigger date, not creation date). Open with a 1-sentence summary of the failure mode + a TL;DR remediation block, then walk through the diagnosis steps in order.

If a playbook here gets re-used across 2+ incidents, **lift it into [`../RUNBOOKS.md`](../RUNBOOKS.md)** and `git rm` the dated file (or move to [`../archive/`](../archive/)).

## Index

(empty — populate as incidents accumulate)
