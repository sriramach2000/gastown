# Gastown worker policy (2026-05-21)

**Status:** PROPOSED — operator confirms before next-session boot.
**Replaces:** any prior implicit worker contract.
**Companion:** `gastown_bootstrap_audit_2026-05-21.md` (postmortem) + `gastown_session_handoff_2026-05-21.md` (resume pointer).
**Audit teeth:** every clause below maps to a check in `~/gt/.policy/check.sh` (§10). Polecats that fail the check do not advance the wave.

---

## 1. Scope

Applies to **every polecat slung in any rig under the `xl4` town** (and any future town that adopts this policy). This document must be:

1. Read aloud by every polecat at every `gt prime` call (the polecat's first action after waking).
2. Linked from each bead's spec text (one line near the top: `Policy: docs/design/gastown_worker_policy_2026-05-21.md — read before editing`).
3. Linked from `~/gt/xl4/polecats/AGENTS.md` so the per-polecat sandbox loads it as standing context.

Operator approval of this document is what makes the worker contract binding. Until then, treat it as the de-facto contract anyway.

## 2. Branch policy

| Project / rig | Allowed push targets | Hard NO |
|---------------|---------------------|---------|
| `xl4` (this project, `xl4_client_log_fluentd_processor_clean`) | `feat/rca-path-raw` (and feature branches off it; integration branches off it) | Any push to `main` — operator changed L1 on 2026-05-21; Part A's old "land on main" rule is suspended for this project |
| Future rigs | Defined in each rig's `AGENTS.md` `## Branch policy` section | Default deny on `main` until operator explicitly opts in |

Polecats that need to update the policy file or change the target branch MUST file `gt escalate -s high` with specific evidence and wait for the mayor / operator. Self-amending the policy is a fireable offence (figuratively).

## 3. Verify-effect contract (Block 14, made enforceable)

After **every** state-changing action — file write, git commit, bead state flip, sling, formula attach — the polecat MUST verify the effect before claiming success. **Exit code 0 is a signal, not a guarantee.**

Quote from the global preamble (Block 14, agent-prompt-preamble.md, verbatim):

> **Rule.** When wrapping an external CLI or long-running command, VERIFY THE EFFECT (the state change on disk / API / service), not the exit code. Exit 0 is a signal, not a guarantee. Do NOT trust `$?` alone — inspect the produced artefact.

**Bash template:**

```bash
# Wrong
gt sling <bead> --to <polecat> || { echo "sling failed"; exit 1; }

# Right
gt sling <bead> --to <polecat> || true
gt bd show <bead> | grep -q "attached_formula:" \
  || { echo "sling produced no effect — bead has no attached_formula"; exit 1; }
```

**Python template:**

```python
# Wrong — capture_output hides crashes; rc may be 0 anyway
result = subprocess.run(cmd, capture_output=True, check=False)

# Right — let stderr surface, verify the artefact
subprocess.run(cmd, check=False)
if not expected_output.exists():
    raise RuntimeError(f"{cmd[0]} produced no effect")
```

**Quick effect checks for common actions:**

| Action | Effect to verify |
|--------|-----------------|
| `git commit` | `git log -1 --name-only HEAD` shows the expected files |
| `git push` | `git ls-remote origin <branch>` SHA matches local HEAD |
| `gt sling` | `gt bd show <bead>` shows `attached_formula` line |
| `gt done` | bead status flips to `closed`; MR appears in escalation log |
| file write | `test -s <path>` and `wc -l <path>` is non-zero |
| python install | `python -c "import X; print(X.__version__)"` succeeds |

## 4. Cookie-trail contract

Every state-changing action MUST append one JSON line to `~/gt/<rig>/.cookie-trail.jsonl`:

```json
{"ts": "<ISO-8601>", "polecat": "<name>", "bead": "<id>", "action": "<verb>", "evidence": "<one-line proof>"}
```

Examples (one per line):

```jsonl
{"ts":"2026-05-21T08:00:00Z","polecat":"rictus","bead":"xl4-ypv","action":"git_commit","evidence":"sha=bb97e1a files=pipeline.py"}
{"ts":"2026-05-21T08:05:30Z","polecat":"rictus","bead":"xl4-ypv","action":"effect_check","evidence":"grep extract_cache_usage pipeline.py -> 1 hit at L4180"}
{"ts":"2026-05-21T08:10:11Z","polecat":"rictus","bead":"xl4-ypv","action":"escalate","evidence":"hq-cf5 filed: spec missing"}
```

Rules:

- **`gt done` is blocked** until the trail has at least one entry per artefact named in the bead's `File scope`.
- **Trail entries are append-only.** Never edit, never delete. If an action is wrong, write a new entry with `action: "revert"` and the corrective evidence.
- **Crew or mayor may grep** the trail at any time to verify a polecat's claims.

## 5. Allowed skill calls (three-tier model)

Polecats may invoke skills via the `Skill` tool. The whitelist is structured as **three tiers** to separate behavioural contract (always-on) from capability (on-demand) from discovery (when stuck).

### Tier 1 — Mandatory contract (must invoke at the trigger conditions)

These are **not optional capabilities** — they are the operating contract. Each maps to a specific trigger; the polecat invokes them automatically when the trigger fires. `~/gt/.policy/check.sh` (§10) verifies the cookie trail shows each was invoked when its trigger fired.

| Skill | Trigger | Why it's mandatory |
|-------|---------|--------------------|
| `superpowers:using-superpowers` | At `gt prime` (first action after waking) | Loads the contract; without this the polecat doesn't know what other skills exist |
| `superpowers:brainstorming` | Before any creative work (new file, new module, design decision) | Forces user-intent exploration before code touches |
| `superpowers:writing-plans` | For any bead with ≥3 steps (most beads) | Written plan ⇒ traceable execution ⇒ reviewable artefact |
| `superpowers:test-driven-development` | When implementing features or bugfixes | Tests first; this session's `verify_polecats` shipping broken proved why |
| `superpowers:systematic-debugging` | When any test fails or any unexpected behaviour appears | Replaces try-stuff loops with hypothesis-driven debugging |
| `superpowers:verification-before-completion` | Before EVERY `gt done`, commit, or push | Block 14 encapsulated; mirrors §3 above. Single most important clause |
| `superpowers:requesting-code-review` | Before any `git push` to a shared branch | External eyes catch what the polecat normalises |
| `superpowers:receiving-code-review` | When mayor / operator returns feedback | Technical rigor, not performative agreement |
| `superpowers:finishing-a-development-branch` | At integration time (`gt done` on the last child of an epic) | Forces explicit merge/PR/cleanup decision |

### Tier 2 — Capabilities (pull on demand for the work at hand)

| Skill | When | Why |
|-------|------|-----|
| `ecc:plan` | Non-trivial edits when `superpowers:writing-plans` needs domain backing | Restate requirements + risk pass with codebase context |
| `ecc:code-review` | After committing local diffs, before `gt done` | Cheap insurance |
| `ecc:python-review` | After any `pipeline.py` or `services/*/app.py` edit | xl4 is Python-heavy |
| `ecc:python-testing` / `ecc:tdd-workflow` | When writing tests | Coverage + table-driven discipline |
| `ecc:simplify` | After review finds duplication | Targeted cleanup |
| `ecc:checkpoint` | At wave boundaries | Verification snapshot |
| `ecc:security-review` | After auth / I/O changes | Pre-push security pass |
| `bedrock-rca-report-format` | If the polecat ever emits a Bedrock RCA markdown | xl4-specific format invariant |

### Tier 3 — Discovery (when capability set is insufficient)

| Skill | When | Action after invoking |
|-------|------|----------------------|
| `ecc:ecc-guide` | Polecat hits a need not covered by Tier 2 | Read the surface; if a skill exists, escalate to operator to add it to Tier 2 |
| `ecc:skill-scout` | Same as above, when the polecat wants programmatic search | Treat output as suggestion only; do NOT auto-invoke un-vetted skills |
| `superpowers:writing-skills` | The right skill doesn't exist yet | Escalate first; only author after operator approval |

### Forbidden (would escalate scope / cost outside the polecat's lane)

| Skill | Why forbidden |
|-------|---------------|
| `ecc:multi-workflow`, `ecc:multi-execute`, `ecc:multi-backend`, `ecc:multi-frontend` | Spawn nested sub-agents — mayor's job, not polecat's |
| `ecc:claude-devfleet`, `ecc:autonomous-agent-harness`, `ecc:continuous-agent-loop` | Same: nested orchestration |
| `ecc:gan-build`, `ecc:gan-design`, `ecc:santa-loop`, `ecc:loop-start` | Iterative loops outside the polecat's bead scope |
| `feature-development-swarm`, `documentation-swarm`, `bug-feedback-loop` | Mayor-level orchestration patterns |
| `superpowers:dispatching-parallel-agents`, `superpowers:subagent-driven-development`, `superpowers:executing-plans` | Mayor/operator dispatches; polecats execute |
| `ecc:save-session`, `ecc:resume-session`, `ecc:learn-eval` | Write to `~/.claude/`, outside the sandbox |
| `update-config`, `keybindings-help`, `ecc:hookify*`, `fewer-permission-prompts` | Mutate global config / hooks |
| `project-bootstrap`, `ecc:project-init` | Town-level mutation |

The whitelist is **additive** — operator can promote a Tier 3 discovery into Tier 2 capability as confidence grows. Tier 1 changes only via operator amendment of this doc.

## 6. Audit checkpoints (self-audit cadence)

The polecat self-audits its state against this policy at every one of these triggers:

1. **`gt prime` end** — confirm the policy was read and the bead is in scope.
2. **Every 10 tool calls** — quick `~/gt/.policy/check.sh --polecat <self>` run. Failed audit ⇒ stop, file BLOCKER, do not continue.
3. **Before any `git push`** — verify branch policy (§2) and cookie trail completeness (§4).
4. **Before `gt done`** — full audit pass; trail must cover every artefact; tests must have run; review skill must have been invoked.
5. **Any 3 consecutive failed remediation attempts on the same symptom** — invoke Block 13 (next clause).

## 7. 3-attempt rule (Block 13, verbatim — non-negotiable)

If a single failure recurs after 3 remediation attempts in the same polecat run, **STOP**. Do not try a 4th variant. File `gt escalate -s high` with:

1. The original failure symptom (exact error / mismatch / test name).
2. The 3 bulleted attempts, each with one-line rationale.
3. Why each attempt failed.
4. Current hypothesis of the real problem.
5. Request to either re-decompose the bead, re-route to a different polecat, or escalate to the operator.

This is **Rule #0** for the inner polecat loop — the analog of the spec-first gate for the outer mayor loop. Both enforce the same principle: *when the plan is failing, stop executing and replan.*

## 8. Escalation guidance

File `gt escalate -s <severity>` immediately when any of these happen:

| Trigger | Severity | Required evidence in the escalation body |
|---------|----------|------------------------------------------|
| Spec doc named by the bead does not exist | high | path + `ls -la` output |
| Base branch missing required source file the bead assumes | high | branch + `git log --oneline -1` + the file path that's missing |
| Push rejected by remote (branch protection, hook, etc.) | high | exact stderr from `git push` |
| Cannot reach external resource (S3, NotebookLM wrapper, etc.) | high | the URL + exit code + stderr |
| Tests broken before any edit | medium | test command + stderr + bisect attempt |
| Spec text is internally contradictory | medium | quoted lines |
| Stuck on Block 13 (3-attempt rule fired) | high | the 5-item template in §7 |

**Never improvise spec.** If something is missing, escalate. The operator/mayor is the only authority that fills spec gaps.

## 9. Forbidden actions (project-wide)

| Action | Rationale |
|--------|-----------|
| Direct push to `main` (any project in this town) | Operator L1 policy 2026-05-21 |
| `git reset --hard` on a branch with unpushed commits | Block 12 / safety; loses work |
| `git push --force` to any branch | Same |
| `gt bd delete <id>` | Beads are append-only history; deletion erases the audit trail |
| `gt down`, `rm -rf ~/gt`, `mv $HOME/go/bin/gt` | Town-level mutation; operator-only |
| `bd close --force` to bypass effect checks | Defeats §3 |
| Disabling the cookie trail (`mv .cookie-trail.jsonl ...`) | Defeats §4 |
| Adding skills to the whitelist by editing this doc | Operator-only (§5) |
| Running `gt sling` against another polecat from inside a polecat | Polecats are workers, not dispatchers |

## 10. Audit enforcement — `~/gt/.policy/check.sh`

The town orchestrator runs a checker at every wave boundary (sling, formula-attach, `gt done`):

```bash
#!/usr/bin/env bash
# ~/gt/.policy/check.sh — runs at wave boundaries
# Inputs:  --polecat <name> | --bead <id> | --wave <n>
# Output:  exit 0 (clean) | exit 1 (HIGH violation; wave blocks)

set -euo pipefail
TRAIL="$HOME/gt/xl4/.cookie-trail.jsonl"
test -s "$TRAIL" || { echo "POLICY HIGH: cookie trail missing or empty"; exit 1; }

# §2 — branch policy
if git -C ~/gt/xl4 log --oneline main..HEAD 2>/dev/null | grep -q .; then
  echo "POLICY HIGH: commits on main detected — branch policy forbids"
  exit 1
fi

# §3 — every `git_commit` action must be followed by an `effect_check` entry
awk -F '"action":"' '/git_commit/ {seen=NR} /effect_check/ {if (NR==seen+1) seen=0} END {if (seen) exit 2}' "$TRAIL" \
  || { echo "POLICY HIGH: git_commit without effect_check at line N"; exit 1; }

# §4 — every bead in IN_PROGRESS must have ≥1 trail entry naming it
for bead in $(~/go/bin/gt bd list --in-progress 2>/dev/null | awk '{print $1}'); do
  grep -q "\"bead\":\"$bead\"" "$TRAIL" \
    || { echo "POLICY HIGH: in-progress bead $bead has no cookie-trail entry"; exit 1; }
done

# §7 — escalation budget (≥1 escalation if any bead has been IN_PROGRESS >2h with no commits)
# ... (left as scaffolding; mayor fills in this check)

echo "POLICY CLEAN"
```

The orchestrator runs `~/gt/.policy/check.sh` and blocks the wave if exit is non-zero. Failed checks generate an automatic `gt escalate -s high` with the check.sh stderr appended.

## 11. Changelog

| Date | Author | Change |
|------|--------|--------|
| 2026-05-21 | postmortem author | Initial draft; v0; awaits operator confirm |
| 2026-05-21 | operator | v0.1 APPROVED — cli-hub-meta-skill integration per [`gastown_worker_policy_amendment_cli_hub_2026-05-21.md`](./gastown_worker_policy_amendment_cli_hub_2026-05-21.md) ("essential for full autonomy"). Mechanical rollout in flight (setup.sh / check.sh / allowlist deployed by bootstrap; base policy §5 Tier 3 + §8 + §9 + §10 references the amendment). |
| 2026-05-21 | postmortem author | v0.2 PROPOSED — bug-hospital integration per [`gastown_worker_policy_amendment_hospital_2026-05-21.md`](./gastown_worker_policy_amendment_hospital_2026-05-21.md). Adds `bug-feedback-loop` as Tier 1 mandatory (prior-art gate + lifecycle gate); `docs/bugs/{pending,resolved,wontfix}/BUG-NNNN-<slug>.md` schema; severity × shape escalation fork. Awaits operator confirm. |
| 2026-05-21 | operator | v0.2 APPROVED — hospital amendment ratified ("hospital yes"). 3 hq-* escalations backfilled as BUG-0001/0002/0003 in xl4 repo. `bug-feedback-loop` skill promoted to Tier 1 mandatory contract. |
| 2026-05-21 | operator | v0.1.1 — cli-hub allowlist set to `trust_registry` mode per operator decision ("everything that has been approved as not dubious by the hub. it is verified and usually has comments and download statistics"). Any name returned by `cli-hub list` is auto-approved for `cli-hub install` without paired escalation. |

---

**Operator decisions this doc raises (before next-session boot):**

1. Confirm the §5 whitelist — add or remove skills.
2. Confirm the §2 branch table — extend to any other rigs that exist or will exist.
3. Confirm §10's `check.sh` scaffold is acceptable, or rewrite the awk parser more strictly.
4. Decide whether failed §10 checks should auto-revert the last `git_commit` or just block (current draft: block only).
