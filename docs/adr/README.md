# Architecture Decision Records (ADRs)

> **WHAT:** Immutable, append-only record of architectural decisions.
> **AUTHORITY:** Historical context. Once accepted, an ADR is never edited — superseded ones get a `**Superseded by:** ADR-NNNN` header.
> **POINTER:** Active decisions live in [`../ARCHITECTURE.md`](../ARCHITECTURE.md) / [`../BUSINESS-STRATEGY.md`](../BUSINESS-STRATEGY.md). In-flight proposals live in [`../proposals/`](../proposals/).

## How to add an ADR

1. Pick the next free number: `ls adr/ | grep -oE '^[0-9]+' | sort -n | tail -1`.
2. Copy the template (or write from scratch — frontmatter convention is `# NNNN - Title` heading).
3. Each ADR ends with `**Status:** Accepted | Rejected | Superseded by ADR-NNNN`.
4. Open a PR; merge after review. Never edit a merged ADR.

## Index

(none yet — early-stage repo, decisions still being captured in `BUSINESS-STRATEGY.md` and `ARCHITECTURE.md`.)
