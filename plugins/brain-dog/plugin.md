+++
name = "brain-dog"
description = "Monitor gbrain doctor score and remediate when below threshold"
version = 1

[gate]
type = "cooldown"
duration = "30m"

[tracking]
labels = ["plugin:brain-dog", "category:maintenance"]
digest = true

[execution]
timeout = "5m"
notify_on_failure = true
severity = "medium"
+++

# Brain Dog

Monitors the gbrain doctor score for the current town and triggers remediation
when the score falls below the configured threshold. This is an automated
maintenance check — score below threshold means the brain index may be stale,
the sidecar may have drifted, or the Bun runtime needs attention.

**You are a dog agent (Claude). Gather the data below, then use your judgment
to decide if remediation is needed.** Consider:

- Current doctor score (0–100)
- Threshold (configurable, default 90)
- Whether the sidecar is running
- Cost cap for remediation (configurable, default $1.00/day)
- Whether a previous remediation attempt already failed today

## Config

```bash
BRAIN_YML="${TOWN_ROOT:-$HOME/gt}/.brain.yml"
SCORE_THRESHOLD="${SCORE_THRESHOLD:-90}"
DAILY_USD_CAP="${DAILY_USD_CAP:-1.0}"
STATE_FILE="${TOWN_ROOT:-$HOME/gt}/.brain-dog-state.json"
```

## Step 1: Load config from brain.yml

Read the threshold and cost cap from `brain.yml` if present:

```bash
echo "=== Brain Dog: Checking brain health ==="

# Source overrides from brain.yml if yq or python3 is available.
if command -v python3 >/dev/null 2>&1 && [ -f "$BRAIN_YML" ]; then
  _threshold=$(python3 -c "
import sys
try:
    import yaml
    d = yaml.safe_load(open('$BRAIN_YML'))
    print(d.get('doctor', {}).get('score_threshold', ''))
except Exception:
    pass
" 2>/dev/null)
  [ -n "$_threshold" ] && SCORE_THRESHOLD="$_threshold"

  _cap=$(python3 -c "
import sys
try:
    import yaml
    d = yaml.safe_load(open('$BRAIN_YML'))
    print(d.get('doctor', {}).get('daily_usd_cap', ''))
except Exception:
    pass
" 2>/dev/null)
  [ -n "$_cap" ] && DAILY_USD_CAP="$_cap"
fi

echo "  Threshold : $SCORE_THRESHOLD"
echo "  Daily cap : \$$DAILY_USD_CAP"
```

## Step 2: Get current doctor score

Call `gt brain stats` to retrieve the doctor score:

```bash
echo ""
echo "=== Brain Stats ==="

STATS_OUTPUT=$(gt brain stats 2>&1)
STATS_RC=$?

echo "$STATS_OUTPUT"

if [ $STATS_RC -ne 0 ]; then
  echo "ERROR: gt brain stats failed (exit $STATS_RC)"
  ERROR="gt brain stats failed: $STATS_OUTPUT"
  SCORE=""
else
  # Extract numeric score from JSON output field "score" or "doctor_score"
  SCORE=$(echo "$STATS_OUTPUT" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('score', d.get('doctor_score','')))" \
    2>/dev/null || echo "")
fi

if [ -z "$SCORE" ]; then
  echo "ERROR: Could not parse doctor score from brain stats output"
  ERROR="Could not parse doctor score"
fi
```

## Step 3: Load previous state

Check whether a remediation already ran today to avoid double-spend:

```bash
echo ""
echo "=== Previous State ==="

LAST_REMEDIATION="never"
if [ -f "$STATE_FILE" ]; then
  LAST_STATE=$(cat "$STATE_FILE" 2>/dev/null)
  echo "  Last state: $LAST_STATE"
  LAST_REMEDIATION=$(echo "$LAST_STATE" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('last_remediation','never'))" \
    2>/dev/null || echo "unknown")
  echo "  Last remediation: $LAST_REMEDIATION"
else
  echo "  No previous state (first run)"
fi

# Check recent brain-dog runs in beads for context
RECENT_RUNS=$(bd list --label plugin:brain-dog --status closed --json 2>/dev/null \
  | python3 -c "import sys,json; items=json.load(sys.stdin); print(items[0]['created_at'] if items else 'none')" \
  2>/dev/null || echo "unknown")
echo "  Last brain-dog run: $RECENT_RUNS"
```

## Step 4: Compare score to threshold

Decide whether remediation is warranted:

