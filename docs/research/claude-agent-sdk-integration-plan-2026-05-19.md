# Claude Agent SDK Integration Plan

**Date:** 2026-05-19
**Author:** Research agent (SDK integration swarm)
**Branch:** research/claude-agent-sdk-integration
**Companion doc:** `claude-agent-sdk-survey-2026-05-19.md`

---

## Executive Summary

This document scores five concrete paths for integrating the Claude Agent SDK with
gastown's polecat-spawn architecture. The goal is "full integration of Claude Agent SDK
protocols" — structured tool dispatch, in-process hook callbacks, programmatic session
control — without necessarily abandoning the operator-visibility benefits of the current
tmux model.

**Recommendation (bottom line up front):** Path E (Hybrid). Keep the tmux session for
operator visibility and session persistence; add a community Go SDK wrapper for
structured tool dispatch and hook callbacks. Deliver the highest-value SDK features
(in-process hooks, structured output, budget controls) in 60 days without rewriting
the tmux layer.

---

## Scoring Criteria

Each path is scored on five axes (1–5, higher is better):

| Axis | Meaning |
|------|---------|
| **SDK fidelity** | How much of the Agent SDK feature set becomes available |
| **Operator visibility** | Preserves `tmux attach` and live intervention |
| **Session persistence** | Polecat survives `gt` process restart |
| **Agent portability** | Works with Codex, Gemini, OpenCode |
| **Delivery speed** | Time to shippable state (60-day horizon) |

---

## Path A: Go HTTP Client to Anthropic API

### What It Is

Write a minimal Go HTTP client in `internal/agent/anthropic.go` that calls
`POST https://api.anthropic.com/v1/messages` directly. Implement the tool loop
manually in Go: parse `stop_reason == "tool_use"`, execute tools, inject results,
repeat.

### File-Level Impact

| File | Change |
|------|--------|
| `internal/agent/anthropic.go` | **NEW** — HTTP client, message loop, tool dispatch |
| `internal/agent/tools.go` | **NEW** — built-in tool implementations (Read, Write, Bash, …) |
| `internal/polecat/session_manager.go` | Replace `NewSessionWithCommandAndEnv` for HTTP-backed polecats |
| `internal/config/loader.go` | New `BuildAgentClientConfig` path |
| `go.mod` | No new deps (stdlib `net/http` only) |

### Estimated Effort

- **12–16 weeks** for production-quality implementation.
- The tool loop is non-trivial: retry logic, stream parsing, context management,
  `--dangerously-skip-permissions` equivalent, permission dialog handling.
- The existing Claude Code binary implements approximately 8,000 lines of TypeScript
  for this loop. Reimplementing it in Go from scratch is a significant undertaking.

### Top 3 Risks

1. **Re-implementing the agent loop** — hundreds of edge cases in tool execution
   (file encoding, bash PTY handling, streaming partial JSON) that the CLI already
   handles. Any deviation produces different behavior than the CLI polecat model.
2. **No hook callbacks** — the Anthropic Messages API has no hook protocol. Hooks
   (PreToolUse, PostToolUse) are a Claude Code / Agent SDK abstraction on top of
   the raw API. This path gives up all hook infrastructure.
3. **API schema drift** — the Messages API surface (tool_result format, content block
   types, streaming event types) changes with model versions. The SDK insulates callers
   from this; a raw HTTP client does not.

### What Unlocks

- Pure Go, no Python or Node.js dependency at runtime.
- Direct cost control (`max_budget_usd` can be implemented trivially).
- Full Go-native observability (otel spans, structured logs).

### Score

| Axis | Score | Notes |
|------|-------|-------|
| SDK fidelity | 2/5 | No hooks, no session resume, no MCP |
| Operator visibility | 1/5 | No tmux pane |
| Session persistence | 2/5 | Must implement checkpoint/resume |
| Agent portability | 2/5 | Anthropic API only; other agents need separate path |
| Delivery speed | 2/5 | Longest build time |
| **Total** | **9/25** | |

---

## Path B: Python Bridge Service

### What It Is

Spawn a Python sidecar process that imports `claude-agent-sdk`. Go talks to it via
gRPC or HTTP. The sidecar runs the agent loop using the full Python SDK; Go calls into
it for session control, hook registration, and result streaming.

```
gt binary (Go)
    │  gRPC / HTTP
    ▼
python sidecar
    │  claude-agent-sdk (subprocess → claude CLI)
    ▼
claude binary (Node.js)
```

### File-Level Impact

