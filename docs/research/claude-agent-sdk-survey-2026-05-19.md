# Claude Agent SDK Survey

**Date:** 2026-05-19
**Author:** Research agent (SDK integration swarm)
**Branch:** research/claude-agent-sdk-integration

---

## 1. What the Claude Agent SDK Provides

### Background

Anthropic shipped the Agent SDK in mid-2025 as "Claude Code SDK," renamed it to "Claude
Agent SDK" in September 2025 when the runtime proved general-purpose. The SDK gives
programmers the same agent loop, tool execution, and context management that power the
`claude` CLI, exposed as a Python (`claude-agent-sdk`) or TypeScript
(`@anthropic-ai/claude-agent-sdk`) library.

As of May 2026:
- Python package: `v0.1.48` on PyPI (Python 3.10+)
- TypeScript package: `v0.2.71` on npm
- **No first-party Go package exists.** Anthropic has not responded to GitHub issue #498
  (opened 2026-01-20) requesting Go support.

### 1.1 The Agent Loop

Both SDKs expose the same lifecycle:

1. Caller sends `query(prompt, options)` — returns an async iterable / async generator.
2. SDK spawns or connects to a Claude Code subprocess (the CLI binary is bundled with
   the TypeScript package; Python requires it installed separately).
3. Claude evaluates the prompt; if it wants to call a tool it emits a `ToolUseBlock`.
4. The SDK executes the tool (built-in tools run in-process; hooks fire around execution).
5. The tool result is injected back and Claude continues.
6. Loop repeats until `stop_reason == "end_turn"` or a budget limit is hit.
7. A `ResultMessage` is emitted with `subtype`, `num_turns`, `total_cost_usd`, usage.

### 1.2 Built-in Tools

| Tool | What It Does |
|------|--------------|
| `Read` | Read any file in cwd |
| `Write` | Create new files |
| `Edit` | Precise in-place edits |
| `Bash` | Run terminal commands, git operations |
| `Monitor` | Watch a background process, react to each output line |
| `Glob` | Find files by pattern |
| `Grep` | Regex search over file contents |
| `WebSearch` | Search the web |
| `WebFetch` | Fetch and parse web page content |
| `AskUserQuestion` | Ask the caller clarifying questions |
| `Agent` | Spawn a subagent to handle a focused subtask |

The caller controls which tools are available per query via `allowed_tools` /
`disallowed_tools`. Setting `permission_mode="bypassPermissions"` is the programmatic
equivalent of `--dangerously-skip-permissions`.

### 1.3 Hook Callbacks

Hooks are in-process Python or TypeScript callbacks that fire at specific lifecycle
points. The full set of documented hook events as of 2026-05:

| Hook Event | When It Fires |
|------------|---------------|
| `PreToolUse` | Before any tool executes |
| `PostToolUse` | After a tool returns successfully |
| `PostToolUseFailure` | After a tool returns an error |
| `UserPromptSubmit` | When a user prompt enters the loop |
| `SessionStart` | At session initialization |
| `SessionEnd` | At session teardown |
| `Stop` | When the agent loop terminates |
| `SubagentStart` | When a subagent is spawned |
| `SubagentStop` | When a subagent returns |
| `PreCompact` | Before context compaction |
| `PermissionRequest` | When an unpermitted tool is about to be called |
| `Notification` | General lifecycle notifications |
| `Setup` | Environment setup phase |
| `TaskCompleted` | When a task block completes |
| `ConfigChange` | When SDK config changes at runtime |
| `WorktreeCreate` | When a git worktree is created |

Hook callbacks receive `(input_data, tool_use_id, context)` and return a dict controlling
the agent's next action. A `PreToolUse` hook that denies a Bash command:

```python
async def block_rm_rf(input_data, tool_use_id, context):
    cmd = input_data.get("tool_input", {}).get("command", "")
    if "rm -rf" in cmd:
        return {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": "rm -rf blocked by policy"
            }
        }
    return {}
```

This fires entirely in-process — no JSON serialization to a subprocess, no shell
invocation, no file I/O required. Contrast with gastown's current hook model, which
writes JSON to disk and calls `gt tap guard` as a child process.

### 1.4 MCP Server Support

The SDK accepts MCP server definitions in two forms:

**External subprocess (stdio):**
```python
mcp_servers={
    "postgres": {"command": "npx",
                 "args": ["@modelcontextprotocol/server-postgres", conn_str]}
}
```

