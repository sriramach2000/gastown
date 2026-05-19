# CLI-Anything Integration Analysis

**Date:** 2026-05-19
**Author:** Agent A (research swarm)
**Fork:** sriramach2000/CLI-Anything (Apache-2.0, forked from HKUDS/CLI-Anything)
**Branch:** swarm/research-cli-anything

---

## 1. What CLI-Anything Is

### Thesis

CLI-Anything's one-sentence thesis is on the tin: *"Making ALL Software Agent-Native."*
The bet is that AI agents (Claude Code, Cursor, OpenClaw, etc.) need a unified CLI
surface to operate real software. Instead of hoping every app ships a clean API,
CLI-Anything builds agent-callable Python CLIs that wrap each application's native
interface — whether that is an HTTP API, a Python scripting API, XML manipulation,
or headless Electron export.

### Structure

Each CLI lives in its own subdirectory:
```
CLI-Anything/
├── blender/
│   └── agent-harness/
│       ├── BLENDER.md          # Human-facing architecture and SOP
│       ├── cli_anything/       # Python Click package
│       └── setup.py
├── obsidian/
│   └── agent-harness/
│       ├── OBSIDIAN.md
│       ├── cli_anything/
│       └── setup.py
└── skills/
    ├── cli-anything-blender/
    │   └── SKILL.md            # Agent-facing discoverable definition
    ├── cli-anything-drawio/
    │   └── SKILL.md
    └── ...
```

There are two documentation layers:
- `<app>/agent-harness/<APP>.md` — the human-authored architecture and SOP doc
  (150-400 lines). Describes the strategy for wrapping the app, CLI command groups,
  render pipelines, registries of shapes/presets/modifiers, gap analysis.
- `skills/cli-anything-<app>/SKILL.md` — the agent-discoverable skill definition.
  YAML frontmatter (`name`, `description`) plus markdown command tables.
  This is what agents read when they do `cli-hub info <name>`.

### Scale (as of fork, 2026-04-18)

- Approximately 60 first-party CLIs in-repo: blender, obsidian, drawio, comfyui,
  inkscape, godot, gimp, krita, n8n, safari, freecad, musescore, kdenlive, lldb,
  mermaid, shotcut, zoom, zotero, wiremock, qgis, unimol-tools, and more.
- 2,269 tests passing across unit and E2E harnesses.
- Public registry (`public_registry.json`) extending coverage to npm/brew/pip
  packages not in-repo (feishu, wecom, minimax-cli, and others).
- CLI-Hub package manager: `pip install cli-anything-hub` then
  `cli-hub install <name>` resolves, installs, and tracks CLIs from the registry.

---

## 2. The SKILL.md Pattern

### Anatomy of a SKILL.md

A SKILL.md has two parts: a YAML frontmatter block and a markdown body.

**Frontmatter (machine-readable):**
```yaml
---
name: "cli-anything-blender"
description: >-
  Command-line interface for Blender - A stateful command-line interface
  for 3D scene editing ...
---
```

**Body (agent-readable):** Installation, usage examples, REPL mode note,
command-group tables with description columns. The agent reads this to know
what commands are available without having to introspect the binary.

### Example: cli-anything-blender SKILL.md (excerpt)

```markdown
## Command Groups

### Scene
| Command | Description |
|---------|-------------|
| `new`   | Create a new scene |
| `open`  | Open an existing scene |
| `save`  | Save the current scene |
| `info`  | Show scene information |

### Object Group
| Command | Description |
|---------|-------------|
| `add`   | Add a 3D primitive object |
| `remove`| Remove an object by index |
| `transform` | Transform an object (translate, rotate, scale) |
```

### Example: cli-anything-obsidian SKILL.md (excerpt)

```markdown
## Vault
| Command | Description |
|---------|-------------|
| `list`   | List files in the vault or a subdirectory |
| `read`   | Read the content of a note |
| `create` | Create a new note |
| `update` | Overwrite an existing note |
| `delete` | Delete a note from the vault |
| `append` | Append content to an existing note |

## Search
| Command | Description |
|---------|-------------|
| `query`  | Search using Obsidian query syntax |
| `simple` | Plain text search across the vault |
```

### Example: cli-anything-drawio SKILL.md (excerpt)

