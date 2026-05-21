#!/usr/bin/env bash
# open-sessions.sh — open every running gastown session in its own terminal.
#
# Default: one gnome-terminal window with one tab per session, each tab
# running `gt session at <rig>/<name>` to attach to the polecat's tmux pane
# on gastown's custom socket (so plain `tmux attach -t ...` won't work).
#
# Idempotent: gnome-terminal tabs are uniquely titled; run again after
# slinging wave 2/3 to add tabs for the new sessions (it will add duplicates
# unless you close the first window — there's no de-dup on the gt side).
#
# Modes:
#   bash open-sessions.sh              # default: tabs in one new window
#   MODE=windows bash open-sessions.sh # separate window per session
#   MODE=list    bash open-sessions.sh # just print what would open, don't open
#
# Filter:
#   FILTER_RUNNING=1   include only sessions with running=true (default)
#   FILTER_RUNNING=0   include every session gt knows about

set -euo pipefail

MODE="${MODE:-tabs}"                            # tabs | windows | list
FILTER_RUNNING="${FILTER_RUNNING:-1}"
TERMINAL="${TERMINAL:-gnome-terminal}"
# Gastown workspace discovery — falls back to env vars when cwd is outside
# the town. Override if your town/rig is non-default.
export GT_TOWN_ROOT="${GT_TOWN_ROOT:-$HOME/gt}"
export GT_RIG="${GT_RIG:-xl4}"

command -v "$TERMINAL" >/dev/null 2>&1 \
  || { echo "error: $TERMINAL not on PATH (override with TERMINAL=...)" >&2; exit 1; }
command -v gt >/dev/null 2>&1 \
  || { echo "error: gt not on PATH" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 \
  || { echo "error: python3 not on PATH (used to parse JSON)" >&2; exit 1; }
[[ -f "$GT_TOWN_ROOT/mayor/town.json" ]] \
  || { echo "error: no mayor/town.json under GT_TOWN_ROOT=$GT_TOWN_ROOT" >&2; exit 1; }

# Capture gt's stdout and stderr separately so an error message can't be
# mis-piped into json.load(). Also cd into the rig dir for belt-and-suspenders.
gt_stderr="$(mktemp)"
trap 'rm -f "$gt_stderr"' EXIT
gt_stdout="$(cd "$GT_TOWN_ROOT/$GT_RIG" 2>/dev/null && gt session list --json 2>"$gt_stderr")" || {
  echo "error: gt session list failed:" >&2
  cat "$gt_stderr" >&2
  exit 1
}

# Emit "<rig>/<polecat>" one per line for every (optionally-running) session.
mapfile -t SESSIONS < <(
  printf '%s' "$gt_stdout" | python3 -c "
import sys, json, os
only_running = os.environ.get('FILTER_RUNNING', '1') == '1'
for s in json.load(sys.stdin):
    if only_running and not s.get('running'):
        continue
    print(f\"{s['rig']}/{s['polecat']}\")
"
)

if (( ${#SESSIONS[@]} == 0 )); then
  echo "no sessions to open (FILTER_RUNNING=$FILTER_RUNNING)"
  exit 0
fi

if [[ "$MODE" == "list" ]]; then
  printf '%s\n' "${SESSIONS[@]}"
  exit 0
fi

case "$MODE" in
  tabs)
    # One window, one tab per session.
    args=()
    for s in "${SESSIONS[@]}"; do
      args+=( --tab --title="$s" -- bash -c "gt session at '$s'; exec bash" )
    done
    "$TERMINAL" "${args[@]}" &
    disown
    echo "opened ${#SESSIONS[@]} tabs in one $TERMINAL window"
    ;;
  windows)
    # One window per session.
    for s in "${SESSIONS[@]}"; do
      "$TERMINAL" --window --title="$s" -- bash -c "gt session at '$s'; exec bash" &
      disown
    done
    echo "opened ${#SESSIONS[@]} separate $TERMINAL windows"
    ;;
  *)
    echo "unknown MODE: $MODE (expected tabs|windows|list)" >&2
    exit 2
    ;;
esac
