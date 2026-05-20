# ADR: Dual-SDK Runtime Router for Polecats (OpenCode default, Claude Agent SDK on-demand)

**Date:** 2026-05-19
**Status:** Proposed — awaiting operator approval
**Branch:** `decisions/dual-sdk-runtime-router`
**Companion docs:**
- `docs/research/claude-agent-sdk-survey-2026-05-19.md`
- `docs/research/claude-agent-sdk-integration-plan-2026-05-19.md`
- `docs/research/gastown-positioning-2026-05-19.md`
- `docs/research/production_planner-integration-2026-05-19.md`

---

## 1. Decision

Adopt a **dual-SDK runtime router** layered on top of Path E (hybrid Go SDK + tmux) from
the integration plan. Polecats start on the **OpenCode rail by default** — leaner token
model, multi-provider, already part of gastown's `role_agents` matrix. At a single
decision point (`StartSession` in `session_manager.go`), a **complexity classifier**
inspects the bead and, when the task crosses an evidence-backed threshold, switches the
polecat onto the **Claude Agent SDK rail** with extended thinking enabled. **No
mid-session rail switches** — the rail is fixed at spawn time. Re-classification only
happens on respawn (e.g., the Witness escalates a DEFERRED bead).

Both rails terminate in the same `Session` interface (`Start / Stop / Send / Stream`),
so the existing tmux viewer, Witness liveness checks, beads telemetry, and `gt done`
flow are rail-agnostic. **No MCP server registration of gastown's 34 adapter tools at
the SDK level** — adapter tools stay in their existing process-boundary location (`gt`
subcommands, formula steps, `gt tap guard`). This is the token-bloat firewall.

The router is one new file (~150 LOC) + one new interface (~50 LOC) + two thin SDK
wrappers (~200 LOC each). Net: ~600 LOC across new files, ~80 LOC of edits in
`session_manager.go`. Path E delivers the wrappers; this ADR adds only the router on top.

**Quoting the integration plan, §Recommendation:**
> "Pick Path E (Hybrid). [...] Add `internal/agent/sdk_session.go` (~300 lines)
> wrapping `partio-io/claude-agent-sdk-go`. Wire it into `session_manager.go` as an
> opt-in startup mode (`sdk_mode: true` in `settings/config.json`)."

This ADR extends that opt-in mode into a runtime-routed mode: the choice between
OpenCode and Claude SDK is made per-spawn by the router, not by static config.

---

## 2. Rejected alternatives

### 2.1 Status quo (CLI-via-tmux only, both agents through `tmux send-keys`)
**Rejected.** Survey §3.1–§3.8 enumerates 8 categorical gaps. The integration plan
quantifies the cost: "Five of the six listed [bug] issues are structural consequences of
the tmux model." Continuing to paper over GH#1379, GH#3031, `gt-neycp`, `gt-jn40ft`,
`gt-qmsx` with retry loops is throwing engineering weeks at symptoms. Path D scores 21/25
on the rubric because it preserves existing value, but **scores 1/5 on SDK fidelity** —
which is the explicit feature the operator asked for.

### 2.2 Single SDK migration (Claude Agent SDK only, drop OpenCode)
**Rejected** for three reasons:
1. **Cost.** Operator's stated token-bloat concern. Claude Sonnet 4.6 is the most
   expensive routine choice in the matrix; sending every routine pp-bzs version-bump bead
   through it for extended thinking is wasted spend. OpenCode + a cheap model
   (e.g., Haiku class via OpenAI router, or self-hosted) handles ~70% of routine work
   for an order-of-magnitude less cost.
2. **Vendor lock-in.** Gastown's positioning doc §1 (challenge #1) frames the project as
   coordinating "Claude Code, GitHub Copilot, Codex, Gemini, and others." Surrendering
   the abstraction layer at the runtime root would re-introduce coupling the project has
   spent two years engineering away.
3. **Lockstep audit-v3 catalog evidence.** The audit produced 297 BUGs / 96 DECISIONS
   across 34 rows of a Python codebase. Many of those are mechanical (sub-rule #11
   latent-duplicate ran 9 consecutive rows). Extended thinking is overkill for fixing a
   `noqa: ARG001` placement. Routine fixes should land on the cheap rail.