**In-process SDK MCP server (Python function → MCP tool, zero IPC overhead):**
```python
@tool("query_db", "Run a read-only SQL query", {"sql": str})
async def run_query(args):
    rows = db.execute(args["sql"])
    return {"content": [{"type": "text", "text": str(rows)}]}

server = create_sdk_mcp_server("gastown-tools", "1.0.0", [run_query])
options = ClaudeAgentOptions(mcp_servers={"internal": server})
```

In-process MCP servers call Python functions directly; there is no IPC roundtrip.

### 1.5 Session Management

Sessions are persistent and addressable by ID:

```python
# Capture session ID at start
async for msg in query(prompt, options):
    if isinstance(msg, SystemMessage) and msg.subtype == "init":
        session_id = msg.data["session_id"]

# Resume later (full context preserved)
async for msg in query("continue...", ClaudeAgentOptions(resume=session_id)):
    ...

# Fork from a known checkpoint (explore alternative approach)
async for msg in query("try a different fix", ClaudeAgentOptions(fork_session=session_id)):
    ...
```

Session state is stored as JSONL on the filesystem. Resuming reloads the full
conversation context without reprocessing tool outputs.

### 1.6 Streaming Output Types

The SDK yields a typed message stream. Each message is one of:

| Type | Meaning |
|------|---------|
| `SystemMessage` (subtype `init`) | Session ID, working directory, initial config |
| `AssistantMessage` | Claude's response; content is a list of `TextBlock` or `ToolUseBlock` |
| `UserMessage` | Tool results injected back into the loop |
| `ResultMessage` | Terminal message; contains result text, cost, turn count, usage |
| `StreamEvent` | Token-level partial message (opt-in via `include_partial_messages`) |

### 1.7 Subagents

```python
options = ClaudeAgentOptions(
    allowed_tools=["Read", "Grep", "Agent"],
    agents={
        "code-reviewer": AgentDefinition(
            description="Expert code reviewer",
            prompt="Analyze code quality and security.",
            tools=["Read", "Glob", "Grep"],
        )
    }
)
```

The parent agent calls the `Agent` tool; the subagent runs in its own session with its
own message stream. Messages from a subagent context carry `parent_tool_use_id` for
tracing. This differs fundamentally from gastown's current model, which creates a
separate tmux session per agent with no structured output path back to the spawner.

### 1.8 Managed Agents (Alternative Hosted Path)

In April 2026, Anthropic launched **Managed Agents** public beta: a hosted REST API
where Anthropic runs the agent loop and sandbox. Key endpoints:
`POST /v1/agents`, `POST /v1/sessions`, `GET /v1/sessions/{id}/stream`
(Server-Sent Events). Go code can call this REST API directly with `net/http` — no
SDK required. Tradeoff: no filesystem access on the caller's machine; the agent works
inside Anthropic's sandbox. Relevant for gastown only if workloads can run without
access to the local worktree.

### 1.9 Go Community SDKs

Four community Go wrappers exist. All wrap the `claude` CLI as a subprocess
communicating via NDJSON over stdin/stdout. None call the Anthropic HTTP API directly.

| Repo | Published | Feature Status |
|------|-----------|----------------|
| `schlunsen/claude-agent-sdk-go` | Pre-2026 | Maintained; hooks, sessions |
| `partio-io/claude-agent-sdk-go` | Active | Full parity (hooks, MCP, subagents) |
| `ProjAnvil/claude-agent-sdk-golang` | 2026-05-07 | New; mirrors Python API |
| `M1n9X/claude-agent-sdk-go` | 2026-01-09 | Basic parity |

The best-featured is `partio-io/claude-agent-sdk-go` (Go 1.26+), which exposes:
- `HookPreToolUse`, `HookPostToolUse`, `HookPostToolUseFailure`, `HookStop`,
  `HookSubagentStart`, `HookSubagentStop`, `HookPreCompact`, `HookNotification`
- `HookOutput` fields: `Decision`, `DecisionReason`, `UpdatedInput`, `AdditionalContext`,
  `SystemMessage`, `Continue`, `SuppressOutput`, `BlockStop`
- MCP: stdio, SSE, and HTTP transport types
- Subagent definitions with per-agent model, system prompt, tool restrictions
- Session resume and fork
- Structured output (`WithOutputFormat`)

