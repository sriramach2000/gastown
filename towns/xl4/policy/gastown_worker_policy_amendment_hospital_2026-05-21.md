# Gastown worker policy — Amendment: bug-hospital integration (2026-05-21)

**Status:** APPROVED 2026-05-21 — "hospital yes".
**Amends:** `gastown_worker_policy_2026-05-21.md` v0 §5 Tier 1+2 + §8 Escalation + §9 Forbidden + §10 check.sh.
**Versioning:** Bumps base policy to **v0.2** once rolled out (post-v0.1 cli-hub amendment).
**Companion:** `gastown_worker_policy_amendment_cli_hub_2026-05-21.md` (Tier 3 discovery primitive).
**Rationale:** Today gastown catches broken features only in **ephemeral** channels (`gt escalate` → `hq-*` wisps, BLOCKER notes inside beads). None persist as a defect catalog with regression-test wire. The existing `bug-feedback-loop` skill provides the JIRA-style hospital pattern; this amendment makes it gastown-native — every polecat reads it at `gt prime`, every broken-feature escalation forks into a permanent BUG-NNNN ticket, every resolved ticket carries a `regression_test:` locator.

---

## H1. The gap, made explicit

Inventory at 2026-05-21 (verified via `find . -name 'BUG-*'` and `ls docs/`):

| Channel | What it is | Lifetime | Bug-log shaped? |
|---|---|---|---|
| `gt escalate -s <sev>` → `hq-<id>` wisps | Short blocker tickets to the mayor | Ephemeral — resolved + age out | No — no field schema, no test wire |
| BLOCKER notes inside beads (`bd --append-notes`) | Free-text appended to a bead's notes | Tied to the bead's lifecycle | No — dies when bead closes |
| Cookie trail (worker-policy §4) | Append-only JSONL of state-changing actions | Forever, but per-action | No — too granular; no defect index |

Net: 3 open escalations from this session (`hq-vmr`, `hq-zuo`, `hq-cf5`) describe real defects but will age out as wisp records. No permanent trace. No regression test pinned to any of them.

## H2. What gastown adopts (skill: bug-feedback-loop)

Direct adoption of the global `bug-feedback-loop` skill, mapped onto the xl4 town:

| Skill primitive | Gastown placement |
|---|---|
| `docs/bugs/{pending,resolved,wontfix}/BUG-NNNN-<slug>.md` with 14-field YAML | Lives in the xl4 repo, version-controlled, branch-policy applies (feat/rca-path-raw only) |
| `docs/bugs/INDEX.md` (component → ticket mapping) | Same; orchestrator regenerates at every wave boundary |
| Bidirectional wire: ticket's `regression_test: <locator>` ↔ test's `bugs_covered=(BUG-NNNN)` | Enforced by `bug_sync check` at every wave boundary |
| Harness drives reopen, not duplicate | On smoke/test failure, polecat MUST `bug_sync reopen` for matching `bugs_covered` before drafting a new ticket |
| Prior-art gate | Polecat at `gt prime` greps `docs/bugs/INDEX.md` for components in its bead's `File scope` and reads each matching ticket's `## Learnings` section |
| Lifecycle rule | Resolved BUG without `regression_test:` locator (except `env-only` label) = `bug_sync check` exit 1 = HIGH violation |

## H3. Tier promotion (additive to base §5 and cli-hub amendment §A2)

| Tier | Item | Trigger | Action constraint |
|------|------|---------|--------------------|
| 1 (mandatory) | `bug-feedback-loop` prior-art gate | At `gt prime` if bead's `File scope` overlaps any component listed in `docs/bugs/INDEX.md` | Read all matching `BUG-NNNN-*.md` `## Learnings` sections; cookie-trail `action="bug_prior_art_read"` with the list of BUG IDs reviewed |
| 1 (mandatory) | `bug-feedback-loop` lifecycle gate | Before EVERY `gt done` | `bug_sync check` exit 0; otherwise stop, fix the orphan, re-check |
| 2 (capability) | `bug-feedback-loop` ticket-draft helper | When the polecat observes broken behavior not in INDEX.md | Use the skill's `templates/bug-ticket.md` to draft `docs/bugs/pending/BUG-NNNN-<slug>.md` |

**Tier 1 placement is mandatory** because the prior-art gate prevents re-introducing fixed bugs and the lifecycle gate prevents the orphan-ticket class entirely.

## H4. Escalation fork (modifies base §8)

`gt escalate` now branches by severity AND defect-shape:

| Severity | Shape | Action |
|---|---|---|
| `high` | Blocker (env, spec, branch protection, infra) | Old behavior — file `hq-<id>` wisp only. Aging out is fine. |
| `high` | Broken feature behavior (regression, incorrect output) | File BOTH `hq-<id>` wisp AND draft `docs/bugs/pending/BUG-NNNN-<slug>.md`. Wisp `attached_metadata.bug_id = BUG-NNNN`. |
| `medium` / `low` | Broken feature behavior | Draft `docs/bugs/pending/BUG-NNNN-<slug>.md` only. Hq-wisp optional. |
| `medium` / `low` | Workflow / process | Old behavior — wisp only. |

