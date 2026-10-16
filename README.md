# lazyplaner

A terminal planner that uses a local Ollama model to write a markdown
description for each plan you create. Plans are stored in SQLite.

## Running

```sh
go run .
```

Settings come from environment variables (see `internal/config`):

| Variable           | Default                  |
|--------------------|--------------------------|
| `LAZYPLANER_DB_PATH` | `./lazyplaner.db`      |
| `OLLAMA_BASE_URL`  | `http://localhost:11434` |
| `OLLAMA_MODEL`     | the model picked with `ctrl+o` (saved), else `deepseek-v2:latest` |

## CLI

With a subcommand, `lazyplaner` runs without the UI and prints one JSON object.
It uses the same database and game rules as the TUI, and can run while the TUI
is open (the database is in WAL mode). `lazyplaner help` prints the usage:

```text
usage: lazyplaner                 open the terminal UI
       lazyplaner status
       lazyplaner tree <skill>
       lazyplaner apply-blueprint (--file <path> | --json '<json>') [--replace]
       lazyplaner expand-tree <skill> (--file <path> | --json '<json>')
       lazyplaner set-tree <skill> --level <1-5> --nodes-json '<json>' [--goal "<goal>"] [--category <cat>] [--frozen] [--replace]
       lazyplaner set-directives --json '<json>' [--replace]
       lazyplaner complete <directive_id> [--note "<what you did>"]
       lazyplaner add-plan --subject "<subject>" --desc "<description>" [--no-xp]
       lazyplaner add-note "<text>" [--no-analyze]
Every command prints JSON. A JSON value in a flag may be - to read it from stdin.
```

### Commands

| Command | Does |
|---------|------|
| `status` | Character level and rank, streak, score, every skill (`ACTIVE`/`FROZEN`, level, era, `can_expand`) with its open node ids and next curated task, today's directives (`OPEN`, `IN_PROGRESS`, `DONE`), the 5 newest notes |
| `tree "<skill>"` | Every node of one skill across all eras: id, era, parents, state (`DONE`, `OPEN`, `RUSTY`, `LOCKED`, `SHROUDED`), capstone flag and curated task queue |
| `apply-blueprint` | One shot for a new skill: an 8-node tree, 2-4 curated tasks per node and a master plan, saved all or nothing. `--replace` wipes an existing skill's tree (it keeps its XP) |
| `expand-tree "<skill>"` | Appends the next 8-node era onto the latest capstone, once that capstone is done (or one node is left). Nothing is deleted |
| `set-tree "<skill>"` | Creates a skill from 8 nodes without tasks or a plan; `--replace` swaps an existing tree |
| `set-directives` | Sets today's set: one `recon`, one or two `core_op`, one `deep_push`, or one or two `recovery`. `--replace` swaps a set with nothing done yet |
| `complete <id>` | Finishes a directive and awards its XP; `--note` keeps what was done with the award |
| `add-plan` | Saves a plan; the local model then classifies it for XP (`--no-xp` skips that) |
| `add-note "<text>"` | Saves a field note; the local model reads it (`--no-analyze` skips that) |

### Examples

```sh
lazyplaner status | jq '.skills[] | {name, state, level, open: [.open_nodes[].name]}'
lazyplaner tree "Java"
lazyplaner apply-blueprint --file java.json
cat era2.json | lazyplaner expand-tree "Java" --json -
echo '[{"tier":"recon","node_id":4,"title":"Ghosted Line Drills"},
       {"tier":"core_op","node_id":5,"title":"Gradle Init App"},
       {"tier":"deep_push","node_id":4,"title":"Clean Contour Copy"}]' | lazyplaner set-directives --json -
lazyplaner complete 12 --note "worker pool passes -race with 8 workers"
lazyplaner add-note "Sick today, flu" --no-analyze
```

### Output and errors

- Success: `{"ok": true, ...}`, exit code 0.
- A failed action: `{"ok": false, "error": "..."}`, exit code 1. The error says
  what to fix (`node "X" task 2: title "..." is 6 words; use 2 to 5 words ...`).
- A bad command line: the same, plus `"usage"`, exit code 2.
- `add-plan` and `add-note` save even when the model is down; the JSON then
  carries `xp_error` or `analysis_error`.

### Rules the CLI enforces

- **Database:** `LAZYPLANER_DB_PATH`, default `./lazyplaner.db` in the
  current directory. Run from where you run the TUI, or a new empty game is
  created.
- **Trees** are 8 nodes per era, wired as a closed diamond (2 → 3 → 2 → 1;
  a node with two parents opens when both are done). Grow a finished tree
  with `expand-tree`, not `--replace`.
- **Directive and task texts:** `title` is 2-5 words and at most 32
  characters ("Ghosted Line Drills"); `description` is one sentence, at most
  140 characters; `details` is an optional markdown guide of 3-4 bullets, at
  most 900 characters. Unknown JSON fields are refused.
- **At most 3 active skills.** A new skill past the cap starts `FROZEN` and the
  JSON carries a `note`; swap skills in the TUI (`c`, then `f`).
- **At most 5 open directives a day.** Directives only target `OPEN` or
  `RUSTY` nodes of active skills.