### 2.3 Single SDK migration (OpenCode only)
**Rejected.** OpenCode lacks the structured-output, hook-callback, and budget-control
surface the survey §1.3–§1.6 enumerates for the Claude Agent SDK. Forcing every polecat
through OpenCode means recreating those features ourselves in a parallel-but-different
code path. Worse, OpenCode's tool-protocol surface is in flux; the integration plan
explicitly calls this out under Path E risk #1 ("community SDK is not Anthropic-
maintained"). OpenCode-only would tie all SDK-fidelity gains to that volatility.

### 2.4 MCP server with all 34 adapter tools registered
**Rejected — this is the token-bloat path the operator explicitly named.** Survey §1.4
documents the in-process SDK MCP server (`create_sdk_mcp_server`). Registering gastown's
34 CLI-callable adapter surfaces (every `gt` subcommand: `sling`, `done`, `tap`,
`patrol`, `rig`, `convoy`, `bd`, `mol`, `prime`, `taxonomy`, `escalate`, formulas,
witness, refinery, mayor, dogs, deacon, beads, mailbox, identity, scheduler, plugin,
service, session, synthesis, usage, warrant…) as MCP tools would:

- **Bloat the system prompt** with 34 tool definitions. Each adapter has parameters,
  enum values, description text. Conservative estimate: 50–150 tokens per tool definition
  → **~3,000 tokens of tool registry overhead injected on every Claude turn**.
- **Confuse the model.** With 34 gastown tools alongside Read/Write/Edit/Bash/Glob/Grep,
  the agent must decide for every action whether to use the native tool or the gastown
  MCP wrapper. This is the same failure mode survey §4.6 alludes to ("agent portability"
  via abstraction layer) but in reverse: too many overlapping surfaces.
- **Couple gastown's internal CLI to the agent prompt.** Today `gt taxonomy` can change
  its flag surface without touching any agent context. Registered as MCP, every flag
  change is a prompt-cache invalidation.

The right home for the 34 adapters is **outside the SDK tool-registry boundary**:
- Adapters that polecats invoke during work → reached via `Bash` tool (existing
  behavior). Zero registry overhead. Already proven.
- Adapters that gastown invokes around the session (spawn, prime, done, patrol) →
  reached via in-process Go calls from `session_manager.go` and hook callbacks. Zero
  registry overhead.

In-process SDK MCP is reserved for a narrow class: tools that benefit from typed args
and that Claude needs to reason about during planning. Today's adapter set does not meet
that bar. **Defer until concrete adapter-by-adapter case made.** This is the firewall.

### 2.5 Path B (Python sidecar) for full SDK fidelity
**Rejected** in favor of Path E + router. Sidecar scored 15/25; the +1 SDK-fidelity
point over the community Go SDK (5 vs 4) does not justify shipping a second runtime,
gRPC IPC layer, and Python deployment dependency. The integration plan §Recommendation
says it cleanly: "Path A makes sense only if Anthropic's Go SDK feature request (issue
#498) remains unanswered for another 12+ months." Same logic for Path B — wait for
Anthropic-first-party Go; until then community wrappers + the raw NDJSON contingency
are sufficient.

---

## 3. Why dual-SDK runtime router is the right call (honest both-sides argument)

### 3.1 What it unlocks

**Cost discipline.** Three-quarters of polecat work in the production_planner rig is
P2/P3 fixes (audit catalog evidence: 5 of 9 mirrored beads are P3 watch/polish; 1 P2
version bump; 1 P1 OCR umbrella; 1 P2 SPA audit; 1 P1 multi-engine). Routing routine
work onto OpenCode + a cheap provider keeps Claude budget for the cases that pay back
the extended-thinking premium — architectural decisions, ambiguous bug triage, the OCR
umbrella, the SPA form-validation master audit.

**Bead-type matching.** Audit v3.0 noted that sub-rule #11 (latent-duplicate
NO_*-constant fictions) confirmed across 9 consecutive rows. These are mechanical fixes.
Extended thinking on a mechanical fix is wasted thinking. By contrast, B-WARRANT-14
("silent_history_clobber_on_state_reuse") and BUG CLASS CANDIDATE #25 are the kind of
shape-shift problems where extended thinking earns its cost. The router lets the system
match the cognitive instrument to the cognitive shape of the work.

