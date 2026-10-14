# lazyplaner — Feature Overview

A terminal (TUI) planner with an RPG layer. You write plans, a **local Ollama
model** describes them, and finishing/saving plans earns XP on skills that level
up a character. Everything is stored locally in SQLite.

- **Language / stack:** Go 1.24, Bubble Tea (TUI), Bubbles, Lip Gloss,
  `modernc.org/sqlite` (pure Go, no CGO)
- **AI backend:** Ollama HTTP API (`/api/tags`, `/api/generate`), no cloud
  service
- **Config (env vars):** `LAZYPLANER_DB_PATH` (default `./lazyplaner.db`),
  `OLLAMA_BASE_URL` (default `http://localhost:11434`), `OLLAMA_MODEL` (default
  `llama3.2`)
- **Run:** `go run .` (alt-screen, mouse enabled)

## 0. Home screen (command center)

- **Character HUD** (one line on top): level and rank with an XP bar, active
  skill count, today's directives done/total, streak, score, and a rusty-node
  count when any node is rusty.
- **Left column**, `tab` switches focus between:
  - **🎯 Today's Directives** — today's directives with a progress bar and
    a tier tag on each row. `↑/↓` or `j/k` move, `enter`/`space` completes
    one, `r` rerolls the highlighted open one. With none yet, it prompts `g`
    to generate the day's set. While the model works, the panel header shows
    the spinner (there is no popup).
  - **📋 Plans** — the plan list. Each plan that earned XP shows a
    `[Skill +XP]` badge. `e` edits and `d` deletes only while the plans have
    focus.
- **Right panel:** the selected plan's markdown when the plans have focus and a
  plan is selected; otherwise the **📊 Strategic Briefing**: active skills with
  level bars and rust warnings, the selected directive (description, skill ›
  node, difficulty stars, minutes, XP reward, comeback note if its node is
  rusty), each active skill's **next move**, and the last 3 XP awards.

## 1. Plans

- Create a plan with a **subject** (≤120 chars) and a **description** (≤2000
  chars); both required.
- `ctrl+s` sends them to Ollama, which returns a concise **markdown
  description** of the plan.
- Plan list in the lower left of the home screen; markdown **preview** of the
  selected plan on the right while the plan list has focus.
- Edit a plan's description in `$VISUAL` / `$EDITOR` (`e`).
- Delete a plan (`d`, then `y` to confirm).
- Model picker (`ctrl+o`, from **any screen**, popups included): lists
  installed Ollama models, `r` refreshes, `enter` selects, `esc` or `ctrl+o`
  closes and returns to the screen it was opened from. The footer always shows
  `ctrl+o ◆ <model>` on the right. Selection applies to all later generation.

## 2. Character & skills (RPG layer)

- **Skills** are the character sheet. Names are unique ignoring case.
  - 1–3 words, ≤40 chars; letters, digits, spaces and `+ # . - / & '` only
    (`C++`, `CI/CD`, `R&D`).
  - Rejected: goals/sentences, emoji/symbols like `! ? @ %`, duplicates,
    empty/no-letter names.
  - Validation lives in `internal/rpg/skillname.go`.
- **Categories** (fixed): Mind, Body, Craft, Social, Life. The user never picks
  one; the **AI assigns it** when it builds a skill's tree and when a plan is
  classified.