Decision rule for "broken feature behavior" (from skill, paraphrased): the symptom names a *file path*, a *function name*, a *user-visible misbehavior*, or a *specific test expected-vs-actual* — NOT an environment / config / spec / branch issue.

## H5. Locator grammar (verbatim from `bug-feedback-loop` skill)

```
locator := scheme "://" target
scheme  := smoke | pytest | jest | gotest | cargo | rspec | playwright
target  := framework-specific path
```

xl4 examples (project is Python-heavy):

- `pytest://tests/test_pipeline.py::TestBedrockInvoke::test_cache_block_path`
- `pytest://tests/test_l0_pipeline.py::TestSIG001::test_hits_8813_corpus`
- `smoke://S-04` (when smoke harness lands)

## H6. Forbidden patterns (additive to base §9 + cli-hub §A6)

| Action | Why forbidden |
|--------|---------------|
| Closing a `BUG-NNNN-*.md` to `resolved/` without a `regression_test:` locator (or `env-only` label) | Lifecycle rule; defeats the bidirectional wire |
| Drafting `BUG-NNNN-*.md` when a matching `pending/` or `resolved/` ticket already exists for the same symptom | Duplicates clutter the INDEX; use `bug_sync reopen` instead |
| Moving a ticket from `pending/` to `resolved/` without naming the fix commit SHA in the ticket | Destroys traceability — closes the loop on what fix landed where |
| Editing a `resolved/` ticket's frontmatter (other than appending a new `regression_test:` row) | Resolved tickets are append-only; same spirit as cookie trail |
| Renaming a ticket file after creation (changing the BUG-NNNN slug or number) | Breaks `bugs_covered=(...)` references in tests and INDEX entries |
| Skipping the prior-art gate at `gt prime` | Re-introduces fixed bugs; defeats the cumulative-learning purpose of the catalog |

## H7. `~/gt/.policy/check.sh` additions

Append to the existing checker (base §10 + cli-hub §A7):

```bash
# §H7 — bug-catalog hygiene
BUGS_DIR="$XL4_REPO_PATH/docs/bugs"
if [[ -d "$BUGS_DIR" ]]; then
  # Every resolved ticket must have a regression_test (or env-only label).
  for f in "$BUGS_DIR/resolved/"BUG-*.md; do
    [[ -f "$f" ]] || continue
    if ! grep -qE '^regression_test:' "$f" && ! grep -qE '^labels:.*env-only' "$f"; then
      echo "POLICY HIGH: $f is resolved with no regression_test and no env-only label"
      exit 1
    fi
  done

  # Every regression_test locator must point to a test that exists.
  while IFS= read -r locator; do
    case "$locator" in
      pytest://*)
        path="${locator#pytest://}"
        file="${path%%::*}"
        test -f "$XL4_REPO_PATH/$file" \
          || { echo "POLICY HIGH: regression_test locator $locator -> missing file $file"; exit 1; }
        ;;
      smoke://*) ;;  # smoke harness validates these separately
    esac
  done < <(grep -hE '^regression_test:' "$BUGS_DIR/resolved/"BUG-*.md 2>/dev/null | awk '{print $2}')

  # No prefix collision: any BUG-NNNN appears in at most one of pending/resolved/wontfix.
  python3 - <<'PY' "$BUGS_DIR"
import sys, pathlib, re, collections
root = pathlib.Path(sys.argv[1])
ids = collections.defaultdict(list)
for sub in ("pending", "resolved", "wontfix"):
    for f in (root / sub).glob("BUG-*.md") if (root / sub).is_dir() else []:
        m = re.match(r"(BUG-\d+)-", f.name)
        if m: ids[m.group(1)].append(str(f))
dupes = {k: v for k, v in ids.items() if len(v) > 1}
if dupes:
    print(f"POLICY HIGH: BUG-NNNN appears in multiple folders: {dupes}")
    sys.exit(1)
PY
fi
```

## H8. Bootstrap actions (additive to base §11 + cli-hub §A12)

