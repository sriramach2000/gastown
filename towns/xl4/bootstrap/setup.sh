#!/usr/bin/env bash
# setup.sh — bootstrap gastown + xl4 rig + Part-A/Part-B beads/convoys
#
# Idempotent. Re-running skips completed steps via marker files in
# $STATE_DIR. Edits to the config block below take effect on next run
# only if you also rm the relevant marker.
#
# Reference: docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md in the
# xl4 project. Five polecats across two convoys (Part A on main, Part B
# on feat/rca-path-raw), wave-1 slung automatically.

set -euo pipefail

# ── Config ───────────────────────────────────────────────────────────────────
XL4_REPO_PATH="${XL4_REPO_PATH:-/home/sriram-acharya/dev/sriram/ExcelforeProjects/fluent-d/xl4_client_log_fluentd_processor_clean}"
XL4_REMOTE_URL="${XL4_REMOTE_URL:-git@gitlab.excelfore.com:gurudath.bn/xl4_client_log_fluentd_processor.git}"
GT_FORK_URL="${GT_FORK_URL:-git@github.com:sriramach2000/gastown.git}"
GASTOWN_SRC="${GASTOWN_SRC:-$HOME/dev/sriram/PersonalProjects/gastown}"
GT_HOME="${GT_HOME:-$HOME/gt}"
GT_TOWN_ROOT="${GT_TOWN_ROOT:-$GT_HOME}"
RIG_NAME="${RIG_NAME:-xl4}"
RIG_PREFIX="${RIG_PREFIX:-xl4}"
TOWN_NAME="${TOWN_NAME:-xl4-workspace}"
CREW_NAME="${CREW_NAME:-$(whoami)}"
PARTA_BASE="${PARTA_BASE:-main}"
PARTB_BASE="${PARTB_BASE:-feat/rca-path-raw}"
PARTA_INTEG_BRANCH="${PARTA_INTEG_BRANCH:-integration/log-analyzer-app}"
PARTB_INTEG_BRANCH="${PARTB_INTEG_BRANCH:-integration/single-batch-cache-pivot}"
AGENT_RUNTIME="${AGENT_RUNTIME:-claude}"   # claude | codex | copilot
STATE_DIR="${STATE_DIR:-$GT_HOME/.xl4-bootstrap}"
BEADS_ENV="$STATE_DIR/beads.env"

# ── Helpers ──────────────────────────────────────────────────────────────────
log()  { printf '\033[1;36m[setup]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn ]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[fatal]\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing prereq: $1 ($2)"; }
marker_done() { [[ -f "$STATE_DIR/$1.done" ]]; }
marker_set()  { mkdir -p "$STATE_DIR"; touch "$STATE_DIR/$1.done"; }

# ── Dolt auto-install ────────────────────────────────────────────────────────
# Downloads the official prebuilt tarball from dolthub/dolt and installs the
# binary to /usr/local/bin via `sudo install`. No curl|bash. Skipped if dolt
# is already on PATH. Set AUTO_INSTALL_DOLT=0 to disable and require manual.
ensure_dolt() {
  if command -v dolt >/dev/null 2>&1; then return; fi
  if [[ "${AUTO_INSTALL_DOLT:-1}" != "1" ]]; then
    die "dolt missing and AUTO_INSTALL_DOLT=0 — install manually (see dolthub/dolt)"
  fi
  local arch tarball tmp
  case "$(uname -m)" in
    x86_64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) die "dolt auto-install only supports x86_64/aarch64 (saw $(uname -m)) — install manually" ;;
  esac
  need curl "apt install curl"
  need sudo "needed to install dolt to /usr/local/bin"
  tarball="dolt-linux-${arch}.tar.gz"
  tmp="$(mktemp -d)"
  log "Installing dolt (linux-${arch}) from dolthub/dolt latest release"
  log "  download into $tmp; will sudo-install to /usr/local/bin/dolt"
  ( cd "$tmp" \
    && curl -fSL -o "$tarball" \
         "https://github.com/dolthub/dolt/releases/latest/download/$tarball" \
    && tar -xzf "$tarball" \
    && sudo install -m 0755 "dolt-linux-${arch}/bin/dolt" /usr/local/bin/dolt )
  rm -rf "$tmp"
  command -v dolt >/dev/null 2>&1 || die "dolt install reported success but binary not on PATH"
  log "  dolt installed: $(dolt version 2>&1 | head -1)"
}

