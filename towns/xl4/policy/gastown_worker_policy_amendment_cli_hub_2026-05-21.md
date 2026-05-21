# Gastown worker policy — Amendment: cli-hub integration (2026-05-21)

**Status:** APPROVED by operator 2026-05-21 — "essential for full autonomy". Pending mechanical rollout (base-policy §11 changelog row + check.sh patch + allowlist file).
**Amends:** `gastown_worker_policy_2026-05-21.md` v0 §5 Tier 3 + §8 Escalation + §9 Forbidden + §10 check.sh.
**Versioning:** Bumps base policy to **v0.1** once rolled out.
**Rationale:** CLI-Hub (`https://clianything.cc`) is an agent-native CLI marketplace. A polecat that needs a capability outside the pre-installed Tier 2 set should be able to *discover, install, and use* a CLI at runtime, with the same Block 14 / cookie-trail discipline that governs every other state-changing action. This amendment scopes that loop so polecats don't turn into runaway package installers.

---

## A1. What cli-hub is

A lightweight `pip` wrapper that resolves names from a live registry and installs separate `cli-anything-<name>` packages. Each installed CLI is agent-native: ships with `--json` output, REPL mode, and stateful subcommands.

```bash
pip install cli-anything-hub          # one-time, town-level
cli-hub list                          # browse registry
cli-hub search <keyword>              # find by name / category
cli-hub info <name>                   # show details before install
cli-hub install <name>                # pip-installs cli-anything-<name>
cli-anything-<name> --json <subcmd>   # invoke
```

Live catalog: `https://reeceyang.sgp1.cdn.digitaloceanspaces.com/SKILL.md` (auto-updated).
Repo: `https://github.com/HKUDS/CLI-Anything`.

## A2. Tier placement

cli-hub-meta-skill is a **Tier 3 Discovery** capability. It can promote individual installed CLIs to **Tier 2.5 (pre-approved, town-level pre-baked)** once the operator approves an allowlist entry.

| Tier | Item | Trigger | Action constraint |
|------|------|---------|--------------------|
| 3 (discovery) | `cli-hub-meta-skill` — `cli-hub search` / `cli-hub info` | Polecat's bead needs a capability not covered by Tier 1+2 and no `ecc:*` skill matches | Read-only allowed without escalation. **`cli-hub install`** requires an escalation entry (§A5) **unless** the target name is on the §A3 pre-approved allowlist. |
| 2.5 (pre-baked) | `cli-anything-<name>` (allowlisted, pre-installed in town image) | Bead spec explicitly references the tool, OR polecat invokes after an allowlist hit during search | Use directly with `--json`; cookie-trail every invocation per §4 of base policy. |

The new tier preserves the spirit of base §5: **whitelist is additive; operator-ratified; never self-amended**.

## A3. Pre-approved allowlist (seed v0)

Empty at v0 — every `cli-hub install` requires an escalation. Operator populates this list as confidence in specific tools grows. Format:

```yaml
# ~/gt/.policy/cli_hub_allowlist.yaml
allowlisted:
  # - name: <cli-anything-name>
  #   approved_by: <operator>
  #   approved_on: <YYYY-MM-DD>
  #   rationale: <one line; what bead class needs it>
  #   max_invocations_per_polecat_run: <int>  # optional cap
```

Operator-only writes. Polecat reads. `~/gt/.policy/check.sh` (§A7) parses this file; an install of a non-allowlisted CLI without a paired escalation is a HIGH violation.

## A4. Discovery → install → use loop (canonical sequence)

