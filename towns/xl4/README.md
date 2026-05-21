# towns/xl4 — gastown configuration for the xl4 town

This directory holds **all configuration for the xl4 town** (the gastown deployment that orchestrates polecats against `xl4_client_log_fluentd_processor_clean`). It lives in the gastown fork because it is *orchestration plumbing*, not feature code — every artefact here speaks the language of gastown (rigs, beads, sling, polecats, cookie-trail), not of the xl4 RCA pipeline.

Created **2026-05-21** by relocating from `~/dev/sriram/PersonalProjects/gastown-bootstrap-xl4/` (plain dir, never git-versioned) and from `xl4_client_log_fluentd_processor_clean/docs/design/gastown_*` (mis-placed during the 2026-05-21 bootstrap session — see audit doc).

## Layout

```
towns/xl4/
├── README.md                    (this file)
├── bootstrap/
│   ├── setup.sh                 idempotent bootstrap: gastown + xl4 rig + 5 beads + 2 epics + wave-1 sling
│   └── open-sessions.sh         gnome-terminal multiplexer for polecat windows
└── policy/
    ├── gastown_worker_policy_2026-05-21.md                          v0 base contract (operator-ratified)
    ├── gastown_worker_policy_amendment_cli_hub_2026-05-21.md        v0.1 APPROVED — cli-hub-meta-skill integration
    ├── gastown_worker_policy_amendment_hospital_2026-05-21.md       v0.2 PROPOSED — bug-feedback-loop integration
    ├── gastown_bootstrap_audit_2026-05-21.md                        postmortem of the original bring-up
    └── gastown_session_handoff_2026-05-21.md                        /resume-session entry point
```

## Bootstrap

```bash
# One-time, from this directory:
cd ~/dev/sriram/PersonalProjects/gastown/towns/xl4
bash bootstrap/setup.sh
```

The script is **idempotent**: marker files at `~/gt/.xl4-bootstrap/{step}.done` track per-step completion. To force a re-run of a step, `rm ~/gt/.xl4-bootstrap/<step>.done` and re-run.

Steps (in order, from `setup.sh main()`):

| Step | Function | Marker | Effect |
|------|----------|--------|--------|
| prereqs | `check_prereqs` | — | verify git/go/dolt/tmux/agent-runtime |
| 1 | `build_gastown` | build | clone fork, `make build`, install `$HOME/go/bin/gt` |
| 2 | `init_town` | town | `gt install $GT_HOME` creates `mayor/town.json` |
| 3 | `add_rig` | rig | `gt rig add xl4` wrapping the xl4 repo path |
| 3b | `ensure_crew` | crew | `gt crew add <whoami>` |
| 3c | `install_cli_hub` | cli_hub | `pip install --user cli-anything-hub` (per `policy/gastown_worker_policy_amendment_cli_hub_2026-05-21.md` §A12) |
| 3d | `deploy_policy` | policy | create `~/gt/.policy/check.sh` + `cli_hub_allowlist.yaml` (skips if polecats live) |
| 4 | `create_beads` | beads | 5 task beads (A.0/A.1-4/A.5/B.1/B.2) into `~/gt/xl4/.beads/` |
| 4b | `ensure_epics` | epics | 2 epic beads for Part A + Part B |
| 4c | `reparent_tasks` | reparent | task beads → parent epics |
| 5 | `create_integrations` | integrations | `gt mq integration create` for Part A → main and Part B → feat/rca-path-raw |
| 6 | `create_convoys` | convoys | 2 convoys wrapping the epics |
| 7 | `sling_wave1` | sling_wave1 | formula-sling A.0 + B.1 from town root |
| 8 | `verify_polecats` | verify | poll bead status for ~45s; warn if no pickup |

## Policy enforcement

Polecats execute under the contract in `policy/gastown_worker_policy_2026-05-21.md`. The contract is enforced mechanically by `~/gt/.policy/check.sh` (deployed by `setup.sh` step 3d), which runs at every wave boundary and exits non-zero on any violation. The two amendments extend the contract:

- **v0.1 (APPROVED)** — cli-hub-meta-skill discovery primitive. Polecats may `cli-hub search/info` read-only; `cli-hub install` requires an operator-approved entry in `~/gt/.policy/cli_hub_allowlist.yaml`.
- **v0.2 (PROPOSED)** — bug-feedback-loop hospital. Polecats run a prior-art gate at `gt prime` and a lifecycle gate before `gt done`; defects materialise as `<xl4-repo>/docs/bugs/{pending,resolved,wontfix}/BUG-NNNN-<slug>.md` with bidirectional regression-test wire.

## Live state (lives outside this repo)

- `~/gt/` — the town root; created by `gt install`. Town metadata, beads dolt DB, rig dirs, polecat sandboxes.
- `~/gt/.policy/` — runtime policy: `check.sh`, `cli_hub_allowlist.yaml`. Deployed by `setup.sh` step 3d.
- `~/gt/xl4/` — the xl4 rig: bead DBs, polecat dirs, cookie trail (`~/gt/xl4/.cookie-trail.jsonl`).
- `~/gt/.xl4-bootstrap/` — marker files for bootstrap idempotency.

The xl4 codebase that polecats edit lives at `~/dev/sriram/ExcelforeProjects/fluent-d/xl4_client_log_fluentd_processor_clean/` — a wholly separate repository.

## Cross-repo dependencies

| Dependency | Location | Purpose |
|---|---|---|
| gastown engine (this fork) | `~/dev/sriram/PersonalProjects/gastown/` | the `gt` binary that polecats use |
| Bug 1 fix (in this fork) | branch `fix/hook-validator-and-sling-sync-2026-05-21` @ `c526d63d` | `isBeadID` validator fix for `gt hook attach <bead> <name-with-digits>`; install via `make build && mv gt $HOME/go/bin/` |
| xl4 codebase | `~/dev/sriram/ExcelforeProjects/fluent-d/xl4_client_log_fluentd_processor_clean/` | the codebase polecats edit; branch `feat/rca-path-raw` |
| bug catalog (when v0.2 ratified) | xl4 repo `docs/bugs/{pending,resolved,wontfix}/` | persistent defect log |

## Branch policy

This config is **operator-private** — it pins paths, secrets-adjacent allowlists, and town-specific assumptions. Do NOT PR this branch upstream to `steveyegge/gastown`. The Bug 1 fix on `fix/hook-validator-and-sling-sync-2026-05-21` is the only branch from this fork intended for upstream contribution.

## Provenance

- **Bootstrap audit + worker policy v0** authored 2026-05-20/21 by postmortem author after the first failed gastown bring-up against xl4.
- **cli-hub amendment v0.1** authored 2026-05-21 PM, APPROVED same day by operator ("essential for full autonomy").
- **Hospital amendment v0.2** drafted 2026-05-21 PM after operator question "is there a hospital in this gastown for broken features to be logged?". Awaits ratification.
- **Relocation to this fork** 2026-05-21 evening — single move commit `local/xl4-town-config-2026-05-21` branch.