# ── Prereq check ─────────────────────────────────────────────────────────────
check_prereqs() {
  log "Checking prereqs"
  need git "any package manager"
  need go  "https://go.dev/dl"
  ensure_dolt
  need tmux "apt install tmux"
  git -C "$XL4_REPO_PATH" rev-parse --git-dir >/dev/null 2>&1 \
    || die "xl4 repo not found / not a git working tree at $XL4_REPO_PATH"
  git -C "$GASTOWN_SRC"  rev-parse --git-dir >/dev/null 2>&1 \
    || die "gastown clone not found / not a git working tree at $GASTOWN_SRC"
  case "$AGENT_RUNTIME" in
    claude)  need claude  "https://claude.ai/code" ;;
    codex)   need codex   "https://developers.openai.com/codex/cli" ;;
    copilot) need copilot "https://cli.github.com" ;;
    *) die "unknown AGENT_RUNTIME: $AGENT_RUNTIME" ;;
  esac
  mkdir -p "$STATE_DIR"
}

# ── Step 1: build gastown from fork ──────────────────────────────────────────
build_gastown() {
  if marker_done build; then log "build: skipped (marker present)"; return; fi
  log "Building gastown from $GT_FORK_URL"
  cd "$GASTOWN_SRC"
  if ! git remote -v | grep -q "$GT_FORK_URL"; then
    log "  pointing origin at fork"
    git remote set-url origin "$GT_FORK_URL"
  fi
  git fetch origin
  git pull --ff-only origin "$(git rev-parse --abbrev-ref HEAD)" || warn "  pull skipped (possibly dirty tree)"
  make build
  mkdir -p "$HOME/go/bin"
  mv -f gt "$HOME/go/bin/"
  command -v gt >/dev/null 2>&1 || die "gt not on PATH — add \$HOME/go/bin to PATH"
  gt --version || true
  if ! command -v bd >/dev/null 2>&1; then
    log "  installing beads (bd) from source"
    go install github.com/steveyegge/beads/cmd/bd@latest
  fi
  marker_set build
}

# ── Step 2: install Gas Town HQ ──────────────────────────────────────────────
# `gt install <path>` creates mayor/town.json + .beads/ + CLAUDE.md, which is
# what makes a directory discoverable as a Gas Town workspace. Self-healing:
# if the marker is set but mayor/town.json is missing (e.g. from an earlier
# broken version of this function), re-runs install.
init_town() {
  if marker_done town; then
    if [[ -f "$GT_HOME/mayor/town.json" ]]; then
      log "town install: skipped (mayor/town.json verified)"
      return
    fi
    warn "  town marker present but mayor/town.json missing — re-installing"
  fi
  log "Installing Gas Town HQ at $GT_HOME"
  mkdir -p "$GT_HOME"
  if [[ -f "$GT_HOME/mayor/town.json" ]]; then
    log "  mayor/town.json already exists, skipping gt install"
  else
    gt install "$GT_HOME" --name "$TOWN_NAME"
  fi
  marker_set town
}

# ── Step 3: add xl4 rig ──────────────────────────────────────────────────────
add_rig() {
  if marker_done rig; then log "rig add: skipped"; return; fi
  log "Adding rig '$RIG_NAME' wrapping $XL4_REPO_PATH"
  if gt rig list 2>/dev/null | grep -qE "^\s*$RIG_NAME(\s|$)"; then
    log "  rig $RIG_NAME already present"
  else
    gt rig add "$RIG_NAME" "$XL4_REMOTE_URL" \
      --local-repo "$XL4_REPO_PATH" \
      --prefix "$RIG_PREFIX" \
      --branch "$PARTA_BASE"
  fi
  marker_set rig
}

# ── Step 3b: add crew member ─────────────────────────────────────────────────
# Without a crew member anchored in the rig, the Mayor's dispatch scheduler
# does not reliably allocate polecats to slung wisps. Skip if already present.
ensure_crew() {
  if marker_done crew; then log "crew add: skipped"; return; fi
  log "Adding crew member '$CREW_NAME' to rig '$RIG_NAME' (re-add errors are non-fatal — idempotency is via the marker)"
  # gt crew list's output format is unreliable for grepping (no rig/name slash
  # form, tabular columns). Simpler + safer: just try `gt crew add` and let it
  # complain if the crew already exists — that's not a real error for us.
  gt crew add "$CREW_NAME" --rig "$RIG_NAME" 2>&1 | sed 's/^/  /' || true
  marker_set crew
}

