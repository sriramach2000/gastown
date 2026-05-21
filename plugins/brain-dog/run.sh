#!/usr/bin/env bash
# brain-dog/run.sh — Automated brain health monitor and remediator.
#
# Reads the gbrain doctor score via `gt brain stats`, compares it against a
# configurable threshold, and calls `gbrain doctor --remediate --max-usd <cap>`
# when the score falls below the threshold.  On remediation failure, escalates
# to the Mayor via `gt escalate`.  Records every run via `bd create` with
# labels plugin:brain-dog and category:maintenance.
#
# Config (environment or brain.yml):
#   SCORE_THRESHOLD    integer 0-100 (default: 90)
#   DAILY_USD_CAP      float in USD  (default: 1.0)
#   TOWN_ROOT          path to town root dir (default: $HOME/gt)
#   BRAIN_YML          path to brain.yml (default: $TOWN_ROOT/.brain.yml)
#
# Usage: ./run.sh [--threshold N] [--cap USD] [--dry-run] [--check-only]

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration defaults
# ---------------------------------------------------------------------------

TOWN_ROOT="${TOWN_ROOT:-$HOME/gt}"
BRAIN_YML="${BRAIN_YML:-$TOWN_ROOT/.brain.yml}"
SCORE_THRESHOLD="${SCORE_THRESHOLD:-90}"
DAILY_USD_CAP="${DAILY_USD_CAP:-1.0}"
STATE_FILE="${STATE_FILE:-$TOWN_ROOT/.brain-dog-state.json}"
DRY_RUN=false
CHECK_ONLY=false
LOCKFILE="/tmp/brain-dog.lock"

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
  case "$1" in
    --threshold)  SCORE_THRESHOLD="$2"; shift 2 ;;
    --cap)        DAILY_USD_CAP="$2";   shift 2 ;;
    --dry-run)    DRY_RUN=true;         shift ;;
    --check-only) CHECK_ONLY=true;      shift ;;
    --help|-h)
      echo "Usage: $0 [--threshold N] [--cap USD] [--dry-run] [--check-only]"
      echo "  --threshold N    Doctor score floor before remediation (default: 90)"
      echo "  --cap USD        Max spend for gbrain doctor --remediate (default: 1.0)"
      echo "  --dry-run        Report only; do not call gbrain or bd/gt"
      echo "  --check-only     Same as --dry-run (alias for plugin.md compatibility)"
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

$CHECK_ONLY && DRY_RUN=true   # check-only implies dry-run

# ---------------------------------------------------------------------------
# Lock acquisition (prevent concurrent runs)
# ---------------------------------------------------------------------------

if ! mkdir "$LOCKFILE" 2>/dev/null; then
  echo "[brain-dog] ERROR: Another instance is running (lockfile: $LOCKFILE)"
  echo "[brain-dog] If stale, remove with: rmdir $LOCKFILE"
  exit 1
fi
trap 'rmdir "$LOCKFILE" 2>/dev/null || true' EXIT

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

log() { echo "[brain-dog] $*"; }

# Save terminal state for audit.
SCORE=""
REMEDIATION_STATUS="unknown"
ERROR=""

# ---------------------------------------------------------------------------
# Step 1: Load overrides from brain.yml
# ---------------------------------------------------------------------------

log "Starting brain health check (threshold=$SCORE_THRESHOLD, cap=\$$DAILY_USD_CAP, dry_run=$DRY_RUN)"

if command -v python3 >/dev/null 2>&1 && [[ -f "$BRAIN_YML" ]]; then
  _threshold=$(python3 - <<PYEOF 2>/dev/null || true
import yaml, sys
try:
    d = yaml.safe_load(open("$BRAIN_YML"))
    v = d.get("doctor", {}).get("score_threshold", "")
    if v != "": print(v)
except Exception:
    pass
PYEOF
)
  [[ -n "$_threshold" ]] && SCORE_THRESHOLD="$_threshold"

  _cap=$(python3 - <<PYEOF 2>/dev/null || true
import yaml, sys
try:
    d = yaml.safe_load(open("$BRAIN_YML"))
    v = d.get("doctor", {}).get("daily_usd_cap", "")
    if v != "": print(v)
except Exception:
    pass
PYEOF
)
  [[ -n "$_cap" ]] && DAILY_USD_CAP="$_cap"

  log "brain.yml loaded: threshold=$SCORE_THRESHOLD cap=\$$DAILY_USD_CAP"
fi

# ---------------------------------------------------------------------------
# Step 2: Get doctor score via gt brain stats
# ---------------------------------------------------------------------------

log ""
log "=== Brain Stats ==="

STATS_OUTPUT=$(gt brain stats 2>&1) || STATS_RC=$?
STATS_RC=${STATS_RC:-0}

if [[ $STATS_RC -ne 0 ]]; then
  ERROR="gt brain stats failed (exit $STATS_RC): $STATS_OUTPUT"
  log "ERROR: $ERROR"
else
  log "$STATS_OUTPUT"
  # Extract numeric score from JSON field "score" or "doctor_score"
  SCORE=$(echo "$STATS_OUTPUT" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('score', d.get('doctor_score','')))" \
    2>/dev/null || true)
  if [[ -z "$SCORE" ]]; then
    ERROR="Could not parse doctor score from stats output"
    log "ERROR: $ERROR"
  else
    log "Doctor score: $SCORE"
  fi