```
                ┌─────────────────────────────────────────────────────────────┐
                │ Polecat hits a capability gap (Tier 1+2 do not cover the    │
                │ work-item).                                                  │
                └────────────────────────────┬────────────────────────────────┘
                                             ▼
                ┌─────────────────────────────────────────────────────────────┐
                │ 1. `cli-hub search <keyword>` — read-only discovery.        │
                │    Cookie-trail entry: action="cli_hub_search".             │
                └────────────────────────────┬────────────────────────────────┘
                                             ▼
                ┌─────────────────────────────────────────────────────────────┐
                │ 2. `cli-hub info <name>` for top candidates — read-only.    │
                │    Cookie-trail entry: action="cli_hub_info".               │
                └────────────────────────────┬────────────────────────────────┘
                                             ▼
                ┌─────────────────────────────────────────────────────────────┐
                │ 3. Branch on allowlist:                                     │
                │    a) name IS on allowlist  → install + use (skip step 4)   │
                │    b) name NOT on allowlist → escalate per §A5; STOP here   │
                └────────────────────────────┬────────────────────────────────┘
                                             ▼
                ┌─────────────────────────────────────────────────────────────┐
                │ 4. `cli-hub install <name>` (only after operator approval). │
                │    Cookie-trail entry: action="cli_hub_install"             │
                │                       evidence="ratified by hq-<id>".       │
                │    Effect check: `cli-anything-<name> --version` succeeds.  │
                └────────────────────────────┬────────────────────────────────┘
                                             ▼
                ┌─────────────────────────────────────────────────────────────┐
                │ 5. Invoke. ALWAYS `--json`. Parse output; do not regex      │
                │    human-readable text. Cookie-trail per invocation.        │
                └─────────────────────────────────────────────────────────────┘
```

## A5. Escalation for un-allowlisted installs

When step 3b above fires, the polecat files:

```
gt escalate -s medium --title "cli-hub install request: <name>" --body-file - <<EOF
Bead: <id>
Capability gap: <one line>
Searched: <keywords tried in cli-hub search>
Considered: <other candidate names from search>
Chose: cli-anything-<name>
Source: https://github.com/HKUDS/CLI-Anything (registry: https://reeceyang.sgp1.cdn.digitaloceanspaces.com/SKILL.md)
Operator action needed: append to ~/gt/.policy/cli_hub_allowlist.yaml then ack here
EOF
```

Then **STOP**. Do not install. Do not work around the gap with an unrelated tool. Bead returns to `BLOCKED` until operator acks.

This is the same shape as the bead-spec escalation in §8 of the base policy: capability gap = spec gap = operator-only decision.

## A6. Forbidden patterns (additive to base §9)

| Action | Why forbidden |
|--------|---------------|
| `pip install <random package>` to bypass cli-hub | Escapes the allowlist; defeats audit |
| `cli-hub install` without an escalation OR allowlist hit | Defeats §A3/§A5 |
| Invoking `cli-anything-<name>` without `--json` (when --json is available) | Human-text parsing is brittle; defeats Block 14 verify-effect |
| Installing into the polecat's local venv vs town-level shared venv | Re-install on every polecat run; wastes time + network |
| `cli-hub install` of a CLI with system-level side effects (filesystem-mount, daemon, root-required) without explicit operator note in the allowlist `rationale` | Polecats are sandboxed; surprise daemons leak outside |

## A7. `~/gt/.policy/check.sh` additions

Append to the existing checker (base §10):

```bash
# §A3 — every cli_hub_install entry must be allowlisted OR preceded by an escalation
ALLOW="$HOME/gt/.policy/cli_hub_allowlist.yaml"
test -s "$ALLOW" || ALLOW="/dev/null"

python3 - <<'PY' "$TRAIL" "$ALLOW"
import json, sys, yaml, pathlib
trail = pathlib.Path(sys.argv[1]).read_text().splitlines()
allow = yaml.safe_load(pathlib.Path(sys.argv[2]).read_text() or "") or {}
allowed = {x["name"] for x in (allow.get("allowlisted") or [])}

installs = []
for i, line in enumerate(trail):
    try:
        rec = json.loads(line)
    except json.JSONDecodeError:
        continue
    if rec.get("action") == "cli_hub_install":
        installs.append((i, rec))

for i, rec in installs:
    name = rec.get("evidence", "").split("name=", 1)[-1].split()[0]
    if name in allowed:
        continue
    # Require an escalation entry within the prior 20 trail lines naming the same CLI
    prior = trail[max(0, i - 20):i]
    if not any('"action":"escalate"' in p and name in p for p in prior):
        print(f"POLICY HIGH: cli_hub_install of {name!r} without allowlist or escalation")
        sys.exit(1)
PY
```

## A8. Town-level pre-bake (operator policy, not polecat)

Once a `cli-anything-<name>` is on the allowlist, the operator decides whether to:

- **Lazy**: leave it as `cli-hub install` on first polecat use (slower first-use, smaller image).
- **Pre-bake**: add it to the town's startup `pip install` list (faster, larger image, network-independent).

Default = lazy. Pre-bake any CLI invoked by 3+ beads or by any wave-level orchestration. This decision lives in `~/gt/xl4/AGENTS.md` `## Pre-baked CLIs` section, not in this amendment.