| File | Change |
|------|--------|
| `internal/bridge/client.go` | **NEW** — gRPC/HTTP client to Python sidecar |
| `internal/bridge/proto/agent.proto` | **NEW** — session, hook, stream RPC definitions |
| `sidecar/agent_bridge.py` | **NEW** — Python gRPC server wrapping claude-agent-sdk |
| `sidecar/requirements.txt` | **NEW** — claude-agent-sdk, grpcio |
| `internal/polecat/session_manager.go` | Add bridge-backed session path |
| `scripts/sidecar-start.sh` | **NEW** — sidecar lifecycle management |
| Dockerfile (or equivalent) | Add Python 3.10+ runtime to deployment image |

### Estimated Effort

- **6–8 weeks** for a working integration.
- 2–3 additional weeks for deployment hardening (sidecar lifecycle, crash recovery,
  health checks).

### Top 3 Risks

1. **Extra process, extra failure mode** — the sidecar is a new crash domain. If it
   dies, active polecat sessions die too. Health monitoring must be added to the
   Witness patrol.
2. **IPC latency** — every hook callback crosses gRPC, adding ~1–5 ms. For high-frequency
   hooks (PreToolUse on every Bash call) this compounds. Acceptable for most cases; bad
   for interactive latency-sensitive workflows.
3. **Python dependency in the deployment stack** — gastown is a pure Go binary today.
   Adding Python 3.10+ to deployment requirements is a non-trivial operational change
   (Docker base image, version pinning, venv management).

### What Unlocks

- Full Python SDK feature set: all hooks, MCP, subagents, session resume, structured
  output, budget controls.
- In-process hook callbacks in Python (bridged to Go via gRPC callbacks).
- The sidecar can be tested independently of gastown's Go binary.

### Score

| Axis | Score | Notes |
|------|-------|-------|
| SDK fidelity | 5/5 | Full feature set available |
| Operator visibility | 2/5 | No tmux pane; dashboard output only |
| Session persistence | 3/5 | Session survives gt restart if sidecar managed separately |
| Agent portability | 2/5 | Python SDK is Claude-only |
| Delivery speed | 3/5 | Medium effort; good SDK feature payoff |
| **Total** | **15/25** | |

---

## Path C: Embed Python Interpreter

### What It Is

Use a Go-to-Python binding (`go-python3`, `cgo` + CPython, or `extism` + WASM) to
call `claude-agent-sdk` directly from within the `gt` binary, in-process.

```
gt binary (Go)
    │  cgo / go-python3
    ▼
CPython 3.10+ (in-process)
    │  claude-agent-sdk
    ▼
claude CLI subprocess
```

### File-Level Impact

| File | Change |
|------|--------|
| `internal/agent/python_embed.go` | **NEW** — go-python3 bindings |
| `internal/agent/sdk_adapter.go` | **NEW** — Python to Go type translation |
| `go.mod` | Add `go-python3` or similar binding |
| `Makefile` | Add `PKG_CONFIG_PATH` for libpython |
| CI pipeline | Add CPython 3.10+ build dependency |

### Estimated Effort

- **10–14 weeks** to reach production quality.
- ABI compatibility between Go's CGO and CPython is fragile; the GIL affects goroutine
  scheduling; any panic in Python can crash the Go process.

### Top 3 Risks

1. **Build complexity and ABI risk** — CPython's C API changes between minor versions.
   go-python3 does not fully support Python 3.12+. CGO disables cross-compilation and
   makes the binary non-portable.
2. **GIL + goroutines** — Python's GIL blocks all goroutines on the OS thread that
   holds it. Long-running agent loops (minutes to hours) will monopolize a thread and
   degrade gt's responsiveness.
3. **Crash blast radius** — a panic or segfault in CPython kills the entire `gt`
   process, taking all active polecat session state with it (though the tmux panes
   themselves survive as separate processes).

### What Unlocks

- Tightest possible coupling: Go can call Python functions synchronously.
- No extra process, no gRPC, no port management.
- Full Python SDK feature set.

### Score

| Axis | Score | Notes |
|------|-------|-------|
| SDK fidelity | 5/5 | Full feature set |
| Operator visibility | 2/5 | No tmux pane for SDK sessions |
| Session persistence | 2/5 | Tied to gt process; GIL risk |
| Agent portability | 2/5 | Claude-only |
| Delivery speed | 1/5 | Highest complexity, longest timeline |
| **Total** | **12/25** | |

---

## Path D: Keep CLI but Harden

### What It Is

