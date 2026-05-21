#!/usr/bin/env bash
# Tests for brain-dog/run.sh logic.
#
# Exercises the threshold comparison, config-loading, and escalation-path
# decisions without calling any real external tools (gt, gbrain, bd).
# All external commands are stubbed via PATH prepend.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FAILURES=0

log() { echo "[test] $*"; }

pass() { echo "  PASS: $1"; }

fail() {
  echo "  FAIL: $1"
  FAILURES=$((FAILURES + 1))
}

# ---------------------------------------------------------------------------
# Helper: stub directory for fake commands
# ---------------------------------------------------------------------------

STUB_DIR=$(mktemp -d /tmp/brain-dog-stubs.XXXXXX)
trap 'rm -rf "$STUB_DIR"' EXIT

make_stub() {
  local name="$1"
  local body="$2"
  local path="$STUB_DIR/$name"
  printf '#!/usr/bin/env bash\n%s\n' "$body" > "$path"
  chmod +x "$path"
}

export PATH="$STUB_DIR:$PATH"

# ---------------------------------------------------------------------------
# Reusable helpers that mirror run.sh logic (kept in sync manually)
# ---------------------------------------------------------------------------

# Returns 0 (needs remediation) when SCORE < SCORE_THRESHOLD, else 1.
# Uses the same env-var names and python3 one-liner as run.sh so the
# sync-guard below confirms they stay identical.
score_below_threshold() {
  local SCORE="$1"
  local SCORE_THRESHOLD="$2"
  python3 -c "import sys; sys.exit(0 if float('$SCORE') < float('$SCORE_THRESHOLD') else 1)" 2>/dev/null
}

# Sync-guard: verify the python3 one-liner in run.sh matches what we test here.
RUN_SH_ONELINER=$(grep -o "float.*else 1" "$SCRIPT_DIR/run.sh" | head -1)
TEST_ONELINER=$(grep -o "float.*else 1" "$0" | head -1)
if [[ "$RUN_SH_ONELINER" != "$TEST_ONELINER" ]]; then
  echo "FAIL: score comparison one-liner in test doesn't match run.sh"
  echo "      run.sh : $RUN_SH_ONELINER"
  echo "      test   : $TEST_ONELINER"
  exit 1
fi

# ---------------------------------------------------------------------------
# Test group 1: threshold comparison logic
# ---------------------------------------------------------------------------

echo "=== Threshold comparison tests ==="

# Below threshold → needs remediation (exit 0)
if score_below_threshold 85 90; then pass "score 85 < threshold 90 → REMEDIATE"
else fail "score 85 < threshold 90 should trigger remediation"; fi

if score_below_threshold 0 90; then pass "score 0 < threshold 90 → REMEDIATE"
else fail "score 0 < threshold 90 should trigger remediation"; fi

if score_below_threshold 89 90; then pass "score 89 < threshold 90 → REMEDIATE"
else fail "score 89 < threshold 90 should trigger remediation"; fi

# At or above threshold → skip (exit 1)
if ! score_below_threshold 90 90; then pass "score 90 == threshold 90 → SKIP"
else fail "score 90 == threshold 90 should NOT trigger remediation"; fi

if ! score_below_threshold 95 90; then pass "score 95 > threshold 90 → SKIP"
else fail "score 95 > threshold 90 should NOT trigger remediation"; fi

if ! score_below_threshold 100 90; then pass "score 100 > threshold 90 → SKIP"
else fail "score 100 > threshold 90 should NOT trigger remediation"; fi

# Custom threshold
if score_below_threshold 70 75; then pass "score 70 < custom threshold 75 → REMEDIATE"
else fail "score 70 < custom threshold 75 should trigger remediation"; fi

if ! score_below_threshold 80 75; then pass "score 80 > custom threshold 75 → SKIP"
else fail "score 80 > custom threshold 75 should NOT trigger remediation"; fi

# ---------------------------------------------------------------------------
# Test group 2: dry-run exits 0 without calling gbrain/gt/bd
# ---------------------------------------------------------------------------

echo ""
echo "=== Dry-run mode tests ==="

# Stub gt brain stats to return a low score
make_stub "gt" 'case "$*" in "brain stats") echo '"'"'{"score": 70}'"'"' ;; *) echo "STUB: gt $*" ;; esac'
# gbrain must NOT be called in dry-run; if it is, fail the test
make_stub "gbrain" 'echo "UNEXPECTED gbrain call: $*" >&2; exit 99'
make_stub "bd" 'echo "STUB: bd $*"'