- **Onboarding / setup screen** (first launch, or `s` later):
  - Add up to 8 start skills. Each has three fields: **Skill**, **Current
    Standing / Experience** (optional) and **Target Focus / Goal** (optional).
  - **Skill Auditor:** with an experience sentence ("Wrote Go at work for four
    years, stopped in 2024"), pressing enter asks a small local model at once
    for a start level, a rusty flag and a category. While the experience field
    has text, the manual level selector is replaced by a locked
    `[ 🤖 AI Auto-Calibration Active ]` badge and skipped by `↑/↓`. With no
    experience, the level is picked by hand (1–5, Beginner → Expert). Building
    waits until every audit has answered; if the auditor fails, the hand-picked
    level is kept. The card shows the result; the form's message goes back to
    the hint, and any message clears as soon as the next skill is typed.
  - **Category without experience:** a skill added without an experience
    sentence is sent to a small classifier (with a keyword fallback when the
    model is down), so every card shows Mind/Body/Craft/Social/Life.
  - **Names are capitalized** per word ("public speaking" → "Public
    Speaking"); words that already have a capital stay as typed (SQL, CI/CD,
    iOS).
  - **Goal** is passed to the tree builder, which shapes the advanced nodes
    toward it. (It is used at build time and not stored.)
  - The left panel shows each skill as a card (level, category, rusty badge,
    goal), grouped into **⚔️ Active Fronts** (at most 3) and **🛡️ Garrisoned /
    Backlog** (marked FROZEN). New skills take a free front, else go to the
    backlog. Backlog skills are created in the garrisoned stance, so they do
    not rust.
  - Keys: `tab`/`shift+tab` or `↑/↓` move between form fields, `ctrl+r`
    switches between the form and the roster. In the roster, `↑/↓` pick a card, `f`/`space` toggles it
    between active and frozen (promoting into full fronts swaps with the last
    active skill), `enter` edits, `ctrl+d` removes. `ctrl+s` builds the
    trees, `esc` skips. The footer lists the keys for the focused panel.
  - **Building** designs the trees one skill at a time. The form locks (no
    cursor, no typing) and shows a single progress line, e.g. `Designing skill
    trees (2/6: Python)…`. If one fails, the skills already saved leave the
    list and `ctrl+s` retries the rest.
  - `?` shows contextual help.
- **Editing / renaming a skill resets it to level 1** (XP and node progress →
  0). XP history and the tree are kept.

### XP and levels (deterministic, computed in code — not by the model)

- **Plan XP:** every 5 minutes of effort = 1 base XP (minutes clamped 5–240),
  times a difficulty multiplier (1→×1.0, 2→×1.25, 3→×1.5, 4→×2.0, 5→×2.5).
  Minimum 1 XP.
- **Level curve:** cumulative XP for level _n_ = `100 · (n-1)^1.5` (level 2 =
  100 XP). Early levels come fast, later ones slow down.
- **Rank titles:** Novice (1) → Apprentice (5) → Journeyman (10) → Expert (20) →
  Master (35) → Grandmaster (50).
- The LLM only **classifies** (skill, category, difficulty 1–5, minutes, node,
  parent); fixed formulas turn that into XP, so the same plan always earns the
  same XP.

### Plan → XP flow

When a plan is saved, the model classifies it against the character's existing
skills and tree nodes (structured JSON output, schema-enforced by Ollama). The
result is recorded as an **XP event**: it reuses an existing skill/node by name
or creates a new skill/node (attached to a named parent node, or to the skill
root). The status bar reports what was earned.

### Character sheet / "road" screen (`c`)

- Skills grouped by category, with level, rank title, gradient XP progress bars.
- XP history (recent XP events: activity, skill, XP, difficulty, minutes). XP
  events survive deleting the originating plan.

## 3. Skill trees (`t`)

- Each skill has a **tree of 8 sub-skills** from foundations to mastery. The
  AI only lists them in learning order (name + practical milestone) and picks
  the category; it is sent nothing but the skill, its start level and its
  goal, and told every node must be strictly about that skill. With a goal,
  at least 6 of the 8 nodes must train what that exact goal needs, with no
  generic filler (sleep, hydration, basic syntax); the goal is repeated at
  the end of the prompt.
- **Stable categories:** skills the keyword list knows (programming
  languages, sports, instruments…) always get the same category, whichever
  prompt classified them; Golang is always Craft.
- The **shape is fixed in code** (`rpg.shapeTree`), a 2 → 3 → 2 → 1 diamond:
  roots N0, N1; N2, N3 ← N0 and N4 ← N1; N5 ← N3 and N6 ← N4; capstone
  N7 ← N5.
- **Required levels are set in code** from the start level _L_:
  - _L_ = 1: roots Lv 1 (open), column 2 Lv 2, column 3 Lv 4, capstone Lv 6.
  - _L_ > 1: roots Lv max(1, _L_−2) and N2 Lv _L_−1 start done; N3, N4 need
    exactly _L_, so **two nodes are always open**; column 3 needs _L_+1
    (locked, visible); the capstone _L_+3 (in fog).
- A node costs `40 × required level` XP, but never less than 60% of the XP
  climb from its level to the next (`rpg.NodeXPNeeded`; a Lv 2 node costs
  110 because Lv 2 → 3 is 183 XP). The two nodes a fresh tree opens at a
  level therefore always pay for reaching the next one. Node states:
  - **Locked** — skill level below the node's requirement, or (for a node
    with a parent) the parent is not **Done** yet. A branch opens one node at
    a time.
  - **Unlocked** — open to work on.
  - **Rusty** — open, but neglected (see below). Shown in orange with `⚠`.
  - **Done** — node progress reached its cost.
  - **No soft-lock:** when no node is open, the next tier (the locked nodes
    with the lowest requirement whose parent is done) opens even if the skill
    is a little short of its level.
