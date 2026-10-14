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
| `OLLAMA_MODEL`     | `llama3.2`               |

## Keys

| Key        | Action                                  |
|------------|-----------------------------------------|
| `ctrl+n`   | New plan (subject and description)      |
| `ctrl+o`   | Select the model used for generation    |
| `e`        | Edit the selected plan in `$EDITOR`     |
| `d`        | Delete the selected plan (asks `y` to confirm) |
| `c`        | Character sheet and road (skills, level, XP history) |
| `t`        | Skill tree for each skill (←/→ switches skill, `e` edits it) |
| `g`        | Today's directives, in the home panel: generates the day's set (⚡ Recon, 🔧 Core Op, 🔥 Deep Push); once all are done, one ★ bonus per day. `r` rerolls the highlighted open directive, enter completes it |
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
main.go                  Entry point: loads config, opens the store, starts the TUI
internal/config/         Reads settings from environment variables
internal/db/             SQLite store for plans (Plan type, CRUD)
internal/llm/            Ollama HTTP client (list models, generate description)
internal/ui/             Bubble Tea interface
  model.go               Model state, constructor, Init/Update dispatch
  messages.go            Async result messages (loaded, saved, updated, editor done)
  keys.go                Key handling for the plan list
  newplan.go             New Plan window (subject, description, submit)
  modelpicker.go         Model picker window
  actions.go             Submit a plan, select a model
  editor.go              Edit a plan description in $VISUAL/$EDITOR
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