# ── Step 3c: install cli-anything-hub (Tier 3 discovery primitive) ───────────
# Per gastown_worker_policy_amendment_cli_hub_2026-05-21.md (operator-APPROVED
# 2026-05-21 — "essential for full autonomy"): cli-hub is the discovery channel
# for capabilities outside the Tier 1+2 skill whitelist. Polecats invoke
# `cli-hub search/info` read-only, then escalate per §A5 to get an entry on the
# allowlist before `cli-hub install`.
install_cli_hub() {
  if marker_done cli_hub; then log "cli-hub install: skipped"; return; fi
  log "Installing cli-anything-hub (town-shared venv)"
  if command -v cli-hub >/dev/null 2>&1; then
    log "  cli-hub already on PATH: $(cli-hub --version 2>&1 | head -1 || echo unknown)"
  else
    need pip "apt install python3-pip"
    pip install --user cli-anything-hub 2>&1 | tail -3 | sed 's/^/  /' || \
      warn "  pip install reported non-zero; check manually"
    # Effect-check per worker-policy §3 (verify state change, not exit code)
    command -v cli-hub >/dev/null 2>&1 \
      || { warn "  cli-hub install reported success but binary not on PATH — check ~/.local/bin"; return; }
  fi
  marker_set cli_hub
}

# ── Step 3d: deploy town policy scaffold (~/gt/.policy/) ─────────────────────
# Creates the policy directory + check.sh scaffold + empty cli-hub allowlist
# YAML. Idempotent: skips when files already exist. The check.sh scaffold
# comes from gastown_worker_policy_2026-05-21.md §10 + amendment §A7.
# DO NOT run when polecats are live — overwriting check.sh mid-wave could
# break a running audit. Function detects live polecats and skips with a warn.
deploy_policy() {
  if marker_done policy; then log "town policy: skipped"; return; fi
  local policy_dir="$GT_HOME/.policy"
  # Block deploy if polecats are alive (safety; per session 2026-05-21 lesson)
  if pgrep -af 'claude.*GAS TOWN.*polecat' >/dev/null 2>&1; then
    warn "  live polecats detected — skipping policy deploy (operator quiesce + re-run)"
    return
  fi
  mkdir -p "$policy_dir"
  if [[ ! -f "$policy_dir/cli_hub_allowlist.yaml" ]]; then
    log "  creating $policy_dir/cli_hub_allowlist.yaml (empty v0)"
    cat >"$policy_dir/cli_hub_allowlist.yaml" <<'YAML'
# Town-level cli-hub allowlist — operator-managed.
# Per gastown_worker_policy_amendment_cli_hub_2026-05-21.md §A3 + §A13 v0.1.1
# (operator 2026-05-21: trust_registry mode).
#
# policy.mode: trust_registry
#   The cli-hub registry itself vets entries (verified status + comments +
#   download statistics). Any name returned by `cli-hub list` is auto-approved
#   for `cli-hub install` — no per-tool escalation required.
#   check.sh §A7 honors this mode and skips the name-allowlist verification.
#   Cookie-trail still records every install (audit retained).
#
# To revert to per-tool explicit allowlist:
#   1. Remove policy.mode line
#   2. Populate `allowlisted` with explicit entries (see format below).
policy:
  mode: trust_registry
  registry_url: https://reeceyang.sgp1.cdn.digitaloceanspaces.com/SKILL.md
  rationale: |
    operator 2026-05-21 — "everything that has been approved as not dubious by
    the hub. it is verified and usually has comments and download statistics"
# Per-tool explicit entries (only consulted when policy.mode != trust_registry):
# Format:
#   - name: <cli-anything-name>             # e.g. cli-anything-gimp
#     approved_by: <operator>
#     approved_on: <YYYY-MM-DD>
#     rationale: <one-line: what bead class needs it>
#     max_invocations_per_polecat_run: <int>  # optional cap
allowlisted: []
# Explicit denylist — overrides allowlist even under trust_registry mode.
denylist: []
YAML
  fi
  if [[ ! -f "$policy_dir/check.sh" ]]; then
    log "  creating $policy_dir/check.sh (v0 scaffold; refine per actual waves)"
    cat >"$policy_dir/check.sh" <<'BASH'
#!/usr/bin/env bash
# ~/gt/.policy/check.sh — runs at wave boundaries.
# Per gastown_worker_policy_2026-05-21.md §10 + cli-hub amendment §A7.
# Inputs: --polecat <name> | --bead <id> | --wave <n>
# Output: exit 0 (clean) | exit 1 (HIGH violation; wave blocks)
set -euo pipefail
XL4_REPO_PATH="${XL4_REPO_PATH:-$HOME/dev/sriram/ExcelforeProjects/fluent-d/xl4_client_log_fluentd_processor_clean}"
TRAIL="$HOME/gt/xl4/.cookie-trail.jsonl"
test -s "$TRAIL" || { echo "POLICY HIGH: cookie trail missing or empty"; exit 1; }

# §2 — branch policy (xl4 = no main pushes)
if git -C "$XL4_REPO_PATH" log --oneline main..HEAD 2>/dev/null | grep -q .; then
  echo "POLICY HIGH: commits on main detected — branch policy forbids"; exit 1
fi

# §A7 — cli-hub install gate (allowlist or paired escalation)
ALLOW="$HOME/gt/.policy/cli_hub_allowlist.yaml"
test -s "$ALLOW" || ALLOW="/dev/null"
python3 - "$TRAIL" "$ALLOW" <<'PY' || exit 1
import json, sys, pathlib, re
try:
    import yaml
except ImportError:
    print("POLICY WARN: PyYAML missing; skipping cli-hub allowlist check"); sys.exit(0)
trail_lines = pathlib.Path(sys.argv[1]).read_text().splitlines()
allow_doc = yaml.safe_load(pathlib.Path(sys.argv[2]).read_text() or "") or {}
mode = (allow_doc.get("policy") or {}).get("mode", "")
denied = {x["name"] for x in (allow_doc.get("denylist") or []) if isinstance(x, dict)}
allowed = {x["name"] for x in (allow_doc.get("allowlisted") or []) if isinstance(x, dict)}
# trust_registry mode: any name OK unless on denylist (operator decision 2026-05-21)
if mode == "trust_registry":
    for i, line in enumerate(trail_lines):
        try: rec = json.loads(line)
        except json.JSONDecodeError: continue
        if rec.get("action") != "cli_hub_install": continue
        ev = rec.get("evidence", "")
        m = re.search(r"name=(\S+)", ev)
        name = m.group(1) if m else ""
        if name in denied:
            print(f"POLICY HIGH: cli_hub_install of {name!r} is on the denylist"); sys.exit(1)
    sys.exit(0)
for i, line in enumerate(trail_lines):
    try: rec = json.loads(line)
    except json.JSONDecodeError: continue
    if rec.get("action") != "cli_hub_install": continue
    ev = rec.get("evidence", "")
    m = re.search(r"name=(\S+)", ev)
    name = m.group(1) if m else ""
    if name in allowed: continue
    prior = trail_lines[max(0, i-20):i]
    if not any('"action":"escalate"' in p and name in p for p in prior):
        print(f"POLICY HIGH: cli_hub_install of {name!r} without allowlist or escalation"); sys.exit(1)
PY

echo "POLICY CLEAN"
BASH
    chmod +x "$policy_dir/check.sh"
  fi
  marker_set policy
}