Do not change the architecture. Fix known bugs in the CLI-via-tmux path:
- `gt-jn40ft`: stale session not killed on respawn
- `gt-neycp`: race condition — env vars set after pane starts
- `gt-qmsx`: GT_PANE_ID recording for liveness
- `gt-2ra`: Dolt DB not initialized hang
- GH#1379 / GH#3031: startup nudge delivery race

Audit and improve the tap guard hook dispatch latency. Add structured output reading
from JSONL. Keep tmux, keep CLI.

### File-Level Impact

| File | Change |
|------|--------|
| `internal/polecat/session_manager.go` | Targeted bug fixes |
| `internal/cmd/tap.go` | Hook dispatch improvements |
| `internal/tmux/tmux.go` | `IsIdle` / `IsAtPrompt` precision improvements |
| No new files required | |

### Estimated Effort

- **3–6 weeks** to close the known bugs and harden the existing code.
- Ongoing: every new SDK feature that gastown wants requires new CLI-parsing heuristics.

### Top 3 Risks

1. **Perpetual lag on SDK features** — every new hook type, new output message format,
   or new session capability requires gastown to reverse-engineer it from the CLI.
   Structured output (schema-constrained JSON result) is not accessible without parsing
   JSONL after the fact.
2. **Paste-based delivery remains structurally unreliable** — the nudge-retry loop
   (`verifyStartupNudgeDelivery`) is a consequence of the communication channel.
   Hardening reduces failure rate but does not eliminate it.
3. **No budget controls** — `max_budget_usd` and `max_turns` are CLI flags but they are
   not enforced by gastown per-polecat today. Adding enforcement requires parsing exit
   codes from the CLI in ways the existing code does not support.

### What Unlocks

- Ships fastest.
- Preserves all existing operator tooling (attach, inject, capture).
- Closes known stability bugs.
- Keeps agent portability (Codex, Gemini, etc.).

### Score

| Axis | Score | Notes |
|------|-------|-------|
| SDK fidelity | 1/5 | CLI heuristics only; no native hooks |
| Operator visibility | 5/5 | Full tmux attach |
| Session persistence | 5/5 | Sessions survive gt restart |
| Agent portability | 5/5 | Any agent CLI works |
| Delivery speed | 5/5 | Fastest to ship |
| **Total** | **21/25** | |

---

## Path E: Hybrid (Recommended)

### What It Is

Keep the tmux session for operator visibility, polecat identity, and session
persistence. Add a thin Go SDK wrapper for structured message control.

The two integration points are:

**E1 — Community Go SDK for new polecats (optional, additive):**
Add `partio-io/claude-agent-sdk-go` (or `schlunsen/claude-agent-sdk-go`) as a
dependency. For new polecat sessions, the Go SDK spawns `claude --print
--input-format stream-json --output-format stream-json` as a subprocess and
communicates via NDJSON — the same binary, without tmux wrapping. The tmux session
becomes optional (created only when `GT_ATTACH_TMUX=1` is set, or always created as
a read-only viewer).

**E2 — Managed Agents REST API for specific workloads (opt-in, independent of E1):**
Add a minimal Go HTTP client that calls `POST /v1/sessions` (Managed Agents API) for
workloads where local filesystem access is not required. This is purely additive and
does not touch the polecat spawn path.

The startup path for an E1 polecat:

```
gt sling <rig> <bead>
    │
    └─ SpawnPolecatForSling() [unchanged]
        └─ StartSession() — new branch: sdkMode == true
            │
            ├─ claude.NewSession(
            │     claude.WithModel(rc.ResolvedAgent),
            │     claude.WithCwd(workDir),
            │     claude.WithEnv("GT_ROLE", "polecat"), ...
            │     claude.WithHook(claude.HookPreToolUse, gastown.TapGuardHook),
            │     claude.WithHook(claude.HookStop, gastown.SessionStopHook),
            │  )
            │
            ├─ session.Send(ctx, beacon)    — structured delivery, no paste
            │
            └─ go streamLoop(ctx, session)  — goroutine: process ResultMessage,
                                              update polecat state on stop
```

The tmux pane can still be created alongside for `gt polecat attach` in viewer mode:
```
tmux new-session -d -s <sessionID> \
    "claude --print --input-format stream-json --output-format stream-json"
```
The Go SDK owns the stdio pipe; tmux is wired as a viewer (no send-keys needed).

### File-Level Impact