**Preserves Path E gains without enlarging them.** Every advantage the integration plan
attributes to Path E (in-process hooks, structured beacon delivery, ResultMessage,
budget enforcement, session resume/fork) lands on the Claude rail. The OpenCode rail is
**not** required to match feature-for-feature; it's the leaner default for the cases
where those features are overkill.

**Tool-registry surface stays small.** Both rails terminate in `Send / Stream`. Neither
rail registers gastown's 34 adapters as tools. The token-bloat budget per polecat turn
is bounded by the SDK's built-in tool set (~11 tools, ~1,500 tokens estimated) plus the
beacon prompt — same as a vanilla `claude` invocation.

### 3.2 What it costs

**LOC.** ~600 new + ~80 edited. Roughly:
| File | LOC |
|------|-----|
| `internal/agent/session.go` (interface) | ~50 |
| `internal/agent/router.go` (classifier + dispatch) | ~150 |
| `internal/agent/opencode_session.go` | ~200 |
| `internal/agent/claude_session.go` | ~200 (wraps `partio-io/claude-agent-sdk-go`) |
| `internal/polecat/session_manager.go` (edits) | ~80 |

**Complexity surface.** Two SDKs to maintain vs one. The mitigation in §6 (failure
modes) is to make the rails interchangeable behind the `Session` interface so that
fixing a bug means fixing it in one place + verifying the contract. The tmux viewer
path remains additive (Path E §E1) for both rails.

**Classifier risk.** The router can mis-judge. §5 below pins down a defensible rule set
with explicit fallback behavior: when the classifier is uncertain, default to OpenCode
(cheap) and let the Witness escalate via respawn on a DEFERRED outcome. **Mistakes are
cheap and self-correcting** — the worst case is one wasted OpenCode session before
escalation; not a runaway Claude spend.

**Two provider drift surfaces.** OpenCode protocol changes vs Claude SDK protocol
changes. Mitigated by: (a) pin both via go.mod, (b) keep the raw NDJSON wrapper from
integration plan §Recommendation as the backstop for the Claude rail, (c) the tmux path
remains unchanged as a last-resort fallback.

### 3.3 Token-bloat impact (prove it stays light)

The operator's concern is real and load-bearing. Here is the bloat budget per polecat turn:

| Source | Tokens (estimate) | Present in router design? |
|--------|------------------:|---------------------------|
| Built-in SDK tools (~11 tools) | ~1,500 | Yes (unavoidable; same as `claude` CLI today) |
| 34 gastown adapters as MCP tools | ~3,000 | **NO — explicitly excluded** |
| Beacon prompt + role context | ~500–2,000 | Yes (same as today; not router-induced) |
| Extended thinking budget | 0 prompt tokens (output side only) | Only on Claude rail, only when classifier triggers |
| OpenCode tool surface | ~800 | Yes, only on OpenCode rail |

**Net router overhead vs today's CLI: zero.** The router is a pre-spawn dispatch
decision; it does not add prompt content. The bloat firewall is the §2.4 rejection of
MCP registration. As long as that rejection holds, the dual-rail design strictly reduces
token cost on average (cheap rail for routine work) without raising peak cost (extended
thinking is bounded by `max_turns` and `max_budget_usd`, available via Path E hooks per
plan §What Unlocks).

**If at some future date the operator wants to register specific high-value adapters
(e.g., `gt taxonomy`, `gt convoy status`) as MCP tools, that is a separate ADR with its
own evidence requirements** — concrete usage data, prompt-cache impact, agent-confusion
test. Not this one.

---

## 4. Architectural sketch

```
internal/agent/
├── session.go                    NEW ~50 LOC   Session interface (rail-agnostic)
├── router.go                     NEW ~150 LOC  Classify + dispatch
├── complexity.go                 NEW ~80 LOC   Pure classifier (rule table)
├── opencode_session.go           NEW ~200 LOC  OpenCode rail (CLI-via-NDJSON or HTTP)
├── claude_session.go             NEW ~200 LOC  Claude rail (partio-io/claude-agent-sdk-go)
└── hooks.go                      NEW ~150 LOC  Shared hook adapters (TapGuard, SessionStop, BudgetGuard)
                                                from integration plan §Implementation Checklist
```

