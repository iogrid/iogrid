---
name: Bug
about: A regression or correctness issue in an existing capability
title: 'fix(<scope>): <5-12 word descriptive phrase>'
labels: type/bug
---

<!--
Per dynolabs-io/workflow BACKLOG-STANDARDS:
- Title MUST start with `fix:` or `fix(<scope>):`.
- Body MUST contain the four sections below.
- Add a `severity/*` label: p0 (production broken) / p1 (24h) / p2 (7d) / p3 (whenever).
- Apply ONE `status/*` label when claimed.
-->

## Problem

<1-3 sentences: what's broken. STATE OF THE WORLD, not the fix.
 Include reproduction steps if non-obvious. Link to log lines / commit
 / screenshots showing the bad state.>

## Acceptance criteria

- [ ] <operator-OBSERVABLE outcome that proves the bug is gone>
- [ ] Regression test that would have caught this (or note why infeasible)
- [ ] Screenshot of the fixed state attached as a comment

## Out of scope

- <related symptoms that are tracked separately, with follow-up: #N>

## Repos touched

- iogrid/iogrid (or list multiple if cross-repo)