The SDK protocol: `claude --print --input-format stream-json --output-format stream-json`,
communicating via NDJSON over stdin/stdout. This is the same transport all four
community Go SDKs use.

---

## 2. Current Gastown Polecat-Spawn Architecture

### 2.1 Overview

A polecat is a persistent agent identity with an ephemeral session. Each polecat gets a
git worktree (`rig/polecats/<name>/<rigname>/`) and a tmux session. The Claude binary
runs as the initial process of that tmux pane.

```
gt sling <rig> <bead>
    │
    ├─ SpawnPolecatForSling()          [internal/cmd/polecat_spawn.go]
    │   ├─ CheckDoltHealth()           pre-spawn health gate
    │   ├─ CheckDoltServerCapacity()   admission control
    │   ├─ countWorkingPolecats()      cap at 25 working
    │   ├─ witness.ShouldBlockRespawn() per-bead circuit breaker
    │   ├─ FindIdlePolecat()           reuse existing if available
    │   │   ├─ ReuseIdlePolecat()      branch-only reuse (fast path)
    │   │   └─ RepairWorktreeWithOptions() full repair (slow path)
    │   └─ AllocateAndAdd()            allocate new name + worktree
    │       └─ returns SpawnedPolecatInfo{SessionName, ClonePath, ...}
    │
    └─ StartSession()                  [internal/cmd/polecat_spawn.go]
        └─ SessionManager.Start()      [internal/polecat/session_manager.go]
            │
            ├─ config.ResolveRoleAgentConfig("polecat", ...)
            ├─ runtime.EnsureSettingsForRole()   write settings.json
            ├─ session.FormatStartupBeacon()     build initial prompt
            ├─ config.BuildStartupCommandFromConfig()
            │   └─ produces:
            │      "exec env GT_ROLE=polecat GT_RIG=<rig> ... \
            │       claude --dangerously-skip-permissions \
            │       --settings /path/to/settings.json \
            │       '<beacon prompt>'"
            │
            ├─ tmux.NewSessionWithCommandAndEnv(sessionID, workDir, command, envVars)
            │   └─ tmux new-session -d -s <sessionID> -c <workDir>
            │      tmux set-environment -e flags per envVar
            │      tmux respawn-pane -k -t <sessionID> <command>
            │
            ├─ tmux.WaitForCommand()          poll: shell running?
            ├─ tmux.AcceptStartupDialogs()    handle workspace trust dialog
            ├─ tmux.WaitForRuntimeReady()     poll for "❯ " prompt prefix
            ├─ nudge delivery                 tmux.NudgeSession() → send-keys
            ├─ verifyStartupNudgeDelivery()   async retry loop (GH#1379 / GH#3031)
            └─ session.RecordAgentInstantiateFromDir()  GASTA telemetry
```

### 2.2 Key Files

| File | Role |
|------|------|
| `internal/polecat/session_manager.go` | Session lifecycle: Start, Stop, Inject, Attach, Capture |
| `internal/polecat/manager.go` | Polecat lifecycle: Add, Remove, AllocateAndAdd, FindIdlePolecat |
| `internal/polecat/types.go` | `State`, `Polecat`, `CleanupStatus` value types |
| `internal/cmd/polecat_spawn.go` | `SpawnPolecatForSling`, `StartSession` — gt sling entry point |
| `internal/cmd/polecat.go` | CLI cobra commands: list, add, remove, start, stop, attach, inject |
| `internal/config/loader.go` | `BuildStartupCommandFromConfig` — assembles the claude command string |
| `internal/config/env.go` | `AgentEnv`, `BuildStartupCommandWithEnv` — env var injection |
| `internal/tmux/tmux.go` | `NewSessionWithCommandAndEnv`, `WaitForRuntimeReady`, `NudgeSession` |
| `internal/cmd/tap.go` | Hook registry: PreToolUse/PostToolUse dispatch to `gt tap guard` |

### 2.3 Environment Variable Protocol

gastown injects agent identity and runtime config through environment variables baked
into the tmux session at creation (`-e` flags):

| Variable | Purpose |
|----------|---------|
| `GT_ROLE` | `polecat` — agent role |
| `GT_RIG` | Rig name |
| `GT_POLECAT` | Polecat name |
| `GT_AGENT` | Resolved agent name (claude, codex, gemini, …) |
| `GT_POLECAT_PATH` | Absolute path to polecat's git worktree |
| `GT_BRANCH` | Active git branch (for `gt done` fallback) |
| `GT_RUN` | UUID run ID for GASTA telemetry |
| `GT_PROCESS_NAMES` | Comma-separated process names for liveness detection |
| `POLECAT_SLOT` | Integer slot for port offsetting |
| `ANTHROPIC_API_KEY` | Passed through; may be rotated via quota layer |
| `BD_ACTOR` | Beads actor identity (`polecat/<name>`) |

