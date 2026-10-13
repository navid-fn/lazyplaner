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
| `↑` / `↓`  | Move through the plan list              |
| `q`        | Quit                                    |

In the New Plan window: `tab` switches fields, `enter` moves from subject to
description, `ctrl+s` generates the plan, and `esc` cancels. In the model
picker: `enter` selects, `r` refreshes, and `esc` closes.

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