- **Calibrated start:** a tree is created at the skill's start level _N_. Nodes
  below _N_ are skills you already have and start **Done**; nodes at _N_ start
  **Unlocked** and empty; nodes above _N_ are **Locked**.
- Plans/challenges that train a node add progress to it.
- **Fog of war:** a locked node more than 2 levels above the skill shows as
  `░░ [ ??? ]` on the map, with its name and description hidden, until the
  skill levels up closer to it.
- Rendered as a **2D map**: skill orb at the root, node cards with progress
  bars, box-drawing connectors, gradient colors. Card width and row gap are
  fitted to the terminal size; the map scrolls when larger than the view (custom
  canvas in `internal/ui/canvas.go`).
- The inspector under the map shows the selected node and today's directive
  for it; `enter`/`space` completes that directive from the tree. On an open
  or rusty node with none, `g` asks for a **🎯 Focus** directive for exactly
  that node (spinner in the inspector). One per node per day; focus
  directives join today's list but are not the day's set.
- A leaf that is not the capstone is labelled **side branch**: nothing grows
  from it, but its XP counts toward the level that opens the next column.
- Keys: `←/→` or `h/l` (or tab) switch skill, `↑/↓` or `j/k` move between nodes,
  `enter` complete the node's directive, `e` edit the skill, `?` help, `t`/`esc`
  back. The bottom key bar lists them. Mouse is enabled.

### Stances and rust

- Every skill has a **stance**: **active** (the default) or **garrisoned**
  (parked). Only active skills rust. (Phase 1 stores the stance; there is no UI
  toggle yet.)
- When an active skill earns no XP for **14 days**, its **frontier node** (the
  open node with the highest required level) turns **Rusty**. Rust is checked
  whenever character data loads, including app start. The auditor can also
  start a new skill rusty.
- Earning XP on a rusty node (a plan or a challenge) clears the rust and pays a
  **×1.25 comeback bonus** (always at least +1 XP over normal). The status line
  says when a comeback happened, and the challenge score counts the bonus.
- "Last practiced" is updated on every XP award. Skills from older databases
  are backfilled from their newest XP event; a skill that never earned XP does
  not rust.

## 4. Today's directives (`g`)

- Managed entirely in the home screen's **🎯 Today's Directives** panel.
- `g` generates the day's **tactical set** of 3 directives (4 with a fourth
  active skill), dealt over the **active skills** in order:
  - `[⚡ Recon · 15m]` difficulty 1–2, a fast warm-up or habit task
  - `[🔧 Core Op · 30-45m]` difficulty 3, hands-on practice on an open node
  - `[🔥 Deep Push · 60m]` difficulty 4–5, a deep work milestone
- Only **active** skills and their **open or rusty** nodes are sent to the
  model; locked nodes and garrisoned skills never get directives. Difficulty
  and minutes are held to the tier in Go, and a node outside the open list is
  dropped.
- Titles must start with an action verb and a concrete scope; the prompt
  forbids ending in "Challenge", "Task" or "Study", and such endings are also
  stripped in code.
- `enter` completes a directive, awarding XP to its skill/node like a plan.
- `r` rerolls **only the highlighted open directive**, keeping its tier,
  skill and (if still open) node.
- **Anti-grind cap:** a day gets one set. Once every directive is done, `g`
  gives **one** `★ Bonus Overtime Directive` per day; after that the panel
  says the daily cap is reached.