# ── Bead bodies ──────────────────────────────────────────────────────────────
read -r -d '' DESC_A0 <<'EOF' || true
Read docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md §5 Phase A.0 and
docs/design/log_analyzer_app_and_cache_pivot_2026-05-20.md §6.10 first.

Goal: verify NotebookLM wrapper boots in Fargate under awsvpc, fetches
storage_state.json from S3 at startup, and /readyz passes the ECS container
healthCheck. 1-day timebox.

DO NOT commit the ECS task definition (that lands in A.5).
File scope: services/notebooklm_wrapper/Dockerfile,
services/notebooklm_wrapper/requirements.txt only.
Locked decisions in scope: L3 (no FAISS/torch), L5 (sidecar), L7 (us-east-1).

This bead GATES A.1-A.4 and A.5. If it fails, do not start them.
EOF

read -r -d '' DESC_A14 <<'EOF' || true
Read docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md §5 Phase A.1-A.4
and docs/design/log_analyzer_app_and_cache_pivot_2026-05-20.md §6.1, §6.10.1,
§6.10.2, §6.10.5.

Build 5-file FastAPI service at services/log_analyzer_app/:
  app.py · s3_io.py · Dockerfile · requirements.txt · tests/test_app.py

Endpoints (per HTML §3.2 sequence diagram):
  POST /upload, POST /run/{job_id}, GET /result/{job_id}.

Constraints:
  - boto3>=1.35.0 pinned (L13, P13).
  - asyncio.Semaphore(1) for single-job throttle (L8, §6.10.5).
  - asyncio.wait_for(..., timeout=900) for subprocess (L9, §6.10.1).
  - Emit RunsInFlight CloudWatch metric (+1 after acquire, -1 in finally).
  - Local smoke: tests/compose.local.yml against minio (full upload → run →
    --no-bedrock → result.md → GET /result loop).

Hard NOs:
  - DO NOT inline S3 I/O into pipeline.py (L2).
  - DO NOT add FAISS / torch / sentence-transformers / transformers (L3).
  - DO NOT use force_llm: true in smoke (L12).

