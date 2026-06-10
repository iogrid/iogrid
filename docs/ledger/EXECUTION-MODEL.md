# iogrid Execution Model — multi-model orchestration (right horse for the right course)

> Agreed operating directive, 2026-06-10. Shared with the sibling project **openova**
> (persisted there in `docs/sessions/2026-06-10/STATE-OF-PLAY.md §Execution-model`).
> Any iogrid session — regardless of model/effort the operator set — should follow this.

You are the **ORCHESTRATOR** of your session. You hold the live state (IDs, paths,
history, wedges) and you **NEVER** hand that state away. You delegate work to
subagents, choosing the model **PER DISPATCH** — never blind-default to the model
the session happens to be running.

## Per-spawn routing table (set `model:` explicitly on EVERY Agent call)

| Work type | Route to | Rationale |
|---|---|---|
| Judgment, design decisions, novel forensics/debugging | **YOURSELF** (main loop), or a `fable`/strongest-model subagent ONLY if the implementation is trap-laden and you can write the design into the brief | These fail subtly; strength pays for itself |
| Well-specified implementation (design already decided) | `opus` subagent, **worktree-isolated** if it edits files | Half cost, reliable when the brief decides everything |
| Mechanical multi-step (merges, CI babysitting, evidence capture, doc updates) | `opus` subagent | Runbook-driven |
| Bulk reads, greps, inventories, "where is X" | `haiku` or an **`Explore`** agent (breadth `medium`/`very thorough`) | Pennies; locating ≠ auditing |
| Trivial one-liners (a CI re-run, one label flip) | **INLINE** — no agent at all | Dispatch overhead > the work |

## Effort: no per-spawn knob — set it via three levers
1. **MODEL** choice (biggest lever).
2. **PROMPT scoping** — state exhaustiveness explicitly: "precision over speed, failing tests first" vs "locate only, do not audit".
3. **SCOPE WALLS** — list what the agent must NOT touch. This is what prevents burn.

## Mechanism decision rule
- Control flow **KNOWN in advance + PARALLEL + STATELESS** (censuses, audits, review fan-outs, per-item migrations) → a **Workflow** (script-managed agents).
- Control flow **DYNAMIC + STATEFUL** (pipelines where each result decides the next step) → **YOU + routed subagents**. Never fan out what must share live state. *(e.g. the VPN CLI-black-hole live repro is yours, not a workflow.)*
- Genuinely independent multi-day tracks → separate **peer sessions**. Otherwise no.

## Briefing discipline (a subagent is only as good as its brief)
Every dispatch brief must contain: (a) the **DECIDED design** — agents execute, they don't explore; (b) known **traps/anti-patterns** as non-negotiable guards; (c) **repo conventions preloaded**; (d) the **exact verification** to run; (e) a **STRICT structured return format**; (f) **scope walls**.

**iogrid conventions to preload into every implementation brief:**
- Commit messages end with `Co-Authored-By: Claude Opus 4.8 …`; PR bodies end with the Claude Code line.
- Feature-branch commits use `Refs #N`, **never** `Closes #N`. `TRACKER.md` rows go on `main`, never a feature branch (conflict trap).
- **Deploy ONLY via the image-reroll cron** (`scripts/reroll-iogrid-deployments.sh`). NEVER `kubectl apply -k overlays/prod` / Flux-wire — it crashloops the fleet.
- **Solana is devnet-only** (mint `BaQvWwb1…WorR`); mainnet is a founder go-live. Assert devnet, fail closed on a mainnet mint.
- **Providers run on `bastion.openova.io` (`144.91.121.182`) or a Mac — NEVER on the Claude clients machine** (`212.72.24.20`, NAT'd; it runs the agents). See `project_vpn_proven_real_on_bastion`.
- Daemon needs root for `ip`/`iptables` (file-caps don't pass to children). Run feature-gated checks (`cargo check -p … --features routing-real,routing-tun-forward`).

## The anti-theater invariant (non-negotiable, any mechanism)
- **NO step is done without a clickable artifact**: a merged PR, a passing check, a `kubectl` output, a screenshot, a flipped checklist row.
- **Every subagent report is a CLAIM** — re-query live state yourself (`gh` / `kubectl` / `curl`) before propagating it as fact.
- **"Wire-proof" ≠ acceptance.** Only the end-user-visible outcome counts.
- If a background step can fail silently (502 mid-roll, watch killed), **gate the next step on re-verified state**, not on the step's own echo.

## Economics
Prefer **1–3 surgical dispatches** over wide fan-outs. Fan out ONLY for stateless breadth. **Escalate model strength on EVIDENCE** of difficulty (a failed attempt, a known trap), not on vibes — and **downgrade back** when the work turns mechanical.