- **Stats strip:** done/total today, total score (XP from all completed
  challenges), and **streak** (consecutive days with ≥1 completed challenge;
  today doesn't break the streak until the day is over).
- Days use the local date.

## 5. Help (`h` / `?`)

In-app help popup with tabs (←/→ to switch): **Skills** (what names are
accepted), **Levels**, **Categories**, **Plans**, **Keys**. Opens over any
screen and returns to it.

## 6. Keybindings

| Key        | Action                          |
| ---------- | ------------------------------- |
| `ctrl+n`   | New plan                        |
| `ctrl+o`   | Model picker                    |
| `e`        | Edit selected plan in `$EDITOR` |
| `d` (+`y`) | Delete selected plan            |
| `c`        | Character sheet and XP history  |
| `t`        | Skill tree map                  |
| `g`        | Today's directives / bonus      |
| `s`        | Character setup                 |
| `h` / `?`  | Help                            |
| `tab`      | Home: switch Directives / Plans |
| `↑/↓`      | Move in the focused home panel  |
| `enter`    | Home: complete the directive    |
| `q`        | Quit                            |

New Plan window: `tab` switch fields, `enter` subject→description, `ctrl+s`
generate, `esc` cancel.

## 7. Architecture

```
main.go                 Entry: load config, open store, start Bubble Tea program
internal/config/        Env-var settings
internal/db/            SQLite store
  store.go              Plans CRUD, schema creation + migrations
  xp.go                 Skills, XP events, find-or-create skill/node
  tree.go               Skill nodes, trees, UpdateSkill (rename + reset)
  challenges.go         Daily directives, reroll, bonus cap, completion, stats, streak
  settings.go           Key/value settings
  rust.go               Rust and stance setters, last-practiced backfill
internal/llm/           Ollama client
  client.go             ListModels, GenerateDescription (markdown)
  structured.go         GenerateJSON with a JSON schema (structured output)
internal/rpg/           Game rules, independent of UI and DB
  curve.go              XP curve, level progress, rank titles
  score.go              Award(), categories, difficulty multipliers, comeback bonus
  tree.go               Node status (Locked/Unlocked/Rusty/Done), node XP cost,
                        calibrated start progress, frontier node, rust rule
  classify.go           Plan → skill/category/difficulty/minutes/node (LLM)
  roadmap.go            BuildRoadmap (8 sub-skills from the model, shape + levels in code),
                        AuditSkill (small-model experience → start level, rusty)
  directives.go         Tiers, DailySlots, BuildDirectives (daily set, reroll, bonus)
  skillname.go          Skill-name validation and capitalization
  category.go           CategorizeSkill (small model) with a keyword fallback
internal/ui/            Bubble Tea interface
  model.go / messages.go / keys.go / commands.go   state, async messages, input, tea.Cmd wrappers
  view.go / layout.go / panel.go / styles.go / overlay.go   rendering
  newplan.go, modelpicker.go, editor.go, actions.go, state.go, items.go   plan features
  onboard.go, skilledit.go   setup and skill editing
  character.go, tree.go, canvas.go   character sheet, tree map, drawing canvas
  daily.go                 today's directives: generate, reroll, bonus, complete
  help.go                  help popup
```

**Data flow:** `Update` handles a key/message and returns a `tea.Cmd` → commands
run DB/LLM calls off the UI loop and return a message → `Update` mutates state →
`View` renders (never does I/O).

**Design principles:** the LLM only classifies/generates content; XP and levels
come from deterministic Go formulas. The `rpg` package depends on a small
`Generator` interface, so it is testable without Ollama.

### SQLite schema

| Table         | Purpose                                                                                                                    |
| ------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `plans`       | id, subject, detail, description (markdown), created_at                                                                    |
| `skills`      | id, name (unique, NOCASE), category, total_xp, is_active (stance), last_practiced_at (RFC 3339 text)                       |
| `skill_nodes` | id, skill_id, parent_id (0 = skill root), name, description, required_level, xp_needed, progress, is_rusty; unique per (skill, name) |
| `xp_events`   | id, skill_id, plan_id (no FK — history outlives plans), node_id, activity, difficulty, minutes, xp, created_at             |
| `challenges`  | id, day (local YYYY-MM-DD), batch, skill_id, node_id, title, description, difficulty, minutes, xp, done_at                 |
| `settings`    | key/value (e.g. daily set size)                                                                                            |

Schema is created with `CREATE TABLE IF NOT EXISTS`; later columns are added by
`addColumnIfMissing` for databases from older versions.

## 8. Testing

Go tests cover: DB tree/XP/settings (`internal/db/*_test.go`), RPG rules,
skill-name validation and an Ollama-backed classifier test
(`internal/rpg/*_test.go`), and UI tree + end-to-end flows
(`internal/ui/tree_test.go`, `flow_test.go`).

## 9. Not present (as of now)

No due dates/reminders, no plan completion toggle or status field, no
tags/search, no export/import, no cloud sync or accounts, no non-Ollama LLM
providers. Plans and skills are single-user and local.