- [ ] Create `docs/bugs/{pending,resolved,wontfix}/` directories (gitkeep'd if empty).
- [ ] Create `docs/bugs/INDEX.md` with header + component-to-ticket table (empty rows at v0).
- [ ] Backfill the 3 open escalations as pending BUG tickets (operator decides which deserve permanent tracking):
   - `hq-vmr` (rictus: sling-hook-empty + spec gap) → BUG-0001 candidate
   - `hq-zuo` (push to main rejected by branch protection) → moot under new policy; close as wontfix with rationale
   - `hq-cf5` (cf5: empty-hook silent exit, gastown Bug 3 suspected) → BUG-0002 candidate
- [ ] Patch `~/gt/.policy/check.sh` with §H7 block.
- [ ] Patch `setup.sh` to deploy `docs/bugs/` skeleton if missing (idempotent).
- [ ] Append v0.2 row to base-policy §11 changelog.
- [ ] Promote `bug-feedback-loop` to Tier 1 in base §5 once mechanical rollout completes.

## H9. Mapping to base-policy clauses

| Base clause | Amendment touches | How |
|---|---|---|
| §3 Verify-effect | §H7 | `bug_sync check` exit 0 is the effect-check before `gt done` |
| §4 Cookie-trail | §H3 Tier 1 row 1 | New action verb `bug_prior_art_read` |
| §5 Tier 1/2/3 | §H3 | Adds two Tier 1 mandates (prior-art + lifecycle) |
| §7 3-attempt rule | §H4 | After 3 failed `bug_sync reopen` attempts, escalate not retry |
| §8 Escalation | §H4 | New escalation fork by severity × shape |
| §9 Forbidden | §H6 | 6 new forbidden patterns |
| §10 check.sh | §H7 | New audit clause for bug-catalog hygiene |
| §11 Changelog | this doc | Becomes v0.2 entry once ratified |

## H10. Anti-patterns this prevents (concrete examples)

| Anti-pattern | Why it would have hurt | This amendment prevents it via |
|---|---|---|
| Re-implementing the FAISS path in services/log_analyzer_app/ because nobody read the `L3` decision history | Lost 2-3 days of agent work | Prior-art gate (§H3 Tier 1 row 1) surfaces any prior BUG ticket tagged with the `log_analyzer_app` component |
| Closing the bedrock-pricing single-source-of-truth bug without a regression test | Same bug re-introduced two waves later | Lifecycle rule (§H7) blocks the `gt done` |
| Three workers each drafting a "ClaudeAdapter rebuild_request crashes" ticket independently | INDEX clutter; reviewer confusion | Duplicate-draft forbidden (§H6 row 2); `bug_sync reopen` is the canonical action |
| Resolved ticket frontmatter edited to silently flip `regression_test:` to a passing test that doesn't actually cover the bug | Defeats audit; lifecycle rule passes but coverage is fake | Append-only resolved tickets (§H6 row 4) + `regression_test`-locator file-existence check (§H7) |

## H11. Cost considerations

- Bug-catalog hygiene runs at `gt done` and at wave boundaries — at most 4-6 checks per polecat run. Each check is shell-level + small Python; sub-second.
- Tier 1 prior-art read at `gt prime` is one grep + read of 0–5 ticket files; sub-second.
- `bug_sync reopen` saves the round-trip cost of writing-then-discovering-a-duplicate (single-digit minutes of agent time per occurrence; significant given today's $187/session ceiling).

Net: this amendment is **cheaper to enforce than to skip**.

## H12. Rollout checklist (post-approval)

- [ ] Append v0.2 row to base policy §11 changelog: `2026-05-21 | <operator> | v0.2: bug-feedback-loop integrated per hospital amendment doc`.
- [ ] Patch base policy §5 Tier 1 to add `bug-feedback-loop` skill rows (prior-art + lifecycle).
- [ ] Patch base policy §8 to add severity × shape escalation fork.
- [ ] Patch base policy §9 to add §H6 forbidden patterns.
- [ ] Patch base policy §10 `check.sh` template with §H7 block.
- [ ] Create `docs/bugs/{pending,resolved,wontfix}/` + `docs/bugs/INDEX.md` skeleton in feat/rca-path-raw.
- [ ] Decide backfill action for the 3 open hq-* escalations (`hq-vmr`, `hq-zuo`, `hq-cf5`).
- [ ] Patch `towns/xl4/bootstrap/setup.sh` (in this fork) to deploy the skeleton on next idempotent run.
- [ ] Link this amendment from `~/gt/xl4/polecats/AGENTS.md`.
- [ ] Link this amendment from `docs/design/gastown_session_handoff_2026-05-21.md` next-session resume pointer.

## H13. Changelog (this amendment)

| Date | Author | Change |
|------|--------|--------|
| 2026-05-21 | postmortem author | Initial draft; v0.2; awaits operator confirm |
| 2026-05-21 | operator | APPROVED — "hospital yes"; promoted to Tier 1 mandatory in base policy v0.2. Status: APPROVED. |

---

**Operator decisions this amendment raises:**

1. Ratify the Tier 1 placement (§H3) — promoting `bug-feedback-loop` to mandatory.
2. Decide the backfill action for the 3 open `hq-*` escalations (§H8 row 3) — which deserve permanent BUG-NNNN tickets?
3. Confirm the §H4 severity × shape fork — or simplify (always create a BUG ticket on any escalation regardless of severity?).
4. Confirm §H7 `check.sh` additions are acceptable, or simplify (drop the prefix-collision check?).
5. Decide whether failed §H7 checks should auto-revert the polecat's last `git_commit` when a lifecycle violation is detected, or just block the wave (current draft: block only, matching base §10 + cli-hub §A7).
