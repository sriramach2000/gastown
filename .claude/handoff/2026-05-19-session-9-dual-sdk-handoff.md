---
session_date: 2026-05-19
session_topic: Dual-SDK runtime router (M1-M5) + production_planner integration + bug dispatch pipeline
canonical_production_planner_branch: demo-macbook-pro-46GB
canonical_gastown_branch: feat/costs-token-paradigm-plus-dashboard-live
gt_binary_at_handoff: v1.1.0-199-gc9a88bfc-dirty
---

# Session 9 — Dual-SDK Router + Bug Dispatch

## 1 — Canonical pointers
- gastown: sriramach2000/gastown.git -> feat/costs-token-paradigm-plus-dashboard-live
- prod_planner: sriramach2000/production_planner.git -> demo-macbook-pro-46GB
- prod_planner predecessor handoff: cb0f672 at .claude/handoff/2026-05-14-wave-4-16-kb-chat-design-handoff.md

## 2 — What landed

ADR Accepted .claude/decisions/2026-05-19-dual-sdk-runtime-router.md
- M1 c2ff721e: Session interface + claude_session.go
- M2 467c8b89: opencode_session.go
- M3 5c7fb918: router.go + complexity.go (7-rule classifier; first-match-wins; default OpenCode)
- M4 c9a88bfc: wire RouteFor into session_manager Start()
- M5 5b2d4096: telemetry (rail/cost/escalated_from + gt rail-stats)
- Token-bloat firewall: 34 lockstep adapters NEVER as MCP tools (ADR §2.4)

Polecat refactor:
- Stage 1: max_polecats 8->10 in settings/config.json
- Stage 2 faf7be21: per-job worktree (default scheduler.allow_polecat_reuse=false); polecat destroys worktree on done

Bug fixes (gastown):
- cy3 band-aid 58bb704a (read-side ParseAgentFields fallback)
- cy3 writer-side: ab59e40f IN-FLIGHT
- ys4 a9db7148 (fetcher mutex; bd zombie storm root-cause)
- wy8 fdf89e16 (formulas symlink fix; gt rig add provisions formulas/)
- k45+hju e7f7c156 (rig identity prefix + one-shot absent-clone warning)

Production_planner:
- Planner rig installed at gastown-dogfood/planner/
- 9 BUG-* mirrored as pp-* beads
- BUG-0415 EKS NVIDIA runbook: polecat jasper -> manual cherry-pick -> PR #3 MERGED to demo-macbook-pro-46GB
- mol-bug-ingestion cfad1143 (autonomous formula; integrated into mol-deacon-patrol)

CRITICAL FINDING: Cherry-pick survey 2026-05-19 confirmed BUG-0415 is the ONLY real polecat output across 17 branches. All others are CLAUDE.local.md / gt-p35 strip noise. Polecats are prioritizing town-patrol over their hook_bead. Worth a new gastown-dogfood-* bug.

## 3 — In-flight at handoff

| Agent | Stream | Expected branch |
|---|---|---|
| ab59e40f | cy3 writer-side | fix/gastown-dogfood-cy3-writer-side |
| a89de569 | Live polecat-status SSE events | feat/sse-live-polecat-status |
| aa6ce0eb | Mine production_planner source for un-filed BUGs | chore/mine-pending-bugs-2026-05-19 |

(afaeeedc cherry-pick agent returned with "1 real, 16 skip" — no new PRs.)

## 4 — Operational state
- Watchdog: gt-watchdog daemon, 15s/60s
- Dashboard: gt dashboard --port 8080
- OpenCode v1.14.48 ~/.opencode/bin/opencode
- DOMShell Chrome ext: NOT INSTALLED (operator action)

## 5 — Resume cookbook
```
cd /Users/ryanl/dev/gastown && git log --oneline -5 feat/costs-token-paradigm-plus-dashboard-live
gt --version && gt-watchdog status
git rev-parse --abbrev-ref HEAD && ls docs/bugs/pending/
cd /Users/ryanl/dev/gastown-dogfood && gt convoy list && gt scheduler status
```

## 6 — Next moves
1. Wait for in-flight: cy3-writer, SSE events, mine-bugs
2. Bootstrap mol-bug-ingestion: gt sling mol-bug-ingestion planner/polecats --var rig=planner
3. Operator installs DOMShell ext for CLI-Anything SPA testing
4. File the polecat-prioritizes-gt-p35-over-hook_bead bug (new finding)
5. Refinery autonomy is the next architectural piece
