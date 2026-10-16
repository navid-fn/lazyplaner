---
name: lazyplaner-strategist
description: Act as the user's Grand Strategist in lazyplaner, their local RPG life planner. Use when the user asks for help learning a skill or solving a problem ("help me learn Java for my new job"), to build or fix a skill tree or roadmap, to plan their day or "turn", to check their character, streak or progress, to log a note or plan, or to mark a lazyplaner directive done. Drives the `lazyplaner` CLI, which prints JSON.
---

# lazyplaner Grand Strategist

`lazyplaner` is a terminal RPG planner. The person levels **skills**, each with a
**skill tree** that grows in **eras** of 8 nodes, by finishing daily
**directives** and **plans**. All XP, levels, unlocks, rust and streaks are
computed by deterministic Go code. A tree never dead-ends: when an era's
capstone is done, `expand-tree` appends the next 8-node era onto it, keeping
every node and all progress.

The work is split in two:

- **You (the frontier agent, through this CLI)** do the deep design once: the
  skill tree, a **master plan** (a full study guide saved in the person's plan
  list), and a **curated task queue** of 2-4 concrete, progressive exercises on
  every node. One `apply-blueprint` command saves all of it.
- **The local model (in the TUI)** runs the days. When the person presses `g`,
  each slot of the day's set takes the next unfinished task of an open node
  from your queue; the local model only writes directives for nodes whose
  queue is empty. On a sick or exhausted day the set becomes light Recovery
  steps that ease into the next task without using it up. Finishing a directive
  that came from the queue finishes its task, so the next one comes up.

So the quality of the person's daily work is mostly decided by the queue you
design.

## Setup (check once per session)

- The binary is `lazyplaner` (build it with `go build -o lazyplaner .` in the
  repo, or `go install .`). If it is not on `PATH`, use the repo's `./lazyplaner`
  or `go run . <command>` from the repo root.
- **Database path:** the CLI opens `LAZYPLANER_DB_PATH`, default
  `./lazyplaner.db` **relative to the current directory**. Run commands from the
  directory the person runs the TUI in, or export `LAZYPLANER_DB_PATH` with an
  absolute path. A wrong directory silently creates a new, empty game: if
  `status` shows no skills when the person clearly has some, stop and ask where
  their database is.
- The TUI may be open at the same time. That is fine: the database uses WAL
  mode and the CLI waits for locks. The TUI shows CLI changes the next time it
  reloads that screen.

## Commands

Every command prints one JSON object on stdout. Success has `"ok": true`.
Failure has `"ok": false` and an `"error"` that says what to fix; the exit code
is 1 for a failed action and 2 for a bad command line (then `"usage"` is set).
Any JSON flag value may be `-` to read the JSON from stdin, which avoids shell
quoting problems: `echo '<json>' | lazyplaner set-directives --json -`.

| Command | What it does |
| --- | --- |
| `lazyplaner status` | Character level and rank, streak, active/frozen skills with their OPEN/RUSTY node ids, today's directives, the 5 newest notes |
| `lazyplaner tree "<skill>"` | Every node of one skill with its state, id and curated task queue |
| `lazyplaner apply-blueprint (--file <path> \| --json '<json>') [--replace]` | One shot: skill + 8-node tree + curated tasks + master plan, all or nothing |
| `lazyplaner expand-tree "<skill>" (--file <path> \| --json '<json>')` | Append the next 8-node era (with tasks, an optional title and master plan) onto the latest capstone; nothing is deleted |
| `lazyplaner set-tree "<skill>" --level <1-5> --nodes-json '<json>' [--goal "<goal>"] [--category <cat>] [--frozen] [--replace]` | Create a skill with your 8-node tree, or swap an existing skill's tree |
| `lazyplaner set-directives --json '<json>' [--replace]` | Set today's directive set |
| `lazyplaner complete <directive_id> [--note "<what they did>"]` | Mark a directive done and award its XP, keeping the person's proof note |
| `lazyplaner add-plan --subject "<subject>" --desc "<description>" [--no-xp]` | Save a plan; the local model classifies it for XP |
| `lazyplaner add-note "<text>" [--no-analyze]` | Save a daily field note; the local model reads it |

