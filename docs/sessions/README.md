# Sessions — transient per-session artifacts

> **WHAT:** Date-stamped artifacts from a specific Claude / operator session. Walk runbooks, retrospectives, audit reports.
> **AUTHORITY:** Transient. Not canon. Auto-archive at 30 days.

## Conventions

Filename: `<YYYY-MM-DD>-<topic-slug>.md`. Date = session start date.

## Retention

- ≤ 30 days old: live, may be referenced from open PRs.
- 30–90 days: move to [`../archive/`](../archive/) if the session's TRUST ledger entries are all resolved.
- > 90 days: unconditionally archived.

## Index

(empty — populate as sessions complete)