```bash
echo ""
echo "=== Score vs Threshold ==="
echo "  Score     : ${SCORE:-unknown}"
echo "  Threshold : $SCORE_THRESHOLD"

NEEDS_REMEDIATION=false
SKIP_REASON=""

if [ -z "$SCORE" ]; then
  # Could not get score — treat as degraded and remediate
  NEEDS_REMEDIATION=true
  echo "  Decision  : REMEDIATE (score unavailable)"
elif python3 -c "import sys; sys.exit(0 if float('$SCORE') < float('$SCORE_THRESHOLD') else 1)" 2>/dev/null; then
  NEEDS_REMEDIATION=true
  echo "  Decision  : REMEDIATE (score $SCORE < threshold $SCORE_THRESHOLD)"
else
  echo "  Decision  : SKIP (score $SCORE >= threshold $SCORE_THRESHOLD)"
  SKIP_REASON="score OK"
fi
```

## Step 5: Remediate if needed

Run `gbrain doctor --remediate` with the configured cost cap:

```bash
if $NEEDS_REMEDIATION; then
  echo ""
  echo "=== Remediation ==="
  echo "  Running: gbrain doctor --remediate --max-usd $DAILY_USD_CAP"

  REMEDIATE_OUTPUT=$(gbrain doctor --remediate --max-usd "$DAILY_USD_CAP" 2>&1)
  REMEDIATE_RC=$?
  echo "$REMEDIATE_OUTPUT"

  if [ $REMEDIATE_RC -eq 0 ]; then
    echo "  Remediation succeeded."
    REMEDIATION_STATUS="success"
  else
    echo "  ERROR: Remediation failed (exit $REMEDIATE_RC)"
    REMEDIATION_STATUS="failed"
    ERROR="gbrain doctor --remediate exited $REMEDIATE_RC: $REMEDIATE_OUTPUT"
  fi
else
  REMEDIATION_STATUS="skipped"
fi
```

## Step 6: Save current state

Record this run for the next iteration to compare trends:

```bash
# Save state for next run
python3 - << 'PYEOF'
import json, os, datetime

state = {
    "checked_at": datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
    "score": os.environ.get("SCORE", ""),
    "threshold": os.environ.get("SCORE_THRESHOLD", "90"),
    "remediation_status": os.environ.get("REMEDIATION_STATUS", "unknown"),
}
state_file = os.environ.get("STATE_FILE", os.path.expanduser("~/gt/.brain-dog-state.json"))
os.makedirs(os.path.dirname(state_file), exist_ok=True)
with open(state_file, "w") as f:
    json.dump(state, f, indent=2)
print(f"State saved to {state_file}")
PYEOF
```

## Step 7: Escalate or report

**If remediation failed, escalate to Mayor:**

```bash
gt escalate "brain-dog: gbrain remediation failed" \
  -s MEDIUM \
  --reason "Brain doctor score was ${SCORE:-unknown} (threshold $SCORE_THRESHOLD).
Remediation via 'gbrain doctor --remediate --max-usd $DAILY_USD_CAP' failed.

Error: $ERROR

Run manually: gbrain doctor --remediate --max-usd $DAILY_USD_CAP
Check sidecar: gbrain serve --http"
```

**If score is already OK or remediation succeeded, just record the result:**

```bash
echo "Brain health check complete. Score=${SCORE:-unknown}, status=$REMEDIATION_STATUS."
```

## Record Result

```bash
SUMMARY="brain-dog: score=${SCORE:-unknown} threshold=$SCORE_THRESHOLD status=$REMEDIATION_STATUS"
echo "=== $SUMMARY ==="
```

On success (score OK or remediation succeeded):
```bash
bd create "brain-dog: $SUMMARY" -t chore --ephemeral \
  -l type:plugin-run,plugin:brain-dog,category:maintenance,result:success \
  -d "$SUMMARY" --silent 2>/dev/null || true
```

On escalation (remediation failed):
```bash
bd create "brain-dog: ESCALATED — $SUMMARY" -t chore --ephemeral \
  -l type:plugin-run,plugin:brain-dog,category:maintenance,result:warning \
  -d "Escalated to Mayor. $SUMMARY" --silent 2>/dev/null || true
```

On failure (stats unavailable + remediation failed):
```bash
bd create "brain-dog: FAILED" -t chore --ephemeral \
  -l type:plugin-run,plugin:brain-dog,category:maintenance,result:failure \
  -d "Brain dog check failed: $ERROR" --silent 2>/dev/null || true

gt escalate "Plugin FAILED: brain-dog" \
  --severity medium \
  --reason "$ERROR"
```