The full JSON schemas are in `SKILL.md`, which teaches an agent such as
Claude Code to drive these commands. To make it available to Claude Code in
any directory:
`mkdir -p ~/.claude/skills/lazyplaner-strategist && ln -s "$PWD/SKILL.md" ~/.claude/skills/lazyplaner-strategist/SKILL.md`.

**Hybrid planning:** a frontier agent designs a skill once with
`apply-blueprint`: the tree, a master plan in the plan list, and a queue of
curated tasks on each node. In the TUI, `g` then fills each slot of the day's
set with the next unfinished task of an open node, and asks the local model only
for slots the queue cannot fill. Completing a queued directive completes its
task. On a sick or exhausted day the set eases into the next task instead.

## Keys

| Key        | Action                                  |
|------------|-----------------------------------------|
| `ctrl+n`   | New plan (subject and description)      |
| `ctrl+o`   | Select the model used for generation    |
| `enter`    | Read the selected plan, its markdown rendered (`j/k` scroll, `e` edit, `esc` back) |
| `e`        | Edit the selected plan in `$EDITOR`     |
| `d`        | Delete the selected plan (asks `y` to confirm) |
| `c`        | Character sheet and road (skills, level, XP history); `f` makes the selected skill active or frozen (3 active at most) |
| `t`        | Skill tree for each skill (`tab` switches skill, `hjkl` moves, `[`/`]` jump between eras, `g` a focus directive for the node, `x` expands a finished era, `e` edits the skill) |
| `g`        | Today's directives, in the home panel: generates the day's set (⚡ Recon, 🔧 Core Op, 🔥 Deep Push); once all are done, one ★ bonus per day. `r` rerolls the highlighted open directive. `enter` starts it (`[▶]`), and `enter` again finishes it with a one-line proof note |
| `n`        | Field log: dated daily notes the model reads. A sick or burnt-out day shields the streak and makes the next set light; `a` new, `e` edit, `d` delete, `x` analyze, `c`/`enter` claim XP for work in the note, `p` turn it into a plan |
| `s`        | Character setup (start skills and levels) |
| `h`        | Help: what you can type, levels, categories, plans, keys (←/→ switches section) |
| `↑` / `↓`  | Move through the plan list              |
| `q`        | Quit                                    |

In the New Plan window: `tab` switches fields, `enter` moves from subject to
description, `ctrl+s` generates the plan, and `esc` cancels. In the model
picker: `enter` selects, `r` refreshes, and `esc` closes.

## Creating your character

On first start (or with `s`) the setup screen asks for your starting skills.
Press `?` there for the same help in the app.

**Add a skill you practise or want to build**, such as `Go`, `Running`,
`Drawing`, or `Public speaking`.

- 1 to 3 words, up to 40 characters.
- Letters, numbers, spaces, and `+ # . - / & '` only (`C++`, `CI/CD`, `R&D`).
- Skills can't repeat. `Go` and `go` are the same skill.
- Up to 8 skills per setup. Add more later with `s`.

**Don't add** goals or sentences (`get fit by June` → use `Fitness`), one-off
tasks (`buy milk` belongs in a plan), or emoji and symbols like `! ? @ %`.

Each skill gets a tree built from basics to advanced. The level you pick
(1 Beginner to 5 Expert) is where you start on that tree. The AI chooses the
skill's category (Mind, Body, Craft, Social or Life) when it builds the tree,
so you don't pick one.

**Editing.** In setup, tab to the list, pick a skill with `↑/↓`, and press
enter to change it. `ctrl+d` removes the selected skill. On the tree screen,
`e` renames a skill. Saving that edit resets the
skill to level 1: its XP and node progress go to 0. The XP history and the
tree are kept.

## Project layout

```
main.go                  Entry point: loads config, opens the store, runs the CLI or the TUI
internal/cli/            Headless subcommands that print JSON (see "CLI")
internal/game/           Game actions shared by the CLI and the TUI (blueprints, eras, days)
internal/rpg/            Game rules: XP, levels, tree shape, directives, title limits
internal/config/         Reads settings from environment variables
internal/db/             SQLite store: plans, skills, trees and eras, directives, notes
internal/llm/            Ollama HTTP client (list models, generate description)
internal/ui/             Bubble Tea interface
  model.go               Model state, constructor, Init/Update dispatch
  messages.go            Async result messages (loaded, saved, updated, editor done)
  keys.go                Key handling for the plan list
  newplan.go             New Plan window (subject, description, submit)
  modelpicker.go         Model picker window
  actions.go             Submit a plan, select a model
  editor.go              Edit a plan description in $VISUAL/$EDITOR
  planview.go            Full-screen plan reader with rendered markdown
  proof.go               Start / finish a directive with a proof note
  commands.go            tea.Cmd wrappers that call db and llm off the UI loop
  state.go               Updating the model's plan and model lists
  items.go               List item types for plans and models
  view.go                Renders the full screen and its content
  panel.go               Draws bordered panels with embedded titles
  layout.go              Panel sizes and column widths
  styles.go              Colors and lipgloss styles
```

## How data flows

1. `Update` handles a key or message and returns a `tea.Cmd`.
2. Commands run `db` or `llm` calls in the background and return a message.
3. `Update` handles that message and changes the model state.
4. `View` renders the state. It never performs I/O.