```markdown
## Shape
| Command | Description |
|---------|-------------|
| `add`    | Add a shape to the diagram |
| `remove` | Remove a shape by ID |
| `label`  | Update a shape's label text |
| `move`   | Move a shape to new coordinates |
| `resize` | Resize a shape |
| `types`  | List all available shape types |

## Connect
| Command | Description |
|---------|-------------|
| `add`    | Add a connector between two shapes |
| `remove` | Remove a connector by ID |
| `style`  | Set a style property on a connector |
| `styles` | List available edge styles |
```

### Key structural invariants across all SKILL.md files

1. **Stateful sessions** — every CLI supports a JSON project file as persistent
   state between commands. The agent maintains session state explicitly.
2. **REPL mode** — invoke the binary without a subcommand to enter an interactive
   REPL with tab-completion and history.
3. **`--json` flag** — all commands support machine-readable JSON output for
   agent consumption.
4. **Click CLI plus pip package** — the underlying harness is always a Python Click
   package, installable via `pip install cli-anything-<name>`.
5. **One command group per domain** — commands are grouped by concern (scene, object,
   material for blender; vault, search, note for obsidian; shape, connect, page for
   drawio). Flat namespaces within each group.

### The meta-skill pattern

The `cli-hub-meta-skill/SKILL.md` makes the hub itself agent-discoverable:
```markdown
## Quick Start
pip install cli-anything-hub
cli-hub list
cli-hub install gimp
cli-anything-gimp --json project create --name my-project
```
An agent with this skill loaded can bootstrap any other CLI without human
intervention — pure autonomous tool acquisition.

---

## 3. How Gastown Formulas Compare

### What a gastown formula is

A gastown formula is a TOML workflow template stored at
`internal/formula/formulas/*.formula.toml` and compiled into the `gt` binary.
It is instantiated as a **molecule** — a tracked, resumable execution of
the template's steps — by `gt formula run <name>`.

Three formula types exist:
- **workflow** — sequential/DAG steps, each step dispatches polecats or awaits
  bd mail, has `needs` dependencies, optional `interactive` gates.
- **convoy** — parallel legs dispatched simultaneously, synthesized at the end.
- **aspect** — AOP-style advice (`around/before/after`) that weaves into other
  formulas' steps via pointcuts.

### Example: `shiny.formula.toml` — the simple case

```toml
description = "Engineer in a Box - the canonical right way."
formula = "shiny"
type = "workflow"

[[steps]]
id = "design"
title = "Design {{feature}}"
description = "Think carefully about architecture before writing code..."
acceptance = "Design doc committed..."

[[steps]]
id = "implement"
needs = ["design"]
description = "Write the code for {{feature}}..."
```

### Example: `code-review.formula.toml` — the convoy pattern

10 parallel analysis legs (correctness, performance, security, elegance,
resilience, style, smells, wiring, commit-discipline, test-quality), each
spawning a separate polecat. A synthesis step combines findings. Presets
select leg subsets (gate, full, security-focused, refactor).

### Example: `mol-idea-to-plan.formula.toml` — the pipeline pattern

9-step pipeline: intake → prd-review (6 parallel polecats) → human-clarify
(interactive gate) → generate-plan (6 parallel polecats) → 3 rounds of PRD
alignment (2 polecats each) → 3 rounds of plan self-review (2 polecats each)
→ create-beads → 3 verify-beads passes. Total: 25-plus polecats dispatched,
one human gate, all results flowing through bd mail.

### Example: `security-audit.formula.toml` — the aspect pattern

```toml
type = "aspect"
[[pointcuts]]
glob = "implement"
[[pointcuts]]
glob = "submit"

[[advice]]
target = "implement"
[advice.around.before]
description = "Pre-implementation security check."
[advice.around.after]
description = "Post-implementation security scan (SAST)."
```

### Comparison table

