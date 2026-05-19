# Gastown Positioning Analysis: Gastown vs CLI-Anything

**Date:** 2026-05-19
**Author:** Agent A (research swarm)
**Branch:** swarm/research-cli-anything
**Companion doc:** `cli-anything-integration-2026-05-19.md`

---

## 1. What Problem Each Project Solves

### Gastown solves agent fleet management

The README states it directly:

> "Gas Town is a workspace manager that lets you coordinate multiple AI coding
> agents (Claude Code, GitHub Copilot, Codex, Gemini, and others) working on
> different tasks. Instead of losing context when agents restart, Gas Town persists
> work state in git-backed hooks, enabling reliable multi-agent workflows."

The four challenges gastown addresses:

| Challenge | Gastown Solution |
|-----------|-----------------|
| Agents lose context on restart | Work persists in git-backed hooks |
| Manual agent coordination | Built-in mailboxes, identities, handoffs |
| 4-10 agents become chaotic | Scale comfortably to 20-30 agents |
| Work state lost in agent memory | Work state stored in beads ledger |

Gastown is an **operating system for agent fleets**. Its core abstraction is
the persistent role (Mayor, Witness, Polecat, Deacon, Dogs, Refinery) with
clearly bounded responsibilities. Agents are processes; hooks are file system;
bd is the database; the formula engine is the shell scripting layer.

### CLI-Anything solves software interface wrapping

The CLI-Anything README states: "Today's Software Serves Humans. Tomorrow's
Users will be Agents. CLI-Anything: Bridging the Gap Between AI Agents and the
World's Software."

CLI-Anything is a **tool registry for agent-callable software**. Its core
abstraction is the SKILL.md — a structured, agent-readable definition of what
commands a software application supports. The 2,269-test harness, the pip
package per application, and the CLI-Hub package manager are all infrastructure
for making that tool registry reliable and discoverable.

### The orthogonality claim

At first pass, these are orthogonal:
- Gastown answers: "How do I coordinate 20 agents working on a codebase?"
- CLI-Anything answers: "How do I make Blender/Obsidian/Godot callable by an agent?"

But the orthogonality breaks down at the action layer. A gastown polecat doing
design work needs tools. Right now its tools are: the file system, git, `gt`
subcommands, and whatever the rig's language toolchain provides. CLI-Anything
offers a vetted, tested catalog of 60-plus additional tools.

---

## 2. Where They Overlap

### Both define structured ways for agents to take action

Gastown formulas and CLI-Anything SKILL.md files are both authored by humans,
read by agents, and define available operations. They live at the same layer
in the cognitive stack: "what can I do next?"

The surface similarity:

| Concept | Gastown | CLI-Anything |
|---------|---------|-------------|
| Unit of capability | Formula (TOML) | SKILL.md (Markdown plus YAML) |
| Discovery | `gt formula list` | `cli-hub list` |
| Inspection | `gt formula show <name>` | `cli-hub info <name>` |
| Execution | `gt formula run <name>` | `cli-anything-<name> <cmd>` |
| Agent prompt injection | `gt prime` injects role template | SKILL.md loaded into agent context |

### Both target Claude Code and its siblings

Gastown's default runtime is Claude Code. CLI-Anything's hub explicitly lists
"Claude Code, Cursor, OpenClaw, nanobot" as target agents. This is the same
audience, potentially competing for agent attention.

### Both use the `skills/` convention (Claude Code)

