# Handoff — Gastown ↔ Claude Code Integration Improvement (Next Session)

**Date:** 2026-05-19
**Branch:** `feat/costs-token-paradigm-plus-dashboard-live` @ `ce178822` (on fork)
**Driver:** Operator 2026-05-19 — "why doesnt gastown work properly with claude code. there must be something im missing. it should not be this hard"

## §1 — Branch state

| Repo | Branch | HEAD | Push state |
|------|--------|------|------------|
| gastown | feat/costs-token-paradigm-plus-dashboard-live | `ce178822` | ✅ on `fork=sriramach2000` |
| production_planner | demo-macbook-pro-46GB | `7f5ae42` | ✅ on `origin=sriramach2000` |

Working tree clean.

## §2 — What landed today

- `bd0ff711` — fix: silent OpenCode→Claude fallback log (`gastown-dogfood-sfb`)
- `84a89db3` — fix: M5 rail telemetry actually wired (was dead code) (`gastown-dogfood-sfb-2`)
- `ce178822` — docs: Agent()-as-gastown-layer integration architecture (the architectural diagnosis)
- 15 of 23 production_planner bug fixes merged to demo (8 still in-flight in operator's parallel session)

## §3 — Open items (5 filed bd beads — gastown-dogfood town)

The five bugs that explain "why gastown doesn't work properly with claude code":

| Bead | P | Title | Approach |
|------|---|-------|----------|
| **gastown-dogfood-q5e** | **P0** | polecat sessions inherit operator-mode ~/.claude rules + hooks, gates fire inside autonomous bots | **The dominant friction.** Polecat spawns claude with `--dangerously-skip-permissions` but the process still reads `~/.claude/rules/` and `~/.claude/settings.json` hooks. Operator-mode gates (Blocked-sleep, Fact-Forcing, Destructive-command) fire inside polecats with no one to satisfy them. Fix: settings/rules isolation (see §5). |
| gastown-dogfood-1ed | P1 | gt scheduler run is a no-op | `internal/cmd/scheduler.go` + `internal/scheduler/capacity/dispatch.go` — wiring from "scheduled bead" to `polecat-spawn` never fires. `scheduler-state.json.last_dispatch_count` stays at 1 since 02:33Z. |
| gastown-dogfood-3zg | P1 | mol-deacon-patrol does not iterate per-rig scheduler queues | `internal/formula/formulas/mol-deacon-patrol.formula.toml` patrols HQ-level work only; doesn't enumerate per-rig `<rig>/.runtime/scheduler-state.json` queues. |
| gastown-dogfood-bjo | P1 | bd zombie storm: 342 procs peak during 23-polecat parallel sweep | bd auto-imports `issues.jsonl` on every command + no rate limit. Dashboard + scheduler + refinery race. Watchdog SIGKILLs procs but storm re-spawns faster. Need: bd rate-limit OR in-memory bd proxy. |
| gastown-dogfood-7ak | P2 | per-rig deacon never spawned: planner has no pp-deacon | Either HQ deacon iterates rigs (sibling to 3zg) OR each rig spawns its own `pp-deacon` at boot. Choose one architecture. |

## §4 — Related friction not separately filed

- `mol-bug-ingestion` Step 2 partition hangs in sleep-loop debugging — root cause = q5e (inherited Blocked-sleep gate)
- M5 telemetry write only fires from Manager-path spawns; 7+ `NewSessionManager` call sites in `cmd/` (`polecat_spawn`, `rig`, `up`, `rig_dock`) don't pass `opts.Beads` yet
- OpenCode silent-fallback log added in `bd0ff711` BUT the M5 ledger write of `EscalatedFrom="opencode"` requires `opts.Beads` to be passed (see above)
- `gt scheduler list` HANGS — even after killing zombies — because subsequent bd queries get killed too. Investigate `gt-watchdog` SIGKILL window
- 23-Agent() parallel pattern (today's bypass) is the working dispatcher BUT requires a parent claude session to coordinate. Codify the gastown-aware preamble as a reusable Claude Code skill

## §5 — Resume instructions (first 5 commands)

```bash
# 1. Read the architectural diagnosis (on remote)
cd /Users/ryanl/dev/gastown
cat docs/design/active/agent-as-gastown-layer_2026-05-19.md

# 2. Confirm the 5 filed beads
cd /Users/ryanl/dev/gastown-dogfood
bd list --label gastown-dogfood --status open

# 3. Tackle gastown-dogfood-q5e FIRST — unblocks the other 4
#    The fix is small (no Go code; just settings + env wiring):
#      a. Polecat .claude/settings.json should disable PreToolUse/UserPromptSubmit
#         hooks. Today polecats inherit ~/.claude/settings.json.
#      b. Gastown's polecat-spawn should export GASTOWN_POLECAT_MODE=1
#         (one new line in internal/polecat/session_manager.go Start())
#      c. Update ~/.claude/rules/common/*.md hook scripts to bail early when
#         GASTOWN_POLECAT_MODE=1 is set
#    Estimated diff: ~15 lines across 3 files. Test: spawn a polecat and verify
#    sleep + write commands work without gate fires.

# 4. After q5e lands, retest mol-bug-ingestion end-to-end
gt sling mol-bug-ingestion planner --var rig=planner --var dry_run=true
# Step 2 partition should complete without sleep-loop hell

# 5. Continue dispatch-1 → dispatch-3 → dispatch-4 → dispatch-7ak in order
#    (each is independently testable; dispatch-4 is the largest scope —
#    consider an in-memory bd proxy or fan-out throttle)
```

## §6 — Pointers (read first)

- **Design doc on remote:** `gastown/docs/design/active/agent-as-gastown-layer_2026-05-19.md` (192 lines) — full architecture, evidence table, target architecture
- **Memory (device-local):**
  - `~/.claude/projects/-Users-ryanl-dev-lockstep/memory/feedback_gastown_dispatch_failure_modes.md` — gastown failure-mode catalog with reproducers
  - `~/.claude/projects/-Users-ryanl-dev-lockstep/memory/project_production_planner_canonical_pointers.md` — repo / branch / handoff anchors
  - `~/.claude/projects/-Users-ryanl-dev-lockstep/memory/feedback_worktree_isolation_mandatory_for_parallel_subagents.md` — adjacent lesson
- **Sibling handoff (same date, different scope):** `.claude/handoff/2026-05-19-session-9-dual-sdk-handoff.md` — dual-SDK router work
- **Sibling session active:** operator running 8-PR merge campaign in parallel — let it finish before retesting gastown end-to-end

## §7 — Sibling sessions

| Session | Branch | Work |
|---------|--------|------|
| **This session** (next pickup) | gastown `feat/costs-token-paradigm-plus-dashboard-live` | Fix 5 gastown-dogfood-* bugs (q5e first) |
| **Sibling session** (running now) | production_planner `demo-macbook-pro-46GB` | Finish 8 remaining bug-fix merges + helm upgrade |

Decoupled — gastown improvements don't block the prod_planner merge campaign and vice versa.

## §8 — Why this matters

Gastown's value proposition: **operator drops a `BUG-NNNN-foo.md` ticket → Mayor handles the rest** (mine → file bead → sling polecat → fix → refinery merge). Today, only the bd ledger half works. The dispatcher half (scheduler → polecat-spawn → autonomous fix) is broken because of q5e (settings inheritance) compounded by 1ed/3zg/7ak (wiring gaps) and bjo (bd contention storms under load).

q5e is high-leverage because:
- It's the smallest diff (settings + 1 env var)
- It unblocks every other dispatch bug (formula bash can sleep, polecats can write files without gates)
- It's the difference between "gastown works for single-bug runs" (today) and "gastown handles 23-parallel runs" (target)

Once q5e is fixed, the rest of the dispatch bugs become testable end-to-end, and the Agent()-bypass pattern goes away.

---

**Cold-resume directive:** read §1 → §2 → §3 → §5. Start with `gastown-dogfood-q5e`. Don't touch the sibling session's prod_planner merge campaign.