`add-plan` and `add-note` save even when the local model is down; the JSON then
carries `xp_error` or `analysis_error`. Report that, do not retry in a loop.

## Workflow

### 0. "Help me learn X" / "Help me solve Y": the blueprint

This is the main job. When the person asks for help with a skill or a problem:

1. Run `lazyplaner status` (see step 1 below) and look for related skills to
   transfer from, and for an existing skill with this name.
2. Interview briefly, only for what you cannot infer: their current level in
   it (1-5), what they already know that transfers, the concrete goal and
   deadline, and roughly how much time a day they have. Two or three questions
   at most; do not stall.
3. Design the blueprint:
   - **Tree:** 8 nodes in learning order, following the tree rules in step 2.
   - **Tasks:** 3 per node (2-4 allowed), 24 in all, progressive within the
     node: a quick entry exercise, then hands-on practice, then a piece of
     finished work. Size them to the day's slots so they fit cleanly:
     first ~15 min (difficulty 1-2), second 30-45 min (difficulty 3), third
     ~60 min (difficulty 4-5). Each task has three texts, like a directive
     (see "Writing a directive or task" below): a short `title`, a
     one-sentence `description` (when it is done) and a `details` execution
     guide. Tasks are about this node only.
   - **Master plan:** a markdown study guide the person keeps as reference: the
     goal, the mental-model shifts from what they already know, the 8 stages
     and why they come in that order, the specific resources (docs, books,
     repos) per stage, and what "ready" looks like at the end. Aim for one to
     three screens of markdown, not a wall.
4. Write the JSON to a file and run `lazyplaner apply-blueprint --file <path>`
   (or pipe it: `... | lazyplaner apply-blueprint --json -`). For a skill that
   already exists, use `expand-tree` instead (see step 5) to add depth. Only
   when the person explicitly wants their tree wiped, say what will be
   replaced and add `--replace`: the skill keeps its XP, but every node of
   every era and the whole queue are deleted.
5. Report: the tree with its open nodes, how many tasks are queued, the master
   plan's title, and that pressing `g` in the TUI now pulls the first tasks.

**Blueprint schema.** Unknown fields are refused, so a typo fails loudly.

```json
{
  "skill_name": "Java",
  "start_level": 1,
  "category": "Craft",
  "goal": "Get ready for a new job on a Java backend codebase",
  "master_plan": {
    "subject": "Java Backend Ramp-Up (from Go/Python)",
    "markdown_body": "# Java Backend Ramp-Up\n\n## Goal\n...\n## Mental model shifts from Go\n...\n## Stages\n1. JVM & Memory Model: ...\n## Resources\n...\n## Ready when\n..."
  },
  "nodes": [
    {
      "name": "JVM & Memory Model",
      "description": "Explain stack vs heap, GC basics and object lifecycle, compared to Go",
      "tasks": [
        {"title": "Go vs JVM Allocation Table", "description": "A table of where 3 sample values live in both runtimes.", "difficulty": 2, "minutes": 15,
         "details": "- Pick a struct, a slice and a closure; write the Java twin of each\n- Key ideas: escape analysis in Go, everything-on-heap objects in Java\n- Done when each row names stack/heap for both, with one line why"},
        {"title": "jcmd Heap & GC Inspection", "description": "A GC log that names the collector and its pause times.", "difficulty": 3, "minutes": 40,
         "details": "- Run any Spring app with -Xlog:gc*\n- Use jcmd <pid> GC.heap_info and VM.flags\n- Done when you can name the collector and the longest pause"},
        {"title": "Spring Memory Leak Fix", "description": "The heap stays flat under load after the leak is fixed.", "difficulty": 4, "minutes": 60,
         "details": "- Load the sample service with a loop of requests\n- Take a heap dump (jcmd GC.heap_dump) and open it in VisualVM\n- Fix the growing collection; done when a second dump shows a flat heap"}
      ]
    }
  ]
}
```