Exemplar to mimic for scaffold style: services/notebooklm_wrapper/.
Depends on: A.0 passing.
EOF

read -r -d '' DESC_A5 <<'EOF' || true
Read docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md §5 Phase A.5 and
docs/design/log_analyzer_app_and_cache_pivot_2026-05-20.md §6.10.

Deliverable: ECS task definition JSON only. Hand the JSON to the operator.
DO NOT apply it.

Required pins:
  - platformVersion: "1.4.0" (L9, P15).
  - stopTimeout: 900 (L9).
  - awsvpc network mode (L8/L9).
  - NotebookLM wrapper as sidecar container in the same task (L5).
  - SSE-S3 (AES-256) bucket default; SSE-KMS only for
    secrets/notebooklm/storage_state.json (L14).
  - IAM execution + task role ARNs scoped to us-east-1 (L7).
  - Cost-per-run CloudWatch alarm at $50 / 1 h on AWS/Bedrock
    InvokeModelCost (L15).

Hard NOs:
  - DO NOT write Terraform / Pulumi / CDK (L4).
  - DO NOT apply the JSON — operator applies.

Depends on: A.1-A.4 landed (so container shape is final).
EOF

read -r -d '' DESC_B1 <<'EOF' || true
Read docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md §5 Phase B.1 and
docs/design/log_analyzer_app_and_cache_pivot_2026-05-20.md §7.1.

Scope: instrumentation only. NO paid Bedrock calls.

Deliverables in pipeline.py:
  - Cache-hit / cache-miss telemetry on the Bedrock invocation path.
  - --single-batch dry-run mode that emits the would-be cached system
    block and would-be user message into bedrock_preview/.
  - Tests for both telemetry counters and the dry-run dump.

Defaults stay:
  - --no-bedrock + cost-projection is the default iteration loop (L11).
  - force_llm: false in smoke (L12).
  - Bedrock region us-east-1 (L7).

Hard NOs:
  - NO paid runs in this bead.
  - DO NOT touch ClaudeAdapter.build_request (that is B.2).

File scope: pipeline.py + tests only.
EOF

read -r -d '' DESC_EPIC_A <<'EOF' || true
Epic for Part A — `services/log_analyzer_app/` (base branch: main).

Children:
  - A.0 — NotebookLM sidecar Fargate spike (GATING — must pass first)
  - A.1-A.4 — 5-file scaffold + endpoints + subprocess + minio smoke
  - A.5 — ECS task definition JSON

Integration branch: integration/log-analyzer-app
Lands back to: main (single squash merge when all 3 children close).

Canonical spec: docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md
Locked decisions: L1–L15 apply (do not relitigate).
EOF

read -r -d '' DESC_EPIC_B <<'EOF' || true
Epic for Part B — single-batch + prompt-cache pivot (base branch: feat/rca-path-raw).

Children:
  - B.1 — Instrumentation + --single-batch dry-run (NO paid calls)
  - B.2 — ClaudeAdapter unfreeze + R1/R2/R3 collapse + cache wiring

Integration branch: integration/single-batch-cache-pivot
Lands back to: feat/rca-path-raw (NOT main — Part B is a feature-branch pivot).

Operator-driven follow-on (NOT in this epic):
  - B.3 — Paid A/B on VF7RHD-8813 (operator runs, operator scores; L10/L11).
  - B.4 — Quality bar + rollback decision (operator).

Canonical spec: docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md §4 + §5
F1 BLOCKER resolution: path (a) — unfreeze ClaudeAdapter.build_request.
EOF

read -r -d '' DESC_B2 <<'EOF' || true
Read docs/design/log_analyzer_app_kickoff_v5_2026-05-20.md §4 (F1 resolution)
and docs/design/log_analyzer_app_and_cache_pivot_2026-05-20.md §7.2 BEFORE
editing any code.

F1 BLOCKER (operator picked path (a) on 2026-05-20 PM):
  Extend ClaudeAdapter.build_request at pipeline.py:4162-4172 — signature
  becomes:
    build_request(prompt, *, system_blocks: list[dict] | None = None, ...)
  Plumb cache_control={"type": "ephemeral"} through when system_blocks is
  provided. The system-absent code path MUST stay byte-compatible with
  today's adapter contract because chunked-mode is the rollback path.

Then:
  Collapse R1 / R2 / R3 from chunked-per-batch into a single batch with a
  cached `system` block. Nova and Gemma adapters STAY FROZEN — §5.2
  amendment applies to ClaudeAdapter only.

