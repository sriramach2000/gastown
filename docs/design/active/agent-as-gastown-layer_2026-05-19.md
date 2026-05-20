# Agent() as Gastown Layer — Integration Design

**Date:** 2026-05-19
**Branch:** `feat/costs-token-paradigm-plus-dashboard-live`
**Driver:** Operator directive 2026-05-19 — "learn how to integrate these sub agents in with the gastown protocol. make it a layer in between the claude sub agents for now. i think theres somethign wrong with the opencodesdk it will require some and the gastown dispatch skill/protoicol"

---

## §1 — Context

Operator dispatched 23 production_planner bug-fix tasks via parent Claude Code on 2026-05-19. Gastown's native scheduler **failed to dispatch any of them** — `last_dispatch_count` stayed at 1 since 02:33Z despite 23 successful `gt sling` enqueues. The Agent() bypass (parent spawning sub-agents directly with worktree isolation) **shipped 11+ bd-closed bugs to demo branch** while gastown's loop sat idle.

**The LEARN:** Agent() works. Gastown's dispatch is broken. Formalize Agent() as a layer that *uses* gastown's protocols (bd ledger, refinery, witness) without depending on gastown's *dispatcher*.

---

## §2 — Three failure points to fix

### §2.1 — Gastown dispatch protocol (`gt scheduler run` is a no-op)

| Symptom | Hypothesis |
|--------|------------|
| `gt scheduler run` returns EXIT 0, spawns 0 polecats | Missing wire from `scheduler.run` to `polecat-spawn`; investigate `internal/scheduler/capacity/dispatch.go` |
| HQ deacon alive but per-rig planner queue not patrolled | `mol-deacon-patrol` doesn't iterate rigs' `scheduler-state.json` queues |
| `gt scheduler list` HANGS | bd lock contention from residual zombie storms (peak 342 procs) |
| `mol-bug-ingestion` Step 2 hangs in `sleep`-loop debugging | Polecat session inherits parent's "Blocked: sleep 30" procedural gate |

**Fix scope:**
- `gastown-dogfood-dispatch-1`: scheduler.run → polecat-spawn wiring gap
- `gastown-dogfood-dispatch-2`: mol-deacon-patrol per-rig queue iteration
- `gastown-dogfood-dispatch-3`: per-rig deacon spawn (or document HQ-deacon-iterates-rigs)
- `gastown-dogfood-dispatch-4`: bd zombie storm prevention (rate-limit witness/refinery fan-out)

### §2.2 — OpenCode SDK rail (M4 wired, **silent-fallback bug found**)

Source audit 2026-05-19 confirms:

| Layer | Status |
|-------|--------|
| `/Users/ryanl/.opencode/bin/opencode` binary | ✅ Installed (106MB, May 10) |
| `opencode_session.go` Start() exec path | ✅ Correctly uses `exec.CommandContext(ctx, s.binary, args...)` with `s.binary = "/Users/ryanl/.opencode/bin/opencode"` |
| Send() prompt delivery | ✅ Uses `opencode run --continue <msg>` (M2 design) |
| `session_manager.go:644` rail dispatch | ✅ Calls `agent.RouteFor(routeBead)` → `agent.New(rail, opts)` |

**The actual bug — silent fallback** (lines 652-655 of `session_manager.go`):

```go
if sessErr != nil && rail == agent.RailOpenCode {
    // OpenCode rail unavailable — fall back to Claude so the polecat still spawns.
    agentSess, sessErr = agent.New(agent.RailClaude, agent.StartOptions{...})
}
```

The fallback emits NO log. Operator sees `[polecat-spawn] bead=<id> rail=opencode rule=7` in logs while the actual binary that spawns is `claude`. Telemetry mistakenly attributes Claude usage to OpenCode. Bead telemetry's `EscalatedFrom` field (M5) goes unset.

**Fix scope (small):**
- Add `fmt.Fprintf(os.Stderr, "[polecat-spawn] OpenCode rail unavailable, fallback → Claude (reason=%v)\n", sessErr)` on the fallback path
- Set bead's `EscalatedFrom = "opencode"` field via UpdateAgentRailTelemetry (M5 surface) so dashboard reflects the actual rail
- Optionally: surface a witness event `polecat-fallback` for monitoring

### §2.3 — Agent()-as-gastown-layer (the integration)

Today (manual weave):
```
Parent Claude
   → spawn N Agent() sub-agents (worktree isolated)
   → each Agent: read ticket → fix code → push fix branch → report
   → Parent on each completion: `bd close <pp-XXX>` manually
[no refinery integration, no witness events, no PR auto-open]
```

Target (layered, sub-agent self-weaves):
```
Parent Claude (orchestrator)
   → Dispatch Layer (in-prompt protocol):
     each Agent prompt includes:
       - bead ID (pp-XXX)
       - bug ticket path
       - worktree directive
       - END-OF-WORK gastown protocol calls:
            bd close <bead-id>
            gh pr create --base demo-... --head <branch>
       - optional witness event emit
Agent() sub-agents (workers)
   → self-close beads + open PRs
Refinery picks up open PRs → merges to demo branch
Witness logs all activity → dashboard SSE → operator sees swarm live
```

---

## §3 — The Gastown-Aware Agent Prompt Preamble

To go at the top of every Agent() spawn from a gastown context:

```
You are a Gastown polecat layer running as a Claude Code Agent() sub-agent.

## Gastown protocol (mandatory)

Your bead: <pp-XXX>
Your rig:  <planner|gastown|lockstep>
Your town: /Users/ryanl/dev/gastown-dogfood

### At task start
- (optional witness): `gt witness emit <pp-XXX> claimed`
- read your bead: `cd /Users/ryanl/dev/gastown-dogfood/<rig> && bd show <pp-XXX>`

### During work
- normal Agent() worktree/edit/commit flow
- NEVER touch other agents' worktrees (/tmp/pp-bug-* is exclusive)

### At task end (BEFORE returning to parent)
- (if fix landed):
    cd /Users/ryanl/dev/gastown-dogfood/<rig>
    bd close <pp-XXX>
- (if PR-worthy):
    gh -R sriramach2000/<repo> pr create \
        --base demo-macbook-pro-46GB --head <branch> \
        --title "fix: <BUG-NNNN> ..." --body "Resolves <BUG-NNNN>"
- (optional witness): `gt witness emit <pp-XXX> closed --commit <sha>`

### If blocked
- `bd update <pp-XXX> --description "BLOCKED: <reason>"`
- DO NOT bd close — let parent escalate
```

**Separation of concerns:**

| Layer | Owns |
|-------|------|
| **Parent Claude** | Batch decision, refinery overflow, octopus-merge for overlap clusters |
| **Gastown prompt preamble** | Bead lifecycle (claim/close), PR creation, witness emission |
| **Agent() sub-agent** | The actual bug fix (code + test + commit) |
| **Gastown native (when fixed)** | Refinery auto-merge, witness event aggregation, dashboard SSE |

---

## §4 — Evidence (2026-05-19 23-bug sweep)

| Bug | Branch | Commit | Bead closed |
|-----|--------|--------|-------------|
| BUG-0118 | fix/BUG-0118-mlx-ollama-watch | a3bc8d0 | ✅ pp-sjl |
| BUG-0134 | fix/BUG-0134-aws-bedrock-cold-path | 50dd370 | ✅ pp-2kx |
| BUG-0156 | fix/BUG-0156-ensemble-umbrella-status | 05bcac1 | ✅ pp-nxv |
| BUG-0161 | fix/BUG-0161-bench-ensemble-columns | f27862b | ✅ pp-bnq |
| BUG-0163 | fix/BUG-0163-tesseract-nits | b617628 | ✅ pp-1cm |
| BUG-0378 | fix/BUG-0378-spa-form-audit | 5ffd2b2 | (umbrella, stays open) |
| BUG-0413 | fix/BUG-0413-line-items-routing | a3ffd5d | (deferred-status, stays open) |
| BUG-0421 | fix/BUG-0421-ngf-v2.6 | 118f920 | ✅ pp-pub |
| BUG-0426 | fix/BUG-0426-kb-messages-persist | c05561a | ✅ pp-bpy |
| BUG-0427 | fix/BUG-0427-migration-0035 | 54141c8 | ✅ pp-7s6 |
| BUG-0428 | fix/BUG-0428-faithfulness | 0d6a546 | ✅ pp-e46 |
| BUG-0431 | fix/BUG-0431-gemma4-e4b | 24031e3 | ✅ pp-zp1 |
| BUG-0432 | fix/BUG-0432-pvc-pre-install | 5e29a20 | ✅ pp-hf4 |
| BUG-0435 | fix/BUG-0435-r-inv-003 | 6dd7404 | ✅ pp-3v6 |
| BUG-0436 | fix/BUG-0436-qty-confidence | a238e40 | ✅ pp-4bk |
| (8 in-flight) | (...) | (...) | (TBD) |

**Conversion:** 15 of 23 completed in ~25 minutes. 11 beads closed via `bd close`. CLI-Anything wire-in landed at `chore/wire-cli-anything-browser` on the fork.

---

## §5 — Next actions

1. **Sub-agent prompt template** — codify the §3 preamble as `~/.claude/templates/gastown-aware-agent-preamble.md`; reference from `~/.claude/rules/common/agent-prompt-preamble.md`
2. **Dispatch tickets** — file the 4 dispatch bugs in §2.1 as `gastown-dogfood-dispatch-{1..4}`
3. **OpenCode SDK validation** — bench whether `internal/agent/opencode_session.go` actually launches opencode; revert default to Claude if broken
4. **Refinery PR-auto-merge** — when bypass agents push `fix/BUG-NNNN-*`, refinery should auto-open PR + merge once tests pass
5. **Witness event emit from sub-agents** — minimal: `gt witness emit <bead> claimed|closed` calls inside the preamble

---

## §6 — File-overlap reality check

The 5 Wave 4 KB-chat bugs (BUG-0423/0424/0425/0426/0430) all touch
`backend/app/routers/kb_chat.py`. Operator chose all-23-parallel knowing this.
Agents run on disjoint `/tmp/pp-bug-NNNN/` worktrees so they don't collide at
edit time — but PR merges will conflict. Resolution paths:

1. Last-PR-to-merge handles conflicts via rebase
2. Octopus-merge into `feat/wave-4-kb-chat-sweep` integration branch first, then one PR
3. Refinery (when working) detects overlap class and routes through option 2 automatically

Operator's accepted-risk call. Tracking as `gastown-dispatch-5`.

---

Related:
- [[feedback-gastown-dispatch-failure-modes]] — failure modes in memory
- [[project-dual-sdk-runtime-router]] — M1-M5 done, OpenCode rail unvalidated
- [[project-mol-bug-ingestion-dispatcher]] — formula Step 2 hangs