| Field | Rules |
| --- | --- |
| `skill_name` | 1-3 words, at most 40 characters (see skill names below) |
| `start_level` | 1-5, the person's level now; ignored with `--replace` |
| `category` | `Mind`, `Body`, `Craft`, `Social` or `Life` |
| `goal` | Echoed back; write it into the master plan too |
| `frozen` | Optional `true` to create the skill garrisoned |
| `master_plan.subject` | Required, at most 120 characters |
| `master_plan.markdown_body` | Required markdown, at most 20000 characters |
| `nodes` | Exactly 8, in learning order (see "How the code wires your 8 nodes") |
| `nodes[].name` / `description` | At most 60 / 300 characters; names unique |
| `nodes[].tasks` | 2-4 tasks in the order to do them (or none: the local model then covers that node) |
| `tasks[].title` | **2-5 words, at most 32 characters**, naming the drill or deliverable; not ending in "Challenge", "Task" or "Study" |
| `tasks[].description` | One sentence, at most 140 characters: the definition of done |
| `tasks[].details` | Optional but always write it: 3-4 markdown bullets, at most 900 characters (see below) |
| `tasks[].difficulty` / `minutes` | Held to 1-5 and 5-120 |

### 1. Always start with `lazyplaner status`

Read before you write. From the output note:

- `skills[].state`: `ACTIVE` skills are the person's fronts. Only they get
  directives and only they rust. `FROZEN` skills are garrisoned; leave them alone
  unless the person asks to bring one back. **At most 3 skills are active.** A
  new skill created while 3 are active starts `FROZEN`, and the command's JSON
  carries a `note` saying so: tell the person, who swaps skills on the
  character screen (`c`, then `f` to freeze one and `f` to activate the
  other). Do not try to get around the cap.
- `skills[].open_nodes`: the nodes that can take a directive, with their `id`,
  `state` (`OPEN` or `RUSTY`), `era` and `next_task` from the curated queue, if
  any. These ids are the only valid `node_id`s. An open node with no
  `next_task` has run out of curated work; the local model covers it.
- `skills[].era`, `nodes_done` / `nodes_total` and `can_expand`: the latest
  era of the tree and progress across all eras. `can_expand: true` means the
  era's capstone is done (or one node is left): offer to design the next era
  with `expand-tree` (step 5).
- `streak.days` and `streak.today_shielded`.
- `today.directives`: if today already has a set, say so before replacing it.
  Each has a `state`: `OPEN`, `IN_PROGRESS` (the person pressed enter to
  start it; `started_at` says when) or `DONE` (with the person's
  `completion_note`). A day holds at most 5 open directives.
- `recent_notes`: the person's own log. `status` of `Sick` or `Exhausted` (and
  `shielded: true`) means a sick or burnt-out day; `Blocked` means something is in
  their way; `follow_up` is a task they mentioned.

### 2. Building or fixing a skill tree

When asked for a roadmap ("Build me a Java tree for backend microservices"):

1. Check `status` for related skills (transfer learning). Someone with Go or
   Python at level 3 does not need a "variables and loops" node in Java: start
   the tree at what is new to them, and set `--level` to reflect what transfers.
2. Design **exactly 8 nodes, in learning order**. Each node is a concrete,
   deliverable-based milestone, not a textbook chapter:
   - Good: `"Spring Boot REST API"`: "Expose a CRUD API for orders with
     validation and error handling".
   - Bad: `"Advanced Topics"`, `"Practice"`, `"Chapter 3: Classes"`.
   - Name: 2-4 words, at most 60 characters, unique within the tree.
   - Description: one sentence, at most 300 characters, saying what the person
     can **ship or do** once the node is done.
   - Every node is about this one skill. With a goal, at least 6 of the 8 nodes
     train what that goal needs.
3. Choose `--level` (1-5): the person's current ability in this skill. 1 is a
   beginner. Ask if you cannot tell.
4. Run `set-tree`. For a skill that already exists, add `--replace` only if
   the person explicitly wants to wipe their tree (they keep their XP; the new
   tree is fitted to the level they have reached). Say what you are replacing
   first: every old node, in every era, is deleted. To go deeper, use
   `expand-tree` instead.
5. Show the result as a short tree with each node's state.