| File | Change |
|------|--------|
| `internal/agent/sdk_session.go` | **NEW** (~200 lines) — `SDKSession` wrapping community Go SDK |
| `internal/agent/hooks.go` | **NEW** (~150 lines) — gastown hook implementations |
| `internal/polecat/session_manager.go` | Add `StartSDKSession()` branch (~50 lines) |
| `internal/config/loader.go` | Add `SDKMode` to `BuildStartupCommandFromConfig` branching |
| `internal/config/types.go` | `SDKMode bool` on `SessionConfig` |
| `go.mod` | Add community Go SDK dependency |
| `internal/cmd/polecat_spawn.go` | Route to `StartSDKSession` when `SDKMode` is set |
| `internal/witness/manager.go` | SDK liveness check via goroutine health, not pgrep |
| No files deleted | tmux path preserved; SDK path is additive |

Total new code: approximately 400–600 lines across new files. Existing files get
20–50 line additions each.

### Estimated Effort

- **4–6 weeks** for E1 (community Go SDK integration).
- **2–3 additional weeks** for E2 (Managed Agents REST client, if needed).
- E1 is self-contained; E2 is independently opt-in.

### Top 3 Risks

1. **Community Go SDK is not Anthropic-maintained** — `partio-io/claude-agent-sdk-go`
   is a community library. If it falls behind the Claude Code CLI protocol version
   (NDJSON schema changes, new stream event types), gastown must either pin the CLI
   version or fork the wrapper. Risk mitigated by monitoring upstream SDK releases and
   by the NDJSON protocol's backward compatibility record (stable since SDK launch).
2. **Two startup code paths diverge over time** — maintaining both a tmux path and an
   SDK path means bugs fixed in one must be ported to the other. Mitigation: make the
   SDK path the default for new rigs within 90 days, then deprecate the tmux-only path.
3. **Hook semantics differ at the margin** — gastown's current `gt tap guard` has
   specific exit-code semantics (exit 2 = deny, exit 0 = allow). The Go SDK hook
   protocol uses `HookOutput{Decision: "deny"}`. The translation is straightforward
   but must be tested for edge cases (deny with reason, partial input modification).

### What Unlocks

Within 60 days:
- In-process hook callbacks (PreToolUse, PostToolUse) — no subprocess fork per event.
- Structured beacon delivery — no paste-based nudge retry loop.
- `ResultMessage` on session end — structured termination handling.
- `max_turns` and `max_budget_usd` enforced at the Go SDK level.
- Session resume and fork via `WithResume` / `WithForkSession`.
- Structured output via `WithOutputFormat` (optional schema).

Within 90 days (if tmux path deprecated for new rigs):
- `verifyStartupNudgeDelivery` retry loop deleted.
- `tmux.IsIdle` / `IsAtPrompt` heuristics deleted.
- Process-based zombie detection (`IsAgentAlive` via pgrep) replaced by goroutine
  health check.
- `ResultMessage.subtype` replaces the Witness patrol's stall detection heuristic.

### Score

| Axis | Score | Notes |
|------|-------|-------|
| SDK fidelity | 4/5 | All core SDK features; MCP via community SDK |
| Operator visibility | 4/5 | tmux viewer still available; SDK owns stdio |
| Session persistence | 4/5 | SDK session resumable via session JSONL |
| Agent portability | 4/5 | tmux path keeps non-Claude agents; SDK path is Claude-only |
| Delivery speed | 4/5 | 4–6 weeks to E1 |
| **Total** | **20/25** | |

---

## Comparative Scorecard

| | A (HTTP) | B (Sidecar) | C (Embed) | D (Harden) | E (Hybrid) |
|-|----------|-------------|-----------|------------|------------|
| SDK fidelity | 2 | 5 | 5 | 1 | 4 |
| Operator visibility | 1 | 2 | 2 | 5 | 4 |
| Session persistence | 2 | 3 | 2 | 5 | 4 |
| Agent portability | 2 | 2 | 2 | 5 | 4 |
| Delivery speed | 2 | 3 | 1 | 5 | 4 |
| **Total** | **9** | **15** | **12** | **21** | **20** |

Path D scores highest on the raw rubric because it fully preserves existing value. But
it scores 1/5 on SDK fidelity — the feature the operator requested. Path E scores
second overall and highest among the paths that actually deliver SDK features.

---

## Recommendation

**Pick Path E (Hybrid).** Here is the 60-day vs. 1-year ROI argument:

**60-day:** Add `internal/agent/sdk_session.go` (~300 lines) wrapping
`partio-io/claude-agent-sdk-go`. Wire it into `session_manager.go` as an opt-in
startup mode (`sdk_mode: true` in `settings/config.json`). Gate it behind a feature
flag. Ship in-process hook callbacks and structured beacon delivery. The tmux path is
unchanged — zero risk to existing operator workflows. The Witness patrol can switch
polecats to SDK mode one rig at a time.