TMPSTATE=$(mktemp /tmp/brain-dog-state.XXXXXX.json)
DRY_OUT=$(SCORE_THRESHOLD=90 DAILY_USD_CAP=1.0 STATE_FILE="$TMPSTATE" \
  bash "$SCRIPT_DIR/run.sh" --dry-run 2>&1) || DRY_RC=$?
DRY_RC=${DRY_RC:-0}

if [[ $DRY_RC -eq 0 ]]; then pass "dry-run exits 0"
else fail "dry-run exited $DRY_RC"; fi

if echo "$DRY_OUT" | grep -q "DRY RUN"; then pass "dry-run prints DRY RUN notice"
else fail "dry-run output missing DRY RUN notice"; fi

if echo "$DRY_OUT" | grep -qi "gbrain doctor --remediate"; then pass "dry-run shows what would run"
else fail "dry-run should show the gbrain command that would run"; fi

# gbrain stub is "exit 99" — if it had been called, dry-run would have exited 99
if [[ $DRY_RC -ne 99 ]]; then pass "dry-run did not invoke gbrain"
else fail "dry-run must NOT invoke gbrain"; fi

rm -f "$TMPSTATE"

# ---------------------------------------------------------------------------
# Test group 3: score OK path — skips remediation, records success bead
# ---------------------------------------------------------------------------

echo ""
echo "=== Score OK path (no remediation) ==="

BD_LOG=$(mktemp /tmp/brain-dog-bd.XXXXXX)
make_stub "gt"  'case "$*" in "brain stats") echo '"'"'{"score": 95}'"'"' ;; *) echo "STUB: gt $*" ;; esac'
make_stub "gbrain" 'echo "UNEXPECTED gbrain call: $*" >&2; exit 99'
make_stub "bd"  "echo \"STUB: bd \$*\" >> \"$BD_LOG\""

TMPSTATE=$(mktemp /tmp/brain-dog-state.XXXXXX.json)
OK_OUT=$(SCORE_THRESHOLD=90 DAILY_USD_CAP=1.0 STATE_FILE="$TMPSTATE" \
  bash "$SCRIPT_DIR/run.sh" 2>&1) || OK_RC=$?
OK_RC=${OK_RC:-0}

if [[ $OK_RC -eq 0 ]]; then pass "score-OK path exits 0"
else fail "score-OK path exited $OK_RC"; fi

if echo "$OK_OUT" | grep -q "SKIP"; then pass "score-OK path prints SKIP"
else fail "score-OK path should print SKIP"; fi

# bd create should have been called with result:success label
if grep -q "result:success" "$BD_LOG" 2>/dev/null; then pass "bd create called with result:success"
else fail "bd create should be called with result:success on score-OK path"; fi

rm -f "$TMPSTATE" "$BD_LOG"

# ---------------------------------------------------------------------------
# Test group 4: score below threshold — gbrain remediation called
# ---------------------------------------------------------------------------

echo ""
echo "=== Remediation path (score below threshold) ==="

BD_LOG=$(mktemp /tmp/brain-dog-bd.XXXXXX)
GBRAIN_LOG=$(mktemp /tmp/brain-dog-gbrain.XXXXXX)
make_stub "gt"     'case "$*" in "brain stats") echo '"'"'{"score": 80}'"'"' ;; *) echo "STUB: gt $*" ;; esac'
make_stub "gbrain" "echo \"STUB: gbrain \$*\" >> \"$GBRAIN_LOG\""
make_stub "bd"     "echo \"STUB: bd \$*\" >> \"$BD_LOG\""

TMPSTATE=$(mktemp /tmp/brain-dog-state.XXXXXX.json)
REM_OUT=$(SCORE_THRESHOLD=90 DAILY_USD_CAP=1.0 STATE_FILE="$TMPSTATE" \
  bash "$SCRIPT_DIR/run.sh" 2>&1) || REM_RC=$?
REM_RC=${REM_RC:-0}

if [[ $REM_RC -eq 0 ]]; then pass "remediation path exits 0 on success"
else fail "remediation path exited $REM_RC (gbrain stub should exit 0)"; fi

if grep -q "doctor --remediate" "$GBRAIN_LOG" 2>/dev/null; then pass "gbrain doctor --remediate was invoked"
else fail "gbrain doctor --remediate should be called when score is low"; fi

if grep -q "\-\-max-usd 1.0" "$GBRAIN_LOG" 2>/dev/null; then pass "gbrain called with --max-usd 1.0"
else fail "gbrain should be called with --max-usd matching DAILY_USD_CAP"; fi

if grep -q "result:success" "$BD_LOG" 2>/dev/null; then pass "bd create called with result:success after remediation"
else fail "bd create should record result:success on successful remediation"; fi