### 2.4 Hook Dispatch (Current Model)

gastown's existing hook model (`internal/cmd/tap.go`) generates
`.claude/settings.json` entries of the form:

```json
{
  "hooks": {
    "PreToolUse": [{
      "hooks": [{"type": "command", "command": "gt tap guard"}]
    }]
  }
}
```

When Claude Code fires a hook, it calls `gt tap guard` as a child process, passing the
hook payload as JSON on stdin. `gt tap guard` reads the JSON, evaluates guard rules,
and exits with a code (0=allow, 2=deny). This model crosses two process boundaries
(Claude → gt tap guard → back) for every hook event. Each round-trip costs ~20–50 ms
and spawns a new process.

### 2.5 Session Liveness and Respawn

The Witness process monitors session health via:
- `tmux.IsAgentAlive(sessionID)` — checks that the `$GT_AGENT` process (e.g., `node` +
  `claude`) is alive in the pane
- `tmux.IsIdle(sessionID)` — checks that the "esc to interrupt" busy indicator is absent
- `heartbeat.TouchSessionHeartbeat()` — filesystem timestamp updated on every `gt` command

When the Witness detects a stalled polecat (session dead, bead still hooked), it
initiates respawn via `gt sling`. The circuit breaker in `polecat_spawn.go` tracks
respawn counts per bead and blocks after N attempts to prevent feedback loops.

### 2.6 Multi-Agent Support

gastown supports non-Claude agents (Codex, Gemini, OpenCode) via the same tmux
subprocess model. `GT_AGENT` and `runtimeConfig.ResolvedAgent` select the correct
process names for liveness detection and the correct readiness strategy (prompt-polling
vs. delay-based for agents without a "❯ " prefix). The startup command is built by
`config.BuildStartupCommandFromConfig`, which reads `role_agents` from
`settings/config.json`.

---

## 3. The Gap: What the SDK Exposes That CLI-via-tmux Cannot

### 3.1 Structured Tool Dispatch (In-Process vs. Subprocess Hooks)

**CLI-via-tmux model:** Hook events are serialized to JSON, written to a subprocess
(`gt tap guard`), which reads stdin, evaluates rules, and exits. Two process forks per
hook event; measured latency ~20–50 ms per call.

**SDK model:** Hook callbacks are in-process function calls. `PreToolUse` fires with
zero IPC overhead. The callback can read internal Go/Python state, call databases,
mutate shared memory, and return a structured decision — all without spawning a child
process.

**Impact for gastown:** Every `bd update`, every beads state transition, every
`gt prime` delivery currently crosses 2–3 subprocess boundaries. With SDK hooks these
become function calls.

### 3.2 Bidirectional Structured Control (vs. tmux send-keys)

**CLI-via-tmux model:** gastown sends instructions to Claude by pasting text into the
tmux pane via `tmux send-keys`. Delivery is best-effort. The
`verifyStartupNudgeDelivery` function (session_manager.go:900+) exists specifically
because paste-based delivery is unreliable: it must retry up to N times, polling for
`IsIdle` to confirm delivery. GH#1379 and GH#3031 are both regressions in this layer.

**SDK model:** The caller sends a new `query(prompt)` call and gets back a typed
message stream. Delivery is guaranteed by the protocol. No paste, no polling, no
retry loop.

### 3.3 Programmatic Session Resume and Fork

**CLI-via-tmux:** `--resume <session_id>` is passed as a CLI flag. Session state is
opaque to gastown — it cannot inspect what was done, enumerate turns, or branch from a
known checkpoint.

**SDK:** `resume=session_id` and `fork_session=True` are first-class options. The
session JSONL is readable; gastown could inspect turn history, rewind, or branch from a
checkpoint without killing the session.

### 3.4 Structured Output

The SDK supports `output_format` (structured JSON schema) so the agent's final answer
can be machine-parsed without regex extraction over pane content. Currently gastown
reads results from the JSONL conversation log file, which requires file I/O and parsing
heuristics.

### 3.5 No Zombie Storms