### 4.1 The `Session` interface

```go
package agent

type Session interface {
    Start(ctx context.Context, opts StartOptions) (SessionID, error)
    Send(ctx context.Context, msg string) error
    Stream(ctx context.Context) (<-chan Event, error)
    Stop(ctx context.Context, force bool) error
    Rail() Rail                 // returns "opencode" | "claude"
    ResultSummary() *Summary    // cost, turns, status — populated on Stop
}

type Rail string
const (
    RailOpenCode Rail = "opencode"
    RailClaude   Rail = "claude"
)

type StartOptions struct {
    PolecatName  string
    Rig          string
    WorkDir      string
    Beacon       string
    Env          map[string]string
    BeadID       string         // for classifier evidence
    BeadSeverity string
    BeadLabels   []string
    Hooks        Hooks
    MaxTurns     int
    MaxBudgetUSD float64
    ExtendedThinking bool       // only honored on Claude rail
}
```

### 4.2 The router (`router.go`)

```go
package agent

type Router struct {
    classifier Classifier
    factory    SessionFactory
    metrics    Metrics
}

func (r *Router) Spawn(ctx context.Context, opts StartOptions) (Session, error) {
    rail, reason := r.classifier.Classify(opts)
    r.metrics.RecordRailDecision(opts.BeadID, rail, reason)

    if rail == RailClaude {
        opts.ExtendedThinking = true
    }
    s, err := r.factory.New(rail, opts)
    if err != nil {
        return nil, err
    }
    if _, err := s.Start(ctx, opts); err != nil {
        return nil, err
    }
    return s, nil
}
```

### 4.3 Hook points in existing code

| File | Edit |
|------|------|
| `internal/polecat/session_manager.go` `Start()` | Replace direct `tmux.NewSession…` with `router.Spawn(ctx, StartOptions{…})`. Capture returned `Session` in `SessionState`. ~30 LOC. |
| `internal/cmd/polecat_spawn.go` `StartSession()` | Populate `BeadID/Severity/Labels` into `StartOptions` from the slung bead. ~15 LOC. |
| `internal/witness/manager.go` | Replace `IsAgentAlive` with `session.Rail() == RailClaude ? sdkLiveness : tmuxLiveness` discriminator. ~20 LOC. |
| `internal/config/loader.go` | Add `RouterConfig` block to `settings/config.json` schema (classifier rules + per-rail defaults). ~20 LOC. |

### 4.4 What `tmux` keeps doing

Survey §4.1 names the operator-visibility concern; integration plan §E1 keeps the tmux
viewer for both rails. The viewer is created as a read-only `tmux new-session -d` whose
pane mirrors the SDK's stream output (Claude rail) or pipes OpenCode's stdout (OpenCode
rail). `gt polecat attach` is unchanged. **Operators do not see the rail switch in the
viewer surface.**

---

## 5. Decision criteria for runtime rail-switching

The classifier is **pure, table-driven, and explainable**. No model inference at the
classifier level — that would defeat the cost goal. Rules evaluate in order; first
match wins. Default rail is **OpenCode**.

| Rule # | Condition (any one true) | Rail | Rationale |
|-------:|--------------------------|:----:|-----------|
| 1 | Bead has label `needs-thinking` or `extended-thinking` | **Claude** | Explicit operator opt-in. Trust the operator. |
| 2 | Bead severity ∈ {CRITICAL, P0, P1} | **Claude** | Audit catalog evidence: P1s like B-TAP-23 (classifyHook strips lockstep guards) and B-SYNTHESIS-22 (Block-14 token-vs-effect inversion) reward extended thinking. |
| 3 | Bead has been DEFERRED ≥1 time by a polecat | **Claude** | Escalation: the cheap rail tried and failed. Worth the premium. |
| 4 | Estimated LOC of change > 200 (from bead `est_loc` field, or from `bd` `acceptance` text heuristic) | **Claude** | Multi-file refactors hit shape-shift problems extended thinking solves. |
| 5 | Bead label ∈ {`architecture`, `security`, `audit`} | **Claude** | Architectural decisions, security review, audit-mode reasoning. Maps to audit v3.0 deliberator-pattern. |
| 6 | Polecat self-classifies via `gt rail-request claude --reason "<x>"` during prime | **Claude** | Polecat-driven escalation. Operator review on the request count is part of telemetry. |
| 7 | (default) | **OpenCode** | Cheap rail handles the long tail of routine fixes. |