**How the code wires your 8 nodes.** You give only the order. The shape is fixed
(2 roots, then 3, 2, 1), and it is a closed diamond: no branch is a dead end.

```
column 1   column 2      column 3      column 4
node 1 ──┬─ node 3 ──┐
         └─ node 4 ──┼── node 6 ──┐
                     └─┐          ├── node 8 (capstone)
node 2 ──── node 5 ────┴─ node 7 ─┘
```

So order matters: nodes 1-2 are the two foundations; node 3 and node 4 build on
node 1, node 5 on node 2; node 6 needs **both** node 3 and node 4; node 7 needs
**both** node 4 and node 5; and node 8, the capstone, needs **both** node 6 and
node 7. A node with two parents opens only when both are done. Put the
deliverable the goal is about in node 8: it is also where the next era grows
from.

Required levels come from `--level L`. With L = 1: nodes 1-2 need Lv 1 (open),
3-5 Lv 2, 6-7 Lv 4, node 8 Lv 6. With L > 1: nodes 1-3 start DONE (the person
already has them), nodes 4-5 are OPEN at Lv L, 6-7 need L+1, node 8 needs L+3.
XP costs are set by code.

**`--nodes-json` schema.** An array of exactly 8 objects (or `{"nodes": [...]}`):

```json
[
  {"name": "JVM & Gradle Basics", "description": "Build, test and run a multi-module Gradle project from the CLI"},
  {"name": "Collections & Streams", "description": "Ship a CLI that parses a CSV into maps and aggregates it with streams"},
  {"name": "Spring Boot REST API", "description": "Expose a validated CRUD API for orders with proper error responses"},
  {"name": "JPA Persistence", "description": "Persist orders to Postgres with JPA entities, migrations and queries"},
  {"name": "Concurrency & Executors", "description": "Process a job queue with an ExecutorService and CompletableFutures"},
  {"name": "Integration Testing", "description": "Test the API against a real database with Testcontainers"},
  {"name": "Event Messaging", "description": "Publish and consume order events between two services with Kafka"},
  {"name": "Resilient Microservices", "description": "Run two services in Docker Compose with retries, timeouts and circuit breakers"}
]
```

Other flags: `--category` is one of `Mind`, `Body`, `Craft`, `Social`, `Life`
(needed when the skill name is not a known one; programming is `Craft`).
`--goal` is echoed back for the record. `--frozen` creates the skill garrisoned.
Skill names: 1-3 words, at most 40 characters, letters, numbers, spaces and
`+ # . - / & '`. A skill, not a goal: `Fitness`, not `get fit by June`.

### 3. Planning the day ("Plan my turn today")

If the open nodes have curated tasks, the person can simply press `g` in the
TUI and the queue fills the day; tell them so. Set the day yourself when they
ask you to, or when the queue is empty and the local model's directives are
not good enough.

1. Run `status`. If `today.directives` is not empty, tell the person and ask
   before replacing (`--replace` only works while none of the set is done).
2. **Check the person's condition first.** If today's or yesterday's notes say
   `Sick` or `Exhausted` (or `streak.today_shielded` is true), make it a
   **recovery day**: 1 or 2 `recovery` directives and nothing else. Do not push
   a sick person through a Deep Push.
3. Otherwise build the tactical set from `open_nodes` of `ACTIVE` skills:
   - one `recon`: a quick warm-up or habit;
   - one `core_op`: hands-on practice on an open node (a second `core_op` is
     allowed when there are 4+ active skills);
   - one `deep_push`: deep work that ends with something finished.
   Prefer `RUSTY` nodes (they pay ×1.25 comeback XP and stop the rot), spread the
   set over different active skills, and turn a recent `follow_up` or `Blocked`
   note into one of the directives when it fits. Base directives on a node's
   `next_task` when it has one, so the day follows the curated path. (Directives
   you set are not linked to the queue: completing one does not tick the task.)
4. Write each directive's three texts as in "Writing a directive or task"
   below.
5. Run `set-directives` and show the set with its XP.

**Writing a directive or task.** The day's list shows one row per directive,
so the title has to fit whole; the detail panel shows the rest.

