# Gastown session handoff (2026-05-21)

**Purpose:** the `/resume-session` companion. Read this on next-session boot before doing anything else.

---

## 1. Status at handoff

The gastown scaffolding for `xl4_client_log_fluentd_processor_clean` is **live but unproductive** — town/rig/beads/convoys/escalations all exist, but no polecat has completed a single piece of work. The session's only landed code is two recovery commits on `feat/rca-path-raw` (`818676a` design docs, `8544335` rictus's 16-line B.1 starter). Operator amended L1 policy mid-session: **no pushes to `main` on this project; everything lands on `feat/rca-path-raw`**. A new worker contract (Doc 2) is proposed but not yet operator-confirmed.

## 2. Live state inventory

| Surface | Identifier(s) | Notes |
|---------|--------------|-------|
| xl4 repo branch (current HEAD) | `feat/rca-path-raw` @ `8544335` | All future Part A + Part B work lands here |
| Beads | `xl4-t1c` (A.0, OPEN, dementus), `xl4-42x` (A.1-A.4, OPEN), `xl4-7ne` (A.5, OPEN), `xl4-ypv` (B.1, IN_PROGRESS, rictus), `xl4-t25` (B.2, OPEN) | All 5 carry corrected `base_branch: feat/rca-path-raw` |
| Epics | `xl4-0vd` Part A (0/3), `xl4-0h9` Part B (0/2) | Both tracked by convoys |
| Convoys | `hq-cv-19650` Part A, `hq-cv-d80yx` Part B | Both 0% |
| Polecats (idle) | `dementus`, `furiosa`, `nux`, `rictus`, `slit`, `capable` | All sandboxes exist; rictus has the only uncommitted progress (already salvaged) |
| Open escalations | `hq-vmr`, `hq-zuo`, `hq-cf5` | Must be resolved before re-slinging |
| Acked escalations | `hq-wisp-h4h`, `hq-wisp-s33`, `hq-wisp-d4z` | Resolved by mayor ack; reference only |
| Gastown fix branch | `fix/hook-validator-and-sling-sync-2026-05-21` @ `c526d63d` on `sriramach2000/gastown` | Fixes Bug 1 (isBeadID); diagnoses Bug 2 (sling-bd sync) |
| Candidate binary | `~/dev/sriram/PersonalProjects/gastown/gt-patched` (~37 MB) | Install with `mv gt-patched ~/go/bin/gt` if operator approves |
| Live binary | `~/go/bin/gt` at upstream `v1.1.0-175-g625bcf8a` | Unchanged from start of session |
| Stale branch | `promote/xl4-t1c-main-assets-2026-05-21` on xl4 origin | Abandoned promote-to-main; delete or keep — operator's call |

## 3. Read these first on resume (in this order)

1. **`docs/design/gastown_bootstrap_audit_2026-05-21.md`** — postmortem; honest failure inventory; what we got right; outstanding state table.
2. **`docs/design/gastown_worker_policy_2026-05-21.md`** — proposed worker contract; branch policy + verify-effect + cookie trail + skill whitelist + 3-attempt rule + `~/gt/.policy/check.sh` scaffold. **Now extended by two amendments (read in order):**
   - **`docs/design/gastown_worker_policy_amendment_cli_hub_2026-05-21.md`** — v0.1 APPROVED 2026-05-21 ("essential for full autonomy"); Tier 2.5 pre-baked allowlist + Tier 3 discovery (`cli-hub-meta-skill`); §A12 mechanical rollout checklist now in flight via setup.sh patches (towns/xl4/bootstrap/ in this fork).
   - **`docs/design/gastown_worker_policy_amendment_hospital_2026-05-21.md`** — v0.2 PROPOSED; `bug-feedback-loop` to Tier 1 mandatory (prior-art gate at `gt prime` + lifecycle gate before `gt done`); `docs/bugs/{pending,resolved,wontfix}/BUG-NNNN-<slug>.md` schema; severity × shape escalation fork. Awaits operator confirm.
3. **`docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md`** — original kickoff spec; L1–L15 locked decisions; F1 resolution; Phase A.0/A.1-A.4/A.5 + B.1/B.2.
4. (Reference) `docs/design/log_analyzer_app_and_cache_pivot_2026-05-20.md` — full 1050+ line planning doc.

## 4. First actions on next-session boot

1. **Decide gastown binary install** — `mv gt-patched ~/go/bin/gt` (recommended; fixes Bug 1) or stay on upstream (Bug 1 returns).
2. **Read the salvage agent's report** in the prior transcript and verify with `git log --oneline feat/rca-path-raw` that rictus's `8544335` is the tip's parent.
3. **Confirm or amend Doc 2** (`gastown_worker_policy_2026-05-21.md`) — the §5 skill whitelist + §10 `check.sh` scaffold are the two highest-leverage points.
4. **Resolve the 3 open escalations** (`hq-vmr`, `hq-zuo`, `hq-cf5`) with explicit evidence; each requires either a spec patch (vmr), a workflow change (zuo: GitLab branch protection on `main` — actually now moot since policy forbids `main` pushes), or a re-sling decision (cf5).
5. **Decide the re-sling strategy** — operator's call: re-sling all 5 beads on `feat/rca-path-raw` under the new policy, OR re-decompose given B.1 already has a starter commit landed.

## 5. Open decisions awaiting operator

| # | Decision | Default if no decision |
|---|----------|------------------------|
| 1 | Merge gastown fix branch upstream (open PR against `steveyegge/gastown`)? | Keep private; let fork drift |
| 2 | Delete `promote/xl4-t1c-main-assets-2026-05-21` stale branch? | Keep (cheap; harmless) |
| 3 | Relax L1 (Part A & B sharing base `feat/rca-path-raw`) explicitly in the v5 kickoff, or treat the new policy as a tactical exception? | Treat as tactical exception; v5 kickoff is unchanged |
| 4 | Re-sling all 5 beads vs cherry-pick start with B.1 only (it has the only existing momentum)? | Re-sling all 5 |
| 5 | Confirm §5 skill whitelist in Doc 2 — add/remove? | Ship as-drafted |
| 6 | Confirm `~/gt/.policy/check.sh` scaffold — accept or rewrite stricter? | Ship as scaffold; mayor refines as the wave runs |

## 6. Known bugs still unpatched

- **Gastown Bug 2 — sling-bd sync.** Polecats sometimes idle with `attached_formula` absent from `gt bd show` for ~5–15s after `gt sling` returns. Workaround: re-sling triggers a fresh nudge. Long-term fix needs daemon-timer-driven jsonl export in upstream gastown.
- **Gastown Bug 3 (suspected, unconfirmed) — empty-hook silent exit.** When a polecat is slung with an empty bead (no `attached_formula` ever appears), the polecat Claude session can exit with no log line. This was the trigger for `hq-cf5`; root cause not yet bisected.

---

**One-line summary for `/resume-session` headline:** *Bootstrap shipped scaffolding + recovered work; no polecat completed any task; new worker contract awaits operator confirm; branch policy is now `feat/rca-path-raw`-only for this project.*