**1-year:** With SDK mode as the default for new rigs, systematically delete:
- `verifyStartupNudgeDelivery` and its retry logic
- `tmux.IsIdle` / `IsAtPrompt` heuristics used for nudge verification
- The process-based zombie detection path (`IsAgentAlive` via pgrep)
- The `gt tap guard` subprocess round-trip for every hook event

The result is a codebase where polecat session management is approximately 40% smaller,
the Witness patrol is simpler (it reads `ResultMessage.subtype` instead of scraping
pane content), and hook callbacks are in-process Go functions rather than spawned
children.

**What to defer:** Path B (Python sidecar) and Path A (raw HTTP client) are both valid
long-term plays but deliver less value per engineering week than Path E. Path A makes
sense only if Anthropic's Go SDK feature request (issue #498) remains unanswered for
another 12+ months AND gastown needs capabilities beyond what the community Go wrappers
provide.

**The one thing to watch:** `partio-io/claude-agent-sdk-go` is a community library. If
it stalls, the backstop is the raw NDJSON protocol (`claude --print --input-format
stream-json --output-format stream-json`). That protocol is documented and a minimal
Go wrapper is approximately 200 lines. Do not build on a single community library
without this contingency established.

---

## Implementation Checklist (Path E, Wave 1)

```
[ ] Add go.mod dependency: github.com/partio-io/claude-agent-sdk-go
[ ] Write internal/agent/sdk_session.go
    - SDKSession struct wrapping claude.Session
    - Start(ctx, polecatName, opts) — translates SessionStartOptions to SDK options
    - Stop(force) — calls session.Close()
    - Send(ctx, message) → session.Send()
    - Stream(ctx) → session.Stream() with fan-out to polecat state machine
[ ] Write internal/agent/hooks.go
    - TapGuardHookAdapter — wraps gt tap guard logic as claude.HookPreToolUse callback
    - SessionStopHookAdapter — fires beads state update on ResultMessage
    - BudgetGuardHook — enforces per-polecat max_budget_usd
[ ] Extend internal/config/types.go: SDKMode bool on SessionConfig
[ ] Extend internal/polecat/session_manager.go: StartSDKSession() branch (~50 lines)
[ ] Add sdk_mode flag to internal/cmd/polecat.go (polecat start --sdk)
[ ] Update internal/witness/manager.go: SDK liveness via goroutine status (not pgrep)
[ ] Integration test: sling a bead to an SDK-mode polecat, verify gt done fires
[ ] Manual test: gt polecat attach on SDK-mode polecat (tmux viewer mode)
```

---

## Path E: Unlocked Capabilities vs. Gastown Bug Inventory

The following known gastown issues are directly mitigated by Path E's SDK delivery:

| Issue | Root Cause | SDK Mitigation |
|-------|------------|----------------|
| GH#1379 (startup nudge race) | `tmux send-keys` arrives before Claude ready | SDK `Send()` is synchronous; no race |
| GH#3031 (false-positive nudge retry) | `IsAtPrompt` vs `IsIdle` confusion | SDK stream has no ambiguous prompt state |
| `gt-neycp` (env var race) | `-e` flags race with pane start | SDK `WithEnv()` is pre-session config |
| `gt-jn40ft` (stale session blocks respawn) | tmux session not killed on zombie | SDK: no tmux session to leak |
| `gt-qmsx` (GT_PANE_ID declaration) | Process-tree inference unreliable | SDK: no pane; liveness is goroutine health |
| `gt-2ra` (Dolt hang on uninit) | Pre-spawn check times out | Unrelated to SDK; fix in Path D also |

Five of the six listed issues are structural consequences of the tmux model. Path E
eliminates them without requiring individual bug fixes.

---

## Path Missed in the Prompt: Managed Agents as Polecat Backend (Path F)

One path the prompt did not enumerate: using the Managed Agents REST API
(`POST /v1/sessions`, `GET /v1/sessions/{id}/stream`) as the polecat session backend.
Under this model, the agent runs in Anthropic's sandbox; gastown subscribes to the
event stream and injects prompts via the API.

This is viable for a subset of gastown workloads (tasks that do not need to write to
the local git worktree). For tasks that do — which is most polecat work (code edits,
git commits, `gt done`) — the Managed Agents sandbox does not have access to the local
filesystem. Path F is therefore excluded from the main recommendation but is worth
revisiting if Anthropic ships Managed Agents with bring-your-own-filesystem support or
a git integration.

---

*End of integration plan.*