**CLI-via-tmux:** When a pane dies abnormally the tmux session remains, `IsAgentAlive`
returns false, and the Witness must detect and clean up stale sessions. The circuit
breaker in `polecat_spawn.go` exists to prevent respawn storms.

**SDK:** The agent loop is a goroutine (or async generator). When it terminates for any
reason — crash, max turns, max budget — the caller gets a `ResultMessage` with
`subtype="error_during_execution"` or `"error_max_turns"`. No polling required; no
tmux sessions to clean up; no zombie detection logic needed.

### 3.6 Budget Controls as First-Class API

The SDK accepts `max_turns` and `max_budget_usd` per query. gastown currently has no
per-polecat cost budget enforced at the agent level; cost tracking is observational
(parsing JSONL telemetry files after the fact).

### 3.7 Subagent Spawning Without New tmux Sessions

With the SDK, a parent agent spawning a subagent (via the `Agent` tool) creates a new
in-process task — not a new tmux session. All subagent messages arrive on the same
typed stream, tagged with `parent_tool_use_id`. gastown's current model creates a
separate tmux session per agent and has no mechanism to receive structured output back
from it.

### 3.8 Respawn Overhead Elimination

Current polecat startup: worktree create (~5s fast path after the persistent-polecat
optimization), tmux session creation, Claude binary load (Node.js startup ~2–3s),
`WaitForRuntimeReady` polling (~5–30s), nudge delivery + verification. Total cold-start
wall time is typically 15–60s.

SDK model: a `query()` call starts the agent loop in ~1–2s (CLI binary still starts as
a subprocess, but there is no tmux, no pane health-check, no nudge delivery layer).

---

## 4. What You Lose by Ditching tmux + CLI

### 4.1 Operator Visibility (`tmux attach`)

The most concrete operational loss. When a polecat is running inside tmux, any operator
can run `tmux attach -t gt-<rig>-<polecat>` to watch the agent work in real time. This
is the primary debugging surface: operators see exactly what Claude is typing, what it
ran, what failed, and can intervene by typing `C-c` or injecting text.

With a pure SDK session, the equivalent is streaming the `AssistantMessage` sequence
to a terminal or dashboard, but it lacks the interactive "just attach and watch"
ergonomic that the tmux model provides for free.

### 4.2 Session Persistence Across Gastown Process Restarts

**CLI-via-tmux:** The Claude process is independent of the `gt` binary. If the `gt`
daemon dies (or the operator kills it), the polecat session keeps running in tmux
untouched. On restart, `gt` reconnects by name: `tmux has-session -t <name>`.

**SDK model:** The agent loop lives inside the `gt` process (or a sidecar). If `gt`
dies, the loop dies. Resuming an in-flight task requires re-issuing the prompt or
implementing a checkpoint/resume protocol on top of the SDK session API.

### 4.3 Native Claude Code Binary Features

The `claude` CLI binary manages its own Node.js runtime, ships with
`--dangerously-skip-permissions`, handles the workspace-trust dialog, and auto-updates.
The SDK runs the same binary as a subprocess internally, but features that require
interactive terminal handling (workspace-trust UI, OAuth device flow) become the SDK
caller's responsibility to automate.

### 4.4 Multi-Window Sessions and tmux Workflow

Operators open multiple tmux windows inside a polecat session (Claude in one pane, bash
for inspection in another). The SDK model delivers a single I/O stream; the
multi-window workspace is not replicable without reimplementing it on top of the SDK.

### 4.5 Proven Operational Tooling

gastown has approximately two years of hardening around the tmux model:
- `gt polecat attach` / `gt session capture` / `gt polecat inject`
- Witness patrol: liveness checks, zombie detection, respawn circuit breaker
- `POLECAT_SLOT`-based port isolation
- `GT_PANE_ID` declared-pane liveness
- `tmux.IsIdle` vs `tmux.IsAtPrompt` distinction (GH#3031)
- `AcceptStartupDialogs` for workspace-trust

All of this would need to be rebuilt or discarded for a pure-SDK model.

### 4.6 Agent Portability (Codex, Gemini, OpenCode)

gastown's abstraction layer (`role_agents`, `ResolvedAgent`, `GT_AGENT`) lets the same
spawn infrastructure run Codex, Gemini, or OpenCode by swapping the command and process
names. The Claude Agent SDK is Claude-only. Supporting non-Claude agents would require
a parallel code path.

---

*End of survey document.*