fi

# ---------------------------------------------------------------------------
# Step 3: Compare score to threshold
# ---------------------------------------------------------------------------

log ""
log "=== Score vs Threshold ==="
log "  Score     : ${SCORE:-unknown}"
log "  Threshold : $SCORE_THRESHOLD"

NEEDS_REMEDIATION=false

if [[ -z "$SCORE" ]]; then
  # Score unavailable — treat as degraded
  NEEDS_REMEDIATION=true
  log "  Decision  : REMEDIATE (score unavailable)"
elif python3 -c "import sys; sys.exit(0 if float('$SCORE') < float('$SCORE_THRESHOLD') else 1)" 2>/dev/null; then
  NEEDS_REMEDIATION=true
  log "  Decision  : REMEDIATE (score $SCORE < threshold $SCORE_THRESHOLD)"
else
  log "  Decision  : SKIP (score $SCORE >= threshold $SCORE_THRESHOLD)"
  REMEDIATION_STATUS="skipped"
fi

# ---------------------------------------------------------------------------
# Step 4: Remediate (unless dry-run or score OK)
# ---------------------------------------------------------------------------

if $NEEDS_REMEDIATION; then
  if $DRY_RUN; then
    log "  DRY RUN — would run: gbrain doctor --remediate --max-usd $DAILY_USD_CAP"
    REMEDIATION_STATUS="dry-run"
  else
    log ""
    log "=== Remediation ==="
    log "  Running: gbrain doctor --remediate --max-usd $DAILY_USD_CAP"

    REMEDIATE_OUTPUT=$(gbrain doctor --remediate --max-usd "$DAILY_USD_CAP" 2>&1) \
      || REMEDIATE_RC=$?
    REMEDIATE_RC=${REMEDIATE_RC:-0}
    log "$REMEDIATE_OUTPUT"

    if [[ $REMEDIATE_RC -eq 0 ]]; then
      log "  Remediation succeeded."
      REMEDIATION_STATUS="success"
    else
      REMEDIATION_STATUS="failed"
      ERROR="gbrain doctor --remediate exited $REMEDIATE_RC: $REMEDIATE_OUTPUT"
      log "  ERROR: $ERROR"
    fi
  fi
fi

# ---------------------------------------------------------------------------
# Step 5: Save state
# ---------------------------------------------------------------------------

if ! $DRY_RUN; then
  SCORE_VAL="$SCORE" THRESHOLD_VAL="$SCORE_THRESHOLD" \
  REM_STATUS="$REMEDIATION_STATUS" STATE_PATH="$STATE_FILE" \
  python3 - <<'PYEOF' 2>/dev/null || true
import json, os, datetime

state = {
    "checked_at": datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    "score": os.environ.get("SCORE_VAL", ""),
    "threshold": os.environ.get("THRESHOLD_VAL", "90"),
    "last_remediation": datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    "remediation_status": os.environ.get("REM_STATUS", "unknown"),
}
state_file = os.environ.get("STATE_PATH", os.path.expanduser("~/gt/.brain-dog-state.json"))
os.makedirs(os.path.dirname(state_file), exist_ok=True)
with open(state_file, "w") as f:
    json.dump(state, f, indent=2)
print(f"[brain-dog] State saved to {state_file}")
PYEOF
fi

# ---------------------------------------------------------------------------
# Step 6: Escalate on failure
# ---------------------------------------------------------------------------

SUMMARY="brain-dog: score=${SCORE:-unknown} threshold=$SCORE_THRESHOLD status=$REMEDIATION_STATUS"
log ""
log "=== $SUMMARY ==="

if [[ "$REMEDIATION_STATUS" == "failed" ]]; then
  log ""
  log "Escalating to Mayor..."
  gt escalate "brain-dog: gbrain remediation failed" \
    -s MEDIUM \
    --reason "Brain doctor score was ${SCORE:-unknown} (threshold $SCORE_THRESHOLD).
Remediation via 'gbrain doctor --remediate --max-usd $DAILY_USD_CAP' failed.

Error: $ERROR

Run manually: gbrain doctor --remediate --max-usd $DAILY_USD_CAP
Check sidecar: gbrain serve --http" 2>/dev/null || true

  bd create "brain-dog: ESCALATED — $SUMMARY" -t chore --ephemeral \
    -l type:plugin-run,plugin:brain-dog,category:maintenance,result:warning \
    -d "Escalated to Mayor. $SUMMARY" --silent 2>/dev/null || true

elif [[ -n "$ERROR" && "$REMEDIATION_STATUS" == "unknown" ]]; then
  # Stats call failed entirely — escalate as plugin failure
  gt escalate "Plugin FAILED: brain-dog" \
    --severity medium \
    --reason "$ERROR" 2>/dev/null || true

  bd create "brain-dog: FAILED" -t chore --ephemeral \
    -l type:plugin-run,plugin:brain-dog,category:maintenance,result:failure \
    -d "Brain dog check failed: $ERROR" --silent 2>/dev/null || true

else
  bd create "brain-dog: $SUMMARY" -t chore --ephemeral \
    -l type:plugin-run,plugin:brain-dog,category:maintenance,result:success \
    -d "$SUMMARY" --silent 2>/dev/null || true
fi

log "Done."