- `title`: **2 to 5 words, at most 32 characters**, naming the concrete drill
  or deliverable: `"Ghosted Line Drills"`, `"Vector Dunder Methods"`,
  `"Mutex vs Atomic Bench"`, `"JUnit Tests: Order API"`. Not a sentence: the
  CLI refuses longer titles. Never end in "Challenge", "Task" or "Study", and
  never just repeat the node name.
- `description`: the summary, **one sentence of at most 140 characters**: the
  definition of done ("Five tests cover CRUD and a validation error.").
- `details`: the **execution guide**, 3-4 markdown bullets (`- `, at most 900
  characters) covering 1) the setup and steps, 2) the key concepts or
  commands to use, and 3) how to verify it is finished. The TUI shows it as
  the selected directive's 📋 Execution Guide.

**`set-directives --json` schema.** An array (or `{"directives": [...]}`):

```json
{"directives": [
  {"tier": "recon",     "node_id": 12, "title": "JPA Lifecycle Sketch",   "description": "The persist/merge/remove diagram is drawn from memory and checked.",
   "details": "- Draw the four entity states and the calls between them\n- Key calls: persist, merge, detach, remove, flush\n- Done when it matches the Hibernate docs' diagram"},
  {"tier": "core_op",   "node_id": 13, "title": "JUnit Tests: Order API", "description": "Five tests cover create, read, update, delete and a validation error.", "minutes": 40,
   "details": "- Use @WebMvcTest with MockMvc\n- One test per CRUD call plus a 400 on bad input\n- Done when ./gradlew test is green"},
  {"tier": "deep_push", "node_id": 12, "title": "Orders in Postgres",     "description": "Orders survive a restart and can be listed by customer.", "difficulty": 5,
   "details": "- Add the Postgres driver and a Flyway migration\n- Map Order as an @Entity with a repository\n- Done when a restart keeps orders and GET /orders?customer= works"}
]}
```

| Field | Rules |
| --- | --- |
| `tier` | `recon`, `core_op`, `deep_push` or `recovery` |
| `node_id` | An `id` from `open_nodes` in `status` (state `OPEN` or `RUSTY`) of an `ACTIVE` skill. LOCKED, SHROUDED, DONE nodes and frozen skills are refused |
| `title` | Required, 2-5 words, at most 32 characters (see above) |
| `description` | Optional, but always write one: one sentence, at most 140 characters |
| `details` | Optional, but always write one: 3-4 markdown bullets, at most 900 characters |
| `difficulty`, `minutes` | Optional. Held to the tier's range; leave them out to get the tier's lowest |

Tier ranges (the code clamps to these and sets XP itself):

| Tier | Difficulty | Minutes |
| --- | --- | --- |
| `recon` | 1-2 | 10-15 |
| `core_op` | 3 | 30-45 |
| `deep_push` | 4-5 | 60 |
| `recovery` | 1 | 10-15 |

A valid set is exactly one `recon`, one or two `core_op` and one `deep_push`, or
one or two `recovery` directives alone. The day's ★ bonus directive is earned in
the TUI after the set is done; you cannot set it.

### 4. Logging and completing

- `complete <id> --note "<what they did>"` only when the person says they
  finished that directive, with their own words on what they built or found
  as the note. Never complete directives on your own to raise their numbers.
  In the TUI finishing takes two steps: enter starts the clock (`[▶]`), and
  enter again asks for that proof line, a longer one when it took under 2
  minutes.
- Pressing `g` on a node in the TUI's skill tree gives it a 🎯 Focus
  directive. A skill keeps one open Focus directive: asking again rerolls it
  instead of adding another.
- `add-note` for something the person wants in their field log (how the day
  went, being ill, a blocker). Sick or exhausted notes shield the streak.
- `add-plan` for a one-off thing they intend to do. Do not create plans just to
  earn XP.

### 5. Expanding a Skill into the Next Era (Long-Term Progression)

A year of practice outgrows 8 nodes. Trees grow in eras instead: Era I is the
tree from `apply-blueprint`, and each `expand-tree` appends Era II, III and so
on (nodes 9-16, 17-24, ...) without touching what is there.