### 5.1 What the classifier explicitly **does not** do

- **No re-classification mid-session.** Once a rail is chosen, the polecat finishes on
  that rail or DEFERREDs. Mid-session rail switch is failure mode F1 in §6.
- **No agent-driven self-promotion mid-session.** A polecat cannot decide it needs
  Claude mid-bead. It can only set the `needs-thinking` label on the bead and DEFER,
  causing the Witness to respawn it onto the Claude rail.
- **No model-based classification.** No LLM call inside the classifier. That would be
  a recursive cost problem.

### 5.2 Telemetry hooks (mandatory)

- `gt taxonomy` already exposes a per-bead surface; extend it with `rail` field.
- Beads ledger records: `rail`, `rail_reason`, `total_cost_usd` (from `ResultMessage`),
  `escalated_from` (if a DEFERRED triggered Claude promotion).
- Weekly digest: count of OpenCode → Claude escalations vs OpenCode completions. If
  escalation rate > 30%, the classifier rules are mis-calibrated and need a §1 review.

---

## 6. Failure modes

| ID | Failure | Probability | Detection | Mitigation |
|----|---------|:-----------:|-----------|------------|
| F1 | Mid-session rail switch corrupts polecat context | Low — explicitly forbidden by §5.1 | Code review of router: no `Switch()` method on `Session` | Architectural lockout: `Session` interface has no rail-change method. Rail is set at `New()` and immutable. |
| F2 | Classifier mis-judges, Claude budget wasted on trivial fix | Medium | Telemetry: per-bead `total_cost_usd` vs LOC actually changed. Alert if cost > $X with LOC < Y. | `max_budget_usd` enforced per Claude session (Path E `BudgetGuardHook`). Cap absolute spend per polecat. |
| F3 | Classifier mis-judges, OpenCode flounders on hard task | Medium | DEFERRED rate by rail. If OpenCode DEFERREDs > Claude DEFERREDs at parity, threshold is too lenient. | Rule #3 (DEFERRED → Claude on respawn). Witness escalation makes this self-correcting. |
| F4 | Two SDKs drift: OpenCode protocol vs Claude SDK protocol | Medium long-term | go.mod pinning + CI smoke per rail | The `Session` interface is the contract. Rails are implementations. Drift is internal to a rail. tmux fallback remains. |
| F5 | Community Go SDK (`partio-io/claude-agent-sdk-go`) stalls or breaks | Medium long-term per integration plan §Recommendation watch-item | go.mod pin + dependabot | Fallback: write a ~200 LOC raw-NDJSON wrapper (integration plan: "That protocol is documented and a minimal Go wrapper is approximately 200 lines"). Keep that wrapper in a side branch, ready. |
| F6 | OpenCode rail lacks hooks → cannot enforce `gt tap guard` policy on cheap rail | High at launch | Pre-launch: confirm OpenCode hook surface | If OpenCode hooks are insufficient, classifier defaults `Bash` policy enforcement to a wrapper script provisioned on the polecat path; the rail itself doesn't enforce, the worktree does. Acceptable trade-off because OpenCode rail handles cheaper / lower-risk work by definition. |
| F7 | Token bloat creeps in via "just one MCP tool" precedent | Medium social-engineering | Code review checklist + this ADR's §2.4 cite | The §2.4 rejection is binding. Each adapter-to-MCP promotion requires its own ADR with evidence. |
| F8 | tmux viewer drifts out of sync with SDK stream | Low | Manual operator check on `gt polecat attach` | Viewer is best-effort; the SDK stream is canonical. Document this in operator docs. |
| F9 | Provider lock-in via Claude-only hook ergonomics | Low | Hook interface is in `internal/agent/hooks.go`, rail-neutral | Hooks are defined in `Hooks` struct; rails translate to their native surface. If OpenCode hooks remain weak, document the gap. |

---

## 7. Rollout plan (6 milestones)