| Dimension | CLI-Anything SKILL.md | Gastown Formula |
|-----------|----------------------|-----------------|
| **What it defines** | A single software's CLI surface (commands, groups, options) | A multi-step agent workflow (steps, legs, polecats) |
| **Granularity** | One skill = one application | One formula = one process (may span many apps/tools) |
| **Execution model** | `cli-anything-blender scene new` — stateless subprocess | `gt formula run shiny` — tracked molecule with bd state |
| **Persistence** | JSON project file (agent-managed) | Beads ledger plus git worktrees (system-managed) |
| **Parallelism** | None (single-threaded CLI) | Native (convoy type spawns N polecats in parallel) |
| **Resumability** | Manual (agent re-reads project file) | Built-in (molecule checkpoints, crash recovery) |
| **Discovery** | `cli-hub list`, `cli-hub info <name>` | `gt formula list`, `gt formula show <name>` |
| **Distribution** | pip package plus SKILL.md in github | Embedded in binary, 3-tier resolution (project/town/system) |
| **Audience** | AI agents using GUI software | Polecats executing agentic workflows |
| **Format** | YAML frontmatter plus Markdown | TOML with Go template syntax |
| **Tool wrapping** | Yes (Inkscape, Godot, Blender...) | No (formulas describe workflows, not tools) |
| **Agent prompting** | Implicit (SKILL.md is read by agent) | Explicit (step descriptions become polecat prompts) |

### The core distinction

A CLI-Anything SKILL.md answers: *"What can I do with this software?"*
A gastown formula answers: *"In what order should polecats do this work?"*

They are not competing definitions of the same thing. SKILL.md is a **tool spec**.
A formula is a **process spec**. The gap between them is exactly where integration
lives: a formula step can invoke a CLI-Anything CLI as its underlying tool.

---

## 4. Three Integration Paths

### Path (a): Adopt the SKILL.md pattern for new gastown formulas

**Hypothesis:** Gastown formulas are hard to discover from outside the `gt` binary.
If gastown published SKILL.md-compatible definitions for its formulas, any
CLI-Anything user (or agent using the hub meta-skill) could find and invoke them.

**What this means in practice:**
- Write `skills/gastown-formula-<name>/SKILL.md` for each formula, following
  CLI-Anything's YAML frontmatter plus command-table pattern.
- The "commands" map to: `gt formula run <name> --<var>=<val>`
- Publish to the CLI-Hub registry under a `gastown-formula-*` namespace.

**Pros:**
- Zero code change to gastown. Pure documentation work.
- Makes gastown formulas legible to CLI-Hub users and agents that use the hub
  meta-skill to discover tools.
- Low cost: one SKILL.md per formula (~50 lines each, ~60 formulas = 3K lines).
- Opens the door for the CLI-Hub community to write agents that use gastown
  workflows as building blocks.

**Cons:**
- Impedance mismatch: SKILL.md's command-table format is designed for one-shot
  subprocess calls. Gastown formulas are long-running, stateful, have human gates.
  A simple command table undersells the complexity.
- Formula variables (`--set key=val`, Go template syntax) do not map cleanly to
  CLI-Anything's Click option model.
- The agent reading a SKILL.md for `gt formula run mol-idea-to-plan` would need
  to understand bd mail, human gates, and polecat lifecycle — none of which can
  be captured in a SKILL.md command table.

**Verdict:** Worth doing for simple formulas (`shiny`, `code-review`, `tdd-cycle`).
Not worth forcing for pipeline formulas (`mol-idea-to-plan`). Selective, not
universal.

---

### Path (b): Publish gastown itself as a CLI-Anything entry (`gastown-cli`)

**Hypothesis:** The `gt` binary's own CLI surface is a candidate for wrapping.
A `cli-anything-gastown` harness would expose `gt sling`, `gt convoy`,
`gt formula run`, `gt mail`, `bd create/list/show` as an agent-callable CLI
with JSON output and a SKILL.md.

**What this means in practice:**
- Write a Python Click package (`cli-anything-gastown`) that delegates to `gt`
  and `bd` subprocess calls.
- Write a SKILL.md covering: convoy management, bead lifecycle, formula dispatch,
  mail, and agent status.
- Submit to the CLI-Hub registry.

**Pros:**
- `gt` already outputs JSON in most commands (`--json` flag on most subcommands).
- This gives non-gastown agents (running in other contexts such as Cursor or
  claude.ai) a bridge to gastown's orchestration.
- Community-facing: any CLI-Hub user can discover and use gastown without
  installing it natively.
- Forces dogfooding: writing the SKILL.md would surface gaps in `gt`'s JSON
  output surface.

**Cons:**
- `gt` requires a fully initialized gastown workspace (`~/gt/`, dolt, beads).
  The harness would need good error messages when the workspace is absent.
- `gt formula run` is complex to wrap: it creates molecules, dispatches polecats,
  and listens for bd mail events. A Click wrapper that hides this is either
  paper-thin (and useless) or has to implement event polling.