## A9. Budget caps (anti-runaway)

Per polecat run:

| Cap | Default | Override |
|-----|---------|----------|
| `cli-hub search` invocations | 10 | Operator amendment |
| `cli-hub info` invocations | 10 | Operator amendment |
| `cli-hub install` invocations | 0 (unless allowlist hit) | Per-tool entry in allowlist |
| `cli-anything-<name>` invocations | per-tool `max_invocations_per_polecat_run` in allowlist; default unlimited if unspecified | Per-tool override |

When the cap is hit, polecat MUST escalate per §A5 with subject "cli-hub budget exhausted" and STOP. Wave does not advance.

## A10. What this enables (motivating examples, non-binding)

These are *plausible* polecat→cli-hub flows, illustrating the loop. Each requires a real bead and operator approval before any install.

| Bead-class example | Candidate CLI | Why cli-hub > alternative |
|---|---|---|
| "Generate architecture diagram of services/log_analyzer_app" | a diagramming CLI from the registry | Polecat doesn't need to learn Mermaid syntax + lay out manually |
| "Convert a Bedrock cost CSV to a chart-ready JSON" | a small data-viz CLI | Cheaper than spawning a sub-agent for the conversion |
| "Render an audit report as PDF" | a doc-conversion CLI | Avoids handcrafting weasyprint config inside the bead |
| "Inspect a `pipeline.py` traceback's GPU CUDA tensor shapes" | a deep-learning debug CLI | Avoids re-implementing CUDA introspection in shell |

Note: **none of these are pre-approved.** They illustrate the shape of an escalation. The operator decides each one.

## A11. Mapping to base-policy clauses

| Base clause | Amendment touches | How |
|---|---|---|
| §3 Verify-effect | §A4 step 4 | `--version` is the effect-check for `cli-hub install` |
| §4 Cookie-trail | §A4 steps 1, 2, 4, 5 | Three new `action` verbs |
| §5 Tier 1/2/3 | §A2 | Adds Tier 2.5 (pre-baked allowlist) |
| §7 3-attempt rule | §A4 + §A9 | After 3 failed installs or 3 failed `--json` parses, escalate not retry |
| §8 Escalation | §A5 | New trigger: "capability gap, want cli-hub install" |
| §9 Forbidden | §A6 | 5 new forbidden patterns |
| §10 check.sh | §A7 | New audit clause for installs vs allowlist |
| §11 Changelog | this doc | Becomes v0.1 entry once mechanical rollout completes |

## A12. Rollout checklist (post-approval)

- [ ] Append v0.1 row to base policy §11 changelog: `2026-05-21 | <operator> | v0.1: cli-hub-meta-skill integrated per amendment doc`.
- [ ] Patch base policy §5 Tier 3 table to reference this amendment doc.
- [ ] Create empty `~/gt/.policy/cli_hub_allowlist.yaml` with header comment.
- [ ] Patch `~/gt/.policy/check.sh` with §A7 block.
- [ ] Add cli-hub bootstrap to `towns/xl4/bootstrap/setup.sh` (in this fork): `pip install cli-anything-hub` (town-shared venv).
- [ ] Link this amendment from `~/gt/xl4/polecats/AGENTS.md`.
- [ ] Link this amendment from `docs/design/gastown_session_handoff_2026-05-21.md` next-session resume pointer.

## A13. Changelog (this amendment)

| Date | Author | Change |
|------|--------|--------|
| 2026-05-21 | postmortem author | Initial draft; v0.1 |
| 2026-05-21 | operator | APPROVED — "essential for full autonomy" |
| 2026-05-21 | operator | v0.1.1 — allowlist policy set to `trust_registry` mode: any name returned by `cli-hub list` is auto-approved (registry vetting is the gate). Operator rationale: "everything that has been approved as not dubious by the hub. it is verified and usually has comments and download statistics". |

---

**Operator decisions remaining (smaller scope post-approval):**

1. Confirm v0 allowlist starts empty (§A3) — or seed with N pre-approved tools now.
2. Pick lazy-install vs pre-bake default (§A8) — current draft: **lazy**.
3. Confirm the §A9 budget caps — current draft is conservative (10/10/0/per-tool).
4. Decide whether `check.sh` (§A7) should auto-revert the polecat's last `git_commit` when an unauthorized install is detected, or just block the wave (current draft: block only, matching base §10).