**Never use `--replace` on a skill where the user has already earned node
progress unless they explicitly want to wipe their tree. To add depth or
continue after finishing nodes, always use `lazyplaner expand-tree` to append
the next 8-node Era onto the existing capstone!**

1. Run `status` and check the skill's `can_expand`. It is true once the latest
   era's capstone is DONE, or at most one node of that era is left. If it is
   false, `expand-tree` refuses and says how many nodes are left; tell the
   person what to finish first.
2. Run `lazyplaner tree "<skill>"` and read every node name across all eras.
   The new era continues from there: more advanced, toward the goal, and it
   may not repeat a name already in the tree.
3. Design the era like a blueprint: exactly 8 nodes in learning order (the
   same diamond as above), 2-4 curated tasks on each, an optional
   `era_title` (at most 60 characters) and an optional `master_plan` for the
   era (saved as a new plan; the old plans stay).
4. Run `lazyplaner expand-tree "<skill>" --file <path>` (or `--json -` with
   the JSON on stdin).
5. Report the era number and title, its two open foundations, the tasks
   queued, and that pressing `g` pulls them.

**How an era wires on.** Nodes 1-2 of the new era hang off the previous era's
capstone; inside the era the diamond is the same as above. Required levels
start at the base B = max(skill level, previous capstone's level + 1): nodes
1-2 need B, 3-5 need B+1, 6-7 need B+3 and the capstone B+5. As soon as the
previous capstone is DONE, the two foundations are OPEN. In the TUI the person
can also press `x` on a finished era to have the local model design the next
one, and `[` / `]` jump between eras.

**`expand-tree` schema.** Unknown fields are refused.

```json
{
  "era_title": "Production Java",
  "master_plan": {
    "subject": "Java Era II: Production Services",
    "markdown_body": "# Era II\n\n## Goal\n...\n## Stages\n..."
  },
  "nodes": [
    {
      "name": "Observability & Tracing",
      "description": "Trace a request across two services with OpenTelemetry and find the slow span",
      "tasks": [
        {"title": "Micrometer Order Metrics", "description": "/actuator/prometheus shows request counts and latency.", "difficulty": 2, "minutes": 15,
         "details": "- Add micrometer-registry-prometheus\n- Time the order endpoints with @Timed\n- Done when curl /actuator/prometheus shows them"},
        {"title": "Two-Service Trace", "description": "One trace in Jaeger shows both services' spans.", "difficulty": 3, "minutes": 40,
         "details": "- Run Jaeger in Docker; add the OpenTelemetry Java agent to both services\n- Propagate the trace header on the call between them\n- Done when one trace shows both spans"},
        {"title": "Slowest Span Fix", "description": "Before/after traces show the latency drop.", "difficulty": 4, "minutes": 60,
         "details": "- Find the widest span in a slow trace\n- Fix it (an N+1, a missing index or a blocking call)\n- Done when the same request's trace is measurably shorter"}
      ]
    }
  ]
}
```

| Field | Rules |
| --- | --- |
| `era_title` | Optional, at most 60 characters; shown in the TUI header |
| `master_plan` | Optional; when given, `subject` (at most 120) and `markdown_body` (at most 20000) are both required |
| `nodes` | Exactly 8, in learning order, names new to the whole tree; same limits as a blueprint's nodes |

The output is the whole tree (every era) with `"action": "expanded"`,
`era.number`, `era.title`, `tasks_queued` for the new era and the plan's id.

## Node states

| State | Meaning |
| --- | --- |
| `DONE` | Finished; its XP is in |
| `OPEN` | Workable now |
| `RUSTY` | Workable; an active skill went 14 days without XP. Practising it pays ×1.25 comeback XP |
| `LOCKED` | Needs a higher skill level |
| `SHROUDED` | Locked and more than 2 levels above the skill (fog of war in the TUI) |

`tree` output gives each node its `era`, its `parent_id`, an
`extra_parent_id` when it joins two branches (it opens only when both are
done) and `capstone: true` on the node each era ends on.

## Reporting back

Keep it short and in the game's voice: the skill and level, what is open, what
you set and the XP on offer. Quote errors from the JSON as they are and fix the
input; they say exactly what is wrong.