- Maintenance burden: every `gt` CLI change potentially breaks the harness.
- Risk of presenting gastown as "just another CLI tool" when its value is the
  persistent process model, not the command surface.

**Verdict:** Prototype-worthy for the `bd` surface (create/list/show/comment
are stateless enough to wrap cleanly). Harder for `gt` orchestration commands.
Start with `cli-anything-beads` before `cli-anything-gastown`.

---

### Path (c): Embed CLI-Hub launcher tiles in the gastown dashboard

**Hypothesis:** The gastown web dashboard (served on `DASHBOARD_PORT`) has panels
for rig status, convoy tracking, and agent health. Adding a CLI-Hub discovery
panel would let operators launch CLI-Anything polecats directly from the dashboard.

**What this means in practice:**
- Add a "Tools" or "CLI-Hub" tab to the dashboard that queries the CLI-Hub
  registry JSON and displays available CLIs by category.
- Each tile has an "Install" button (runs `pip install cli-anything-<name>`)
  and a "Launch" button (spawns a polecat with the corresponding SKILL.md
  pre-loaded and the CLI pre-installed in the polecat's environment).
- The polecat's prompt would include the target CLI's SKILL.md content,
  so it knows what commands are available without any extra discovery step.

**Pros:**
- The tightest user-visible integration. Operators get a single pane: see running
  polecats and available tool CLIs.
- Natural fit: gastown already tracks polecat sessions; adding "what tools is this
  polecat using" is a small extension.
- Zero change to the `gt` CLI. Pure dashboard work.
- The `cli-hub-meta-skill` already solves the discovery piece; the dashboard just
  surfaces it visually.

**Cons:**
- Dashboard work is frontend code (currently minimal Go SSE plus HTML). Adding a
  registry-browsing UI is a non-trivial frontend feature.
- "Install CLI then launch polecat" involves pip plus subprocess in a dashboard
  action, which adds infrastructure complexity (where does pip run — the polecat's
  env or the mayor's env?).
- The polecat sandbox model (git worktrees) does not naturally share pip environments.
  Each polecat would need its own CLI install or a shared tool cache.
- Locks gastown into periodically refreshing the CLI-Hub registry for the tile
  catalog.

**Verdict:** High visual impact, medium-high implementation cost. The shared
tool-cache problem (one pip install shared across polecats) is a prerequisite.
Defer to after Path (b) proves the tool-wrapping concept.

---

## 5. Recommendation

**Start with Path (a), selective SKILL.md publishing for 5-8 simple formulas.**

Rationale:
- Zero risk. Text-only work. No gastown code change required.
- Proves the discoverability hypothesis cheaply: can a CLI-Hub agent find
  and invoke `gt formula run shiny` from a SKILL.md? If yes, the integration
  thesis has legs.
- The right formulas to start: `shiny`, `code-review`, `tdd-cycle`, `design`,
  `rule-of-five`, `security-audit`. All are self-contained single-pass workflows
  with no human gates and simple variable sets.
- The SKILL.md writing process will also surface formula variable ergonomics that
  need improvement (currently `--set key=val` is the only injection mechanism,
  which is less clean than CLI-Anything's Click options).

**Then Path (b), starting with a `cli-anything-beads` harness.**

Rationale:
- `bd create/list/show/comment/ready` is the cleanest gastown surface to wrap.
  All stateless, all with JSON output, no daemon dependencies.
- This creates a repeatable pattern for wrapping gastown tooling, extensible
  later to `gt sling`, `gt convoy list`, and `gt mail inbox`.
- Low maintenance risk: beads is a stable, well-tested binary with a clean CLI
  surface.

**Path (c) (dashboard tiles) is the most user-visible but requires the shared
tool-cache problem to be solved first.** Defer to a later wave after the CLI
wrapping pattern is established.

---

**Decision pending:** Operator input needed to advance:
1. Is the 3-tier formula resolution (project/town/system) the right place to
   look for externally-contributed formula SKILL.md files, or should there be
   a separate `~/.gt/skills/` directory analogous to `~/.claude/skills/`?
2. For the `cli-anything-beads` harness: should it live in sriramach2000/CLI-Anything
   or in a separate repo under gastownhall/?
3. Should the gastown dashboard port serve a CLI-Hub registry proxy, or should
   polecat tool installs pull directly from the HKUDS upstream registry?