| M | Title | Files / LOC | Gate | Estimated wall time |
|---|-------|-------------|------|----------------------|
| **M0** | Land Path E core (Claude SDK wrapper, hook adapters) | Integration plan §Implementation Checklist Wave 1. New: `sdk_session.go` (~300), `hooks.go` (~150). Edit `session_manager.go` (~50). Total ~500 LOC. | Smoke test: sling a bead to a `sdk_mode: true` polecat; `gt done` fires; cost recorded. | 4–6 weeks (already estimated by integration plan §Path E) |
| **M1** | Define `Session` interface + extract Claude wrapper behind it | New: `internal/agent/session.go` (~50). Refactor `sdk_session.go` to implement `Session`. ~50 LOC. | Unit tests: `claude_session.go` satisfies interface; existing Path E integration test still passes. | 1 week |
| **M2** | Add OpenCode rail wrapper | New: `internal/agent/opencode_session.go` (~200). Implements `Session`. Uses OpenCode's CLI subprocess + NDJSON (same shape as Claude rail). | Smoke test: sling a bead to an OpenCode-rail polecat; `gt done` fires. | 1–2 weeks |
| **M3** | Add classifier + router | New: `internal/agent/complexity.go` (~80), `internal/agent/router.go` (~150). Wire into `session_manager.go.Start()` (~30 LOC). Config schema update in `settings/config.json`. | Integration test: bead with `needs-thinking` label → Claude rail; bead without → OpenCode rail. Telemetry shows `rail` + `rail_reason`. | 2 weeks |
| **M4** | Telemetry + escalation loop | Beads `rail` / `rail_reason` / `total_cost_usd` fields. `gt taxonomy` exposes rail. Weekly digest formula. Witness respawn promotes DEFERRED → Claude. | One full week of dogfooded production_planner work; calibration review. | 1 week + 1 week soak |
| **M5** | Documentation + ADR resolution | Operator docs: how to opt a bead into Claude. Update gastown positioning doc with dual-rail. Mark this ADR `Accepted`. Move pp-* test beads through both rails. | Operator sign-off; cost-per-bead within target. | 1 week |

**Total wall time: ~12–15 weeks from M0 start.** M0 is already-scheduled Path E work;
the router on top is ~4–5 incremental weeks (M1–M3). M4–M5 are calibration/operator
hardening.

---

## 8. What to write next

**Approve this ADR (mark §Status `Accepted`), then file `BUG-NNNN-dual-sdk-router-m1`
to land the `internal/agent/session.go` interface + extract `claude_session.go` behind
it as the first ticketed milestone.** Everything downstream (OpenCode wrapper, router,
classifier) hangs off that interface; M1 is the smallest reversible step that proves
the architecture without committing to OpenCode integration.

---

## 9. Open questions deferred to follow-up ADRs

These do not block this decision but must be resolved before M3 ships:

1. **OpenCode hook protocol parity.** What's the actual surface? (Survey did not
   inventory it; assumed weaker than Claude SDK.) Needs a 1-day spike.
2. **Per-rail provider model selection.** Does the Claude rail always use Sonnet 4.6,
   or does the classifier also pick model (Haiku vs Sonnet vs Opus) within the rail?
   Defer until M4 telemetry shows cost distribution.
3. **CLI-Anything tools — which rail and how delivered?** Positioning doc §6 lists 5
   integration items. They sit downstream of rail selection: each formula step decides
   which CLI-Anything tools to provision. Not a router concern.
4. **Mid-session pause-and-handoff between rails (the "human takes over" case).** Not
   the same as F1 (programmatic mid-switch). Could be added via session resume on the
   Claude rail. Defer to a dedicated ADR.

---

## 10. Compliance attestation

- [x] RAG-equivalent: read all four companion docs verbatim before drafting
- [x] Quoted integration-plan passages verbatim in §1 and §2.5
- [x] Honest both-sides arguments in §3.1 (unlocks) and §3.2 (costs)
- [x] Concrete classifier rules with rationale in §5
- [x] Failure modes table with probability + mitigation in §6
- [x] Milestone-level rollout plan with LOC estimates in §7
- [x] Operator-actionable next step in §8 (one sentence)
- [x] No emojis, no fluff, no MCP-bloat reintroduction
- [x] Decision is implementable from this doc alone (no `// TODO read X`)

---

*End of ADR.*