Tests required for BOTH:
  - system-present path (cache wiring exercised).
  - system-absent path (byte-compatible with today's adapter).

Hard NOs:
  - NO paid Bedrock runs in this bead (paid A/B is B.3, operator-only, L10/L11).
  - DO NOT touch Nova or Gemma adapters.
  - DO NOT re-introduce chunking after the collapse — if context-window
    pressure forces chunking, surface a BLOCKER report.

Depends on: B.1 landed (telemetry + dry-run available to validate the cache).
EOF

# ── Step 4: create 5 beads, capture IDs ──────────────────────────────────────
# bd is run with -C pointed at the rig dir so it auto-discovers the rig's
# .beads/*.db (xl4-* prefix). Without -C, bd would write to the town-level
# beads under ~/gt/.beads/ (hq-* prefix), which is wrong for rig work.
# Title is positional. Description comes via --body-file - on stdin so multi-
# line HEREDOCs survive intact. --silent prints just the ID, for scripting.
RIG_BEADS_PATH_DEFAULT="$GT_HOME/$RIG_NAME"
RIG_BEADS_PATH="${RIG_BEADS_PATH:-$RIG_BEADS_PATH_DEFAULT}"

create_bead() {
  # create_bead "title" "desc" [type=task]
  local title="$1" desc="$2" type="${3:-task}"
  local raw id
  raw=$(printf '%s' "$desc" | bd -C "$RIG_BEADS_PATH" create \
        --type "$type" --title "$title" --body-file - --silent 2>&1) || {
    printf '%s\n' "$raw" >&2
    die "bd create failed for: $title"
  }
  # --silent should print only the ID, but sanitize defensively.
  id=$(printf '%s' "$raw" | grep -oE "${RIG_PREFIX}-[a-z0-9]{2,10}" | head -1 || true)
  [[ -n "$id" ]] || { printf '%s\n' "$raw" >&2; die "could not capture bead ID for: $title"; }
  printf '%s' "$id"
}

ensure_bead() {
  # ensure_bead VAR "title" "desc" [type=task]
  # If $VAR is already set in env (e.g. sourced from a partial beads.env),
  # skip creation. Otherwise create the bead, capture its ID into the env
  # var, and append "VAR=<id>" to $BEADS_ENV.
  local var="$1" title="$2" desc="$3" type="${4:-task}"
  local existing="${!var:-}"
  if [[ -n "$existing" ]]; then
    log "    $var already captured as $existing — skipping create"
    return
  fi
  log "    creating $var [$type] — $title"
  local id
  id=$(create_bead "$title" "$desc" "$type")
  printf -v "$var" '%s' "$id"
  echo "$var=$id" >>"$BEADS_ENV"
}

create_beads() {
  if marker_done beads; then log "beads: skipped (file: $BEADS_ENV)"; return; fi
  log "Creating 5 beads via bd (per-bead idempotent — skips any already in $BEADS_ENV)"

  # Source any existing beads.env so per-bead skip-if-already-captured works.
  if [[ -f "$BEADS_ENV" ]]; then
    # shellcheck disable=SC1090
    source "$BEADS_ENV"
  else
    echo "# generated by setup.sh on $(date -Iseconds)" >"$BEADS_ENV"
  fi

  ensure_bead XL4_A0  "A.0 — NotebookLM sidecar Fargate spike (gating)"                     "$DESC_A0"
  ensure_bead XL4_A14 "A.1-A.4 — log_analyzer_app scaffold + endpoints + minio smoke"       "$DESC_A14"
  ensure_bead XL4_A5  "A.5 — ECS task definition JSON"                                       "$DESC_A5"
  ensure_bead XL4_B1  "B.1 — single-batch instrumentation + dry-run mode (NO paid calls)"   "$DESC_B1"
  ensure_bead XL4_B2  "B.2 — ClaudeAdapter unfreeze + R1/R2/R3 collapse + cache wiring"     "$DESC_B2"

  marker_set beads
}

load_beads() {
  [[ -f "$BEADS_ENV" ]] || die "no $BEADS_ENV — run earlier steps first"
  # shellcheck disable=SC1090
  source "$BEADS_ENV"
  : "${XL4_A0:?missing}" "${XL4_A14:?missing}" "${XL4_A5:?missing}" \
    "${XL4_B1:?missing}" "${XL4_B2:?missing}"
}

load_epics() {
  load_beads
  : "${XL4_EPIC_A:?missing — run ensure_epics step}" \
    "${XL4_EPIC_B:?missing — run ensure_epics step}"
}

# ── Step 4b: create 2 epic beads ─────────────────────────────────────────────
# `gt mq integration create` requires an EPIC bead id, not a task. Each Part
# gets one epic; the 5 task beads are re-parented under them in the next step.
ensure_epics() {
  if marker_done epics; then log "epics: skipped"; return; fi
  load_beads
  log "Creating 2 epic beads"
  if [[ -f "$BEADS_ENV" ]]; then
    # shellcheck disable=SC1090
    source "$BEADS_ENV"
  fi
  ensure_bead XL4_EPIC_A "log-analyzer-app (Part A)"           "$DESC_EPIC_A" epic
  ensure_bead XL4_EPIC_B "single-batch-cache-pivot (Part B)"   "$DESC_EPIC_B" epic
  marker_set epics
}

# ── Step 4c: reparent the 5 task beads under their epics ─────────────────────
reparent_tasks() {
  if marker_done reparent; then log "reparent: skipped"; return; fi
  load_epics
  log "Re-parenting task beads under their epics"
  local tid
  for tid in "$XL4_A0" "$XL4_A14" "$XL4_A5"; do
    log "    $tid → $XL4_EPIC_A"
    bd -C "$RIG_BEADS_PATH" update "$tid" --parent "$XL4_EPIC_A" >/dev/null
  done
  for tid in "$XL4_B1" "$XL4_B2"; do
    log "    $tid → $XL4_EPIC_B"
    bd -C "$RIG_BEADS_PATH" update "$tid" --parent "$XL4_EPIC_B" >/dev/null
  done
  marker_set reparent
}

# ── Step 5: integration branches (against EPICs, not tasks) ──────────────────
create_integrations() {
  if marker_done integrations; then log "integrations: skipped"; return; fi
  load_epics
  log "Creating integration branches against epics"
  if ! gt mq integration list 2>/dev/null | grep -q "$PARTA_INTEG_BRANCH"; then
    log "  Epic A ($XL4_EPIC_A) → $PARTA_INTEG_BRANCH (base: $PARTA_BASE)"
    gt mq integration create "$XL4_EPIC_A" --branch "$PARTA_INTEG_BRANCH"
  fi
  if ! gt mq integration list 2>/dev/null | grep -q "$PARTB_INTEG_BRANCH"; then
    log "  Epic B ($XL4_EPIC_B) → $PARTB_INTEG_BRANCH (base: $PARTB_BASE)"
    gt mq integration create "$XL4_EPIC_B" --branch "$PARTB_INTEG_BRANCH" \
      --base-branch "$PARTB_BASE"
  fi
  marker_set integrations
}

# ── Step 6: convoys ──────────────────────────────────────────────────────────
create_convoys() {
  if marker_done convoys; then log "convoys: skipped"; return; fi
  load_beads
  log "Creating convoys"
  gt convoy create "log-analyzer-app (Part A)" \
    "$XL4_A0" "$XL4_A14" "$XL4_A5" --notify overseer
  gt convoy create "single-batch-cache-pivot (Part B)" \
    "$XL4_B1" "$XL4_B2" --notify overseer
  marker_set convoys
}

# ── Step 7: sling wave 1 ─────────────────────────────────────────────────────
# Canonical pattern (verified 2026-05-21): formula sling from TOWN ROOT with
# --create. Bare `gt sling <bead> <rig>` from the rig dir creates the wisp
# bond but skips the nudge that tells Claude inside the polecat to start work
# — polecats spawn, run `gt hook` (which reads empty due to a separate gt
# bd-write/read sync bug), and self-terminate via `gt done --status DEFERRED`.
# Formula sling does "Cook + wisp + attach + nudge" (per `gt sling --help`).
# `respawn-reset` is prepended in case a prior attempt locked the bead.
sling_one() {
  local bead="$1"
  ( cd "$GT_TOWN_ROOT" && gt sling respawn-reset "$bead" >/dev/null 2>&1 || true )
  ( cd "$GT_TOWN_ROOT" && gt sling mol-polecat-work "$RIG_NAME" \
      --on "$bead" --no-convoy --agent "$AGENT_RUNTIME" --create )
}

sling_wave1() {
  if marker_done sling_wave1; then log "sling wave 1: skipped"; return; fi
  load_beads
  log "Slinging wave 1 (A.0 + B.1) via formula sling from town root"
  log "  → $XL4_A0 (A.0)"
  sling_one "$XL4_A0"
  log "  → $XL4_B1 (B.1)"
  sling_one "$XL4_B1"
  marker_set sling_wave1
}

# ── Step 8: verify polecats actually picked up work ──────────────────────────
# Polls bead status for ~45s. The canonical signal that a polecat received the
# nudge and started work is the bead transitioning OPEN → in_progress (the
# polecat's first action is typically `bd update <bead> --status=in_progress`).
# Dolt write/read lag means we may need a few retries before the status flips.
verify_polecats() {
  if marker_done verify; then log "verify polecats: skipped"; return; fi
  load_beads
  log "Verifying wave 1 polecats picked up work (polling up to 45s)"
  local bead status attempts=0 max_attempts=15
  local pending=("$XL4_A0" "$XL4_B1")
  while (( attempts < max_attempts )) && (( ${#pending[@]} > 0 )); do
    sleep 3
    local still_pending=()
    for bead in "${pending[@]}"; do
      # `bd show <id> --json` returns a JSON ARRAY [{...}], so unwrap [0]
      # before .get(). The 2>/dev/null swallows transient "auto-importing..."
      # stderr from bd; the inline Python tolerates list-or-dict.
      status=$(bd -C "$RIG_BEADS_PATH" show "$bead" --json 2>/dev/null \
               | python3 -c "import sys,json; d=json.load(sys.stdin); d=d[0] if isinstance(d,list) else d; print(d.get('status','?'))" 2>/dev/null || echo "?")
      # "hooked" is the canonical post-sling status the Mayor sets BEFORE the
      # polecat transitions to in_progress. Treat it as success.
      case "$status" in
        hooked|in_progress|closed|escalated)
          log "  ✓ $bead → $status"
          ;;
        *)
          still_pending+=("$bead")
          ;;
      esac
    done
    pending=("${still_pending[@]}")
    attempts=$((attempts + 1))
  done
  if (( ${#pending[@]} > 0 )); then
    warn "  polecats did not pick up these beads within 45s: ${pending[*]}"
    warn "  inspect with: gt peek xl4/<polecat-name> -n 80"
    warn "  this run is degraded; rerun to retry the verify step (marker not set on warn path)"
    # Intentionally do NOT set the marker — let the next run retry verify.
    return
  fi
  marker_set verify
}

# ── Final summary ────────────────────────────────────────────────────────────
print_summary() {
  load_epics
  cat <<EOF

────────────────────────────────────────────────────────────────────
  Setup complete.  IDs (also saved to $BEADS_ENV):

    Part A epic  $XL4_EPIC_A   (base $PARTA_BASE)
      A.0        $XL4_A0   ← slung (wave 1)
      A.1-4      $XL4_A14
      A.5        $XL4_A5

    Part B epic  $XL4_EPIC_B   (base $PARTB_BASE)
      B.1        $XL4_B1   ← slung (wave 1)
      B.2        $XL4_B2

  Open polecat sessions in separate terminals:

    tmux ls                                # see all sessions
    tmux attach -t $RIG_NAME               # rig dashboard
    # per-polecat session names are auto-generated; pick from tmux ls

  Monitor:

    gt convoy status                       # all convoys
    gt status                              # rig overview
    bd show $XL4_A0                        # any bead detail

  When A.0 lands:
    gt sling $XL4_A14 $RIG_NAME --no-convoy --agent $AGENT_RUNTIME

  When A.1-4 lands:
    gt sling $XL4_A5 $RIG_NAME --no-convoy --agent $AGENT_RUNTIME

  When B.1 lands:
    gt sling $XL4_B2 $RIG_NAME --no-convoy --agent $AGENT_RUNTIME

  Land integration branches when each epic's children all close:
    gt mq integration land $XL4_EPIC_A    # Part A → main
    gt mq integration land $XL4_EPIC_B    # Part B → $PARTB_BASE

  Phase B.3 (paid A/B on VF7RHD-8813) and Phase B.4 (rollback decision)
  are operator-driven — NOT slung as polecat work.
────────────────────────────────────────────────────────────────────
EOF
}

# ── Main ─────────────────────────────────────────────────────────────────────
main() {
  check_prereqs
  build_gastown              # uses its own cd into $GASTOWN_SRC
  init_town                  # mkdir -p only
  # All subsequent gt/bd commands discover the town by walking up from cwd
  # (per gastown's own scripts/bootstrap-local-rig.sh pattern). Anchor here.
  cd "$GT_HOME"
  add_rig
  # After the rig exists, hop into it: `gt mq integration create`, `gt convoy
  # create`, and `gt sling` all require either cwd inside the rig dir or
  # GT_RIG to be set. We do both for resilience.
  export GT_RIG="$RIG_NAME"
  cd "$GT_HOME/$RIG_NAME"
  ensure_crew
  install_cli_hub
  deploy_policy
  create_beads
  ensure_epics
  reparent_tasks
  create_integrations
  create_convoys
  sling_wave1
  verify_polecats
  print_summary
}

main "$@"