Gastown (via the operator's `~/.claude/skills/` directory) and CLI-Anything
(via the `skills/cli-anything-*/SKILL.md` paths in their repo) both publish
skill files that Claude Code can load. The path convention is identical:
`npx skills add HKUDS/CLI-Anything --skill <name>` installs to the same place
as gastown's own skills.

This is the most direct collision point: if both projects publish SKILL.md
files to `~/.claude/skills/`, an agent session could have gastown formula
skills and CLI-Anything tool skills sitting in the same directory.

---

## 3. Where They Do Not Overlap

### Gastown owns the fleet

CLI-Anything has no concept of multiple concurrent agents, session handoff,
polecat lifecycle, or merge queues. It is fundamentally single-agent: one
CLI invocation at a time. The REPL mode is interactive, but it is still one
operator or agent working one tool session.

Gastown's value proposition — 20-30 agents coordinated without chaos — has
no analog in CLI-Anything.

### CLI-Anything owns the long tail of software integrations

Gastown formulas do not wrap Blender, Obsidian, Godot, ComfyUI, MuseScore,
or any of the 60-plus application CLIs in CLI-Anything. Gastown formulas
describe workflows (design, review, release). They assume the agent knows how
to use the underlying software through its native interface or the standard shell.

CLI-Anything's investment in tested, per-application harnesses (2,269 tests)
represents years of knowledge about how to wrap GUI software reliably. Gastown
has no equivalent and no incentive to build it.

### Gastown owns persistent work state

CLI-Anything has no persistent work bus. Agent state lives in the JSON project
file the agent manages by hand. There is no equivalent of bd (Dolt-backed issue
tracking), no equivalent of the beads ledger, no convoy tracking, no mail system.

If a CLI-Anything agent session crashes mid-workflow, it re-reads the project
JSON and tries to reconstruct context. A gastown polecat can crash and resume
from the last molecule checkpoint because the work bus is separate from the
agent session.

### Gastown owns the control loop

Witness, Deacon, Dogs, Refinery — the four-layer watchdog system — has no
analog in CLI-Anything. CLI-Anything assumes the human or orchestrating agent
monitors health. Gastown builds the monitor into the system.

---

## 4. The Symbiosis Hypothesis

**Core thesis:** A polecat dispatched by gastown can `pip install cli-anything-<name>`
and immediately use that CLI in its work. Gastown becomes the orchestrator,
CLI-Anything becomes the tool registry.

The integration shape:

```
Operator tells Mayor: "Create a Blender scene for the product demo"
       |
Mayor pours mol-idea-to-plan -> creates beads for the scene work
       |
Mayor slings bead to a polecat with a prompt that includes
cli-anything-blender SKILL.md (injected from ~/.claude/skills/)
       |
Polecat runs:
  pip install cli-anything-blender  (or tool cache hit)
  cli-anything-blender scene new -o demo.blend-cli.json
  cli-anything-blender object add --type cube --name MainProduct
  cli-anything-blender material create --name ProductMaterial
  cli-anything-blender render execute --output demo.png
       |
Polecat runs gt done -> Refinery merges demo.png commit
       |
Mayor reports completion to operator
```

Why this is non-trivial:
- The polecat's SKILL.md context (injected via `gt prime`) already knows what
  gastown tools to use. The CLI-Anything SKILL.md adds knowledge of Blender's
  command surface without requiring the polecat to discover it.
- The work is tracked in bd: the bead has acceptance criteria, the molecule
  has checkpoints, the Refinery verifies the output exists before merging.
- If the polecat crashes mid-scene, it resumes from the last molecule step:
  the Blender JSON project file is still on disk and the polecat re-reads it.

### The formula-as-orchestrator pattern

A gastown formula step could specifically provision a CLI-Anything tool:

```toml
[[steps]]
id = "provision-blender-cli"
title = "Install Blender CLI harness"
description = """
Install the CLI-Anything Blender harness for this work session.

Run:
  pip install cli-anything-blender

Verify:
  cli-anything-blender --help

If install fails, escalate via gt escalate with severity=MEDIUM.
"""
acceptance = "cli-anything-blender --help exits 0"

[[steps]]
id = "create-scene"
needs = ["provision-blender-cli"]
title = "Create scene using Blender CLI"
...
```

This makes the CLI-Anything dependency explicit in the workflow definition,
not assumed by the polecat. The formula becomes self-documenting about its
external tool dependencies.

### Bidirectional value

The symbiosis runs in both directions:

- **Gastown benefits:** Polecats gain access to 60-plus tested, agent-callable
  application CLIs without gastown having to build or maintain them.
- **CLI-Anything benefits:** Gastown provides the persistent work tracking,
  multi-agent coordination, and crash recovery that CLI-Anything completely
  lacks. A CLI-Anything workflow inside gastown is more reliable than one
  run ad hoc.

---

## 5. Risks to This Thesis

### Risk 1: Operator-mindshare overlap

Both projects publish skills to `~/.claude/skills/`. An operator configuring
both systems would have formula skills (`shiny`, `code-review`) and tool skills
(`cli-anything-blender`, `cli-anything-drawio`) in the same skills directory.
Claude Code loads all skills at session start. At scale (60-plus CLI-Anything
skills plus gastown formulas), skill context becomes a token budget concern.

**Mitigation:** Gastown's `gt prime` injects role context selectively. Only
the skills relevant to the current rig and task need to be in context. A scoped
injection mechanism (a formula step specifying `tools: [blender]`) would limit
context bloat to what the polecat actually needs.

### Risk 2: Formula and SKILL drift

CLI-Anything SKILL.md files track their CLI versions via pip package versions.
Gastown formulas are embedded in the binary. If a formula step invokes
`cli-anything-blender scene new` and the blender CLI changes its interface,
the formula breaks silently — the formula has no declared dependency on
the cli-anything-blender version.

**Mitigation:** Formula steps that use CLI-Anything tools should pin the
package version in the provisioning step:
```
pip install 'cli-anything-blender==0.2.1'
```
This makes the dependency explicit and auditable.

### Risk 3: Who owns the agent prompts?

A polecat working on a Blender scene has two knowledge sources:
- Its gastown role template (from `gt prime`, defines how to be a polecat)
- The CLI-Anything SKILL.md (defines what Blender commands are available)

These can conflict. If the role template says "use JSON output for all tool
calls" but the CLI-Anything SKILL.md says nothing about the `--json` flag
convention, the polecat may make inconsistent choices.

**Mitigation:** The formula step description should be the authoritative prompt.
It should explicitly say: "Use `cli-anything-blender --json` for all calls.
Parse JSON output. Write results to the project file." The SKILL.md is
reference, not instruction.

### Risk 4: Pip environment isolation in polecats

Polecats run in git worktrees. There is currently no mechanism to provision
pip packages in a polecat's environment before dispatch. `pip install` run
inside a polecat session modifies the system or user Python environment,
not an isolated per-polecat virtualenv.

At scale (10 polecats each installing different CLI-Anything tools), this
creates version conflicts and unpredictable behavior.

**Mitigation:** A shared tool cache at the rig level — e.g.,
`~/gt/<rig>/.cli-anything-cache/` — with a `gt tool install <name>` command
that polecats can call to activate an already-installed CLI. Polecats check
the cache before calling pip. This is a gastown infrastructure feature, not
a CLI-Anything change.

---

## 6. Concrete Next-Step Shortlist

1. **Write 5 gastown formula SKILL.md files** (`shiny`, `code-review`, `tdd-cycle`,
   `design`, `rule-of-five`) following CLI-Anything's format. Submit to CLI-Hub
   registry. Validate that `cli-hub info gastown-formula-shiny` returns useful
   output. Cost: 1-2 days, text only.

2. **Write a `cli-anything-beads` Python harness** wrapping `bd create`,
   `bd list`, `bd show`, `bd comment`, `bd ready`. Test with 20 unit tests
   plus 5 E2E tests against a real beads instance. Submit to CLI-Hub.
   Cost: 3-5 days, Python.

3. **Create `mol-polecat-work-with-tools.formula.toml`** adding a
   `provision-tools` step before `load-context`, allowing the formula to
   specify a list of CLI-Anything tools to pip-install at the start of a
   polecat's work session. Cost: 1 day, TOML plus documentation.

4. **Prototype a polecat running a Blender task** using `cli-anything-blender`
   within a gastown rig. One demo bead ("Create a 3D cube scene and render to
   demo.png"), one formula step that provisions blender CLI, one formula step
   that does the scene work. Validates the full stack end-to-end.
   Cost: 2-3 days, requires Blender installed on the host.

5. **Add a `gt tool` subcommand** managing a per-rig CLI-Anything tool cache
   (install, list, check). Polecats call `gt tool install blender` instead of
   raw pip. The Mayor pre-provisions tools before slinging polecats.
   Cost: 3-4 days, Go.

---

**Decision pending:** Operator input needed to advance:
1. Is the symbiosis hypothesis correct — should polecats use CLI-Anything tools
   as a first-class part of their work, or is CLI-Anything meant for a different
   use case (human operators using GUI tools via agent assistance)?
2. Should gastown's formula system explicitly support external tool declarations
   (item 3 above), or is this premature given the unsolved tool-cache problem
   (item 5)?
3. Which demo use case would be most convincing for the end-to-end prototype:
   Blender (creative), Obsidian (knowledge management), or n8n (workflow automation)?