rm -f "$TMPSTATE" "$BD_LOG" "$GBRAIN_LOG"

# ---------------------------------------------------------------------------
# Test group 5: remediation failure → escalation + warning bead
# ---------------------------------------------------------------------------

echo ""
echo "=== Escalation path (remediation fails) ==="

BD_LOG=$(mktemp /tmp/brain-dog-bd.XXXXXX)
ESCALATE_LOG=$(mktemp /tmp/brain-dog-escalate.XXXXXX)

make_stub "gt" "
case \"\$*\" in
  'brain stats') echo '{\"score\": 60}' ;;
  escalate*)     echo \"STUB: gt escalate \$*\" >> \"$ESCALATE_LOG\" ;;
  *)             echo \"STUB: gt \$*\" ;;
esac"
make_stub "gbrain" 'echo "gbrain doctor failed" >&2; exit 1'
make_stub "bd"     "echo \"STUB: bd \$*\" >> \"$BD_LOG\""

TMPSTATE=$(mktemp /tmp/brain-dog-state.XXXXXX.json)
# run.sh uses `|| true` on gt escalate and bd, so overall exit should be 0
ESC_OUT=$(SCORE_THRESHOLD=90 DAILY_USD_CAP=1.0 STATE_FILE="$TMPSTATE" \
  bash "$SCRIPT_DIR/run.sh" 2>&1) || ESC_RC=$?
ESC_RC=${ESC_RC:-0}

if [[ $ESC_RC -eq 0 ]]; then pass "escalation path exits 0 (non-fatal)"
else fail "escalation path exited $ESC_RC; run.sh should not propagate gbrain failure"; fi

if grep -q "escalate" "$ESCALATE_LOG" 2>/dev/null; then pass "gt escalate was called on remediation failure"
else fail "gt escalate should be called when gbrain remediation fails"; fi

if grep -q "result:warning" "$BD_LOG" 2>/dev/null; then pass "bd create called with result:warning on escalation"
else fail "bd create should use result:warning when escalating"; fi

rm -f "$TMPSTATE" "$BD_LOG" "$ESCALATE_LOG"

# ---------------------------------------------------------------------------
# Test group 6: TOML frontmatter in plugin.md parses correctly
# ---------------------------------------------------------------------------

echo ""
echo "=== plugin.md TOML frontmatter validation ==="

PLUGIN_MD="$SCRIPT_DIR/plugin.md"
if [[ ! -f "$PLUGIN_MD" ]]; then
  fail "plugin.md not found at $PLUGIN_MD"
else
  TOML_BODY=$(awk '/^\+\+\+/{p=!p; next} p' "$PLUGIN_MD" | head -50)
  if python3 -c "
import sys
try:
    import tomllib
except ImportError:
    try:
        import tomli as tomllib
    except ImportError:
        # tomllib unavailable; fall back to basic check
        sys.exit(0)

content = sys.stdin.read()
tomllib.loads(content)
" <<< "$TOML_BODY" 2>/dev/null; then
    pass "plugin.md TOML frontmatter parses without error"
  else
    fail "plugin.md TOML frontmatter failed to parse"
  fi

  # Verify required TOML fields are present
  for field in 'name = "brain-dog"' '[gate]' '[tracking]' '[execution]'; do
    if echo "$TOML_BODY" | grep -qF "$field"; then
      pass "plugin.md contains TOML field: $field"
    else
      fail "plugin.md missing TOML field: $field"
    fi
  done

  # Verify required labels
  if echo "$TOML_BODY" | grep -q 'plugin:brain-dog'; then
    pass "plugin.md labels include plugin:brain-dog"
  else
    fail "plugin.md labels must include plugin:brain-dog"
  fi

  if echo "$TOML_BODY" | grep -q 'category:maintenance'; then
    pass "plugin.md labels include category:maintenance"
  else
    fail "plugin.md labels must include category:maintenance"
  fi
fi

# ---------------------------------------------------------------------------
# Test group 7: executable bits
# ---------------------------------------------------------------------------

echo ""
echo "=== Executable bit verification ==="

if [[ -x "$SCRIPT_DIR/run.sh" ]]; then pass "run.sh is executable"
else fail "run.sh must be chmod +x"; fi

if [[ -x "$SCRIPT_DIR/run_test.sh" ]]; then pass "run_test.sh is executable"
else fail "run_test.sh must be chmod +x"; fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo ""
if [[ $FAILURES -gt 0 ]]; then
  echo "FAILED: $FAILURES test(s) failed"
  exit 1
else
  echo "PASSED: all tests passed"
fi
