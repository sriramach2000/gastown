# Production Planner Integration — 2026-05-19

> **Pivot context.** Earlier this session targeted gastown-dogfood as the integration site. Operator
> redirected mid-session: `sriramach2000/production_planner` is the actual product; gastown is the
> orchestrator; CLI-Anything (HKUDS fork) is a browser-click harness for SPA testing.

## Current wiring state

| Layer | Component | State | SHA / Path |
|---|---|---|---|
| Product | production_planner @ `demo-macbook-pro-46GB` | Vite+React+Ollama+ADK; 227 resolved / 9 pending P1s | local: `/Users/ryanl/dev/production_planner` |
| Orchestrator | gastown `gt` binary | Rebuilt with SSE+taxonomy | v1.1.0-180-gf19006a7 |
| HQ | gastown-dogfood town | 3 rigs: gastown, lockstep, **planner** (new) | `/Users/ryanl/dev/gastown-dogfood` |
| Rig | planner | Cloned, witness+refinery patrol beads spawned | `gastown-dogfood/planner/` |
| Beads | 9 production_planner mirrors | `pp-*` prefix | open via `bd list` in planner rig |
| First dispatch | pp-bzs (NGF v2.0.2 → v2.6.0 bump) | Convoy `hq-cv-i43hq`, wisp `pp-wisp-hhi` | Queued, polecat spawn pending |
| Browser harness | cli-anything-browser (DOMShell) | Installed in venv | `/tmp/cli-anything-venv` |

## What landed (chronological)

### Phase 1 — Audit closure
- v3.0 row 34 (warrant) merged at HEAD `061a905` (lockstep `design/go-port-to-gastown`)
- 297 BUGs / 96 DECISIONS / 22 sub-rules audit catalog complete (separate workstream)

### Phase 2 — gastown dashboard fidelity (3-agent parallel swarm)
- Agent A (research) → `swarm/research-cli-anything` @ `1a7e01f9` — 752 LoC analysis
- Agent B (SSE) → `swarm/dashboard-sse` @ `f1e67343` — 338 LoC, 4/4 tests
- Agent C (taxonomy) → `swarm/agent-taxonomy` @ `1832d6bd` — 963 LoC, 8/8 tests
- Orchestrator wiring → merged into `feat/costs-token-paradigm-plus-dashboard-live` @ `f19006a7`
  - `mux.Handle("/events", NewSSEHandler(NewNoopPublisher(), 2*time.Second))`
  - `mux.Handle("/api/taxonomy", NewTaxonomyHandler())`
  - `web.SetTaxonomyBuilder(...)` in `internal/cmd/dashboard.go` (bridges `cmd.Taxonomy` -> `web.TaxonomyResponse`)
  - `rootCmd.AddCommand(taxonomyCmd)` in `internal/cmd/taxonomy.go` init
  - htmx-ext-sse loader + `sse-connect="/events"` on dashboard div
- Pushed to `fork=sriramach2000/gastown` (operator-approved push target)
- Race condition: 3 parallel `Agent` calls without `isolation: "worktree"` shared the working tree; branches stacked but no file overlap thanks to disjoint claims

### Phase 3 — Production planner integration
- `gt rig add planner https://github.com/sriramach2000/production_planner.git --prefix pp` (57.9s)
- 9 BUG-XXXX tickets mirrored as `pp-*` beads:
  - **pp-bzs** P2 — Bump NGF v2.0.2 -> v2.6.0 (slung; queued)
  - **pp-82w** P1 — Multi-engine OCR ensemble umbrella
  - **pp-ty1** P2 — SPA form-validation master audit (CLI-Anything browser target)
  - pp-8ji, pp-c9r, pp-fzx, pp-k3p, pp-my4, pp-yze (6 P3 watch/polish)
- CLI-Anything fork: `sriramach2000/CLI-Anything` (Apache-2.0, 147 hub CLIs)
- `cli-anything-browser` harness installed in `/tmp/cli-anything-venv`
  - CLI subcommands: `act`, `fs`, `page`, `repl`, `session`
  - Architecture: Click -> DOMShell MCP server (subprocess) -> Chrome accessibility tree as filesystem

## Gastown infra bugs uncovered (filed today)

| ID | P | Title | Impact |
|---|---|---|---|
| gastown-dogfood-wy8 | P2 | `gt rig add` creates dangling formulas symlink when gastown rig not yet cloned | Blocks `gt sling` entirely until manual symlink fix |
| gastown-dogfood-k45 | P2 | Rig identity bead uses `gt-` prefix instead of rig's bd prefix | Routing warnings on every gt command |
| gastown-dogfood-hju | P3 | Stale gastown rig entry in rigs.json (no local clone) | Cosmetic warning |

Workaround applied for pp-bzs sling: `rm .beads/formulas; ln -s /Users/ryanl/dev/gastown/internal/formula/formulas .beads/formulas`.

## Pending operator actions

1. **Restart the running dashboard** (PID 65636 on :8080) so `/events` and `/api/taxonomy` activate
2. **Install Chrome DOMShell extension** for CLI-Anything browser harness to function
3. **Resolve the polecat-doesn't-spawn issue** (gastown-dogfood-cy3, existing P1) so pp-bzs can actually execute

## Next slices

| Slice | Effort | Unblock signal |
|---|---|---|
| Wire CLI-Anything browser as a formula-callable tool in the planner rig | M | Operator installs DOMShell ext |
| Backfill polecat spawn issue (cy3) so dispatch loop actually completes | L | This is the long pole |
| Build a taxonomy panel in convoy.html (currently endpoint exists but no UI) | S | Dashboard restart |
| Mirror the rest of production_planner's resolved tickets as bd refs for prior-art search | M | Optional, helps polecats |

## References

- Sibling research: cli-anything-integration-2026-05-19.md
- Sibling research: gastown-positioning-2026-05-19.md
- production_planner handoff: `/Users/ryanl/dev/production_planner/.claude/handoff/2026-05-14-wave-4-16-kb-chat-design-handoff.md` (pinned at `cb0f672`)
- Memory pointer: `~/.claude/projects/-Users-ryanl-dev-lockstep/memory/project_production_planner_integration.md`
