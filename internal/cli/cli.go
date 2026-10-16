// Package cli runs lazyplaner headless: each subcommand reads or changes
// the same database as the terminal UI, through the same game rules, and
// prints JSON. It is how scripts and AI agents drive the game.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/game"
	"github.com/navid-fn/lazyplaner/internal/llm"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1 // the command ran and failed
	exitUsage = 2 // the command line was wrong
)

// recentNotes is how many notes status shows.
const recentNotes = 5

// Env is what a command runs against. Client is nil when no model is set
// up; commands that need one report an error for that step.
type Env struct {
	Store  *db.Store
	Client *llm.Client
	Stdin  io.Reader
	Stdout io.Writer
	Now    func() time.Time
}

// usageError is a mistake on the command line.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

type command struct {
	usage string
	run   func(ctx context.Context, env Env, args []string) (any, error)
}

var commands = map[string]command{
	"status":          {"status", runStatus},
	"tree":            {"tree <skill>", runTree},
	"set-tree":        {`set-tree <skill> --level <1-5> --nodes-json '<json>' [--goal "<goal>"] [--category <cat>] [--frozen] [--replace]`, runSetTree},
	"apply-blueprint": {`apply-blueprint (--file <path> | --json '<json>') [--replace]`, runApplyBlueprint},
	"expand-tree":     {`expand-tree <skill> (--file <path> | --json '<json>')`, runExpandTree},
	"set-directives":  {`set-directives --json '<json>' [--replace]`, runSetDirectives},
	"complete":        {`complete <directive_id> [--note "<what you did>"]`, runComplete},
	"add-plan":        {`add-plan --subject "<subject>" --desc "<description>" [--no-xp]`, runAddPlan},
	"add-note":        {`add-note "<text>" [--no-analyze]`, runAddNote},
}

// commandOrder is the order Usage lists the commands in.
var commandOrder = []string{"status", "tree", "apply-blueprint", "expand-tree", "set-tree", "set-directives", "complete", "add-plan", "add-note"}

// IsCommand reports whether name is a CLI subcommand.
func IsCommand(name string) bool {
	if name == "help" || name == "--help" || name == "-h" {
		return true
	}
	_, ok := commands[name]
	return ok
}

// Usage is the command list.
func Usage() string {
	var b strings.Builder
	b.WriteString("usage: lazyplaner                 open the terminal UI\n")
	for _, name := range commandOrder {
		b.WriteString("       lazyplaner " + commands[name].usage + "\n")
	}
	b.WriteString("Every command prints JSON. A JSON value in a flag may be - to read it from stdin.\n")
	return b.String()
}

// Run runs one subcommand and prints its result, or {"ok": false,
// "error": ...}, as JSON. It returns the process exit code.
func Run(ctx context.Context, env Env, args []string) int {
	if env.Now == nil {
		env.Now = time.Now
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(env.Stdout, Usage())
		return exitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		writeJSON(env.Stdout, map[string]any{"ok": false, "error": "unknown command " + args[0], "usage": Usage()})
		return exitUsage
	}

	result, err := cmd.run(ctx, env, args[1:])
	if err != nil {
		out := map[string]any{"ok": false, "error": err.Error()}
		code := exitError
		var ue usageError
		if errors.As(err, &ue) {
			out["usage"] = "lazyplaner " + cmd.usage
			code = exitUsage
		}
		writeJSON(env.Stdout, out)
		return code
	}
	writeJSON(env.Stdout, result)
	return exitOK
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// newFlags returns a flag set that reports errors instead of printing them.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses flags that may come before or after the positional
// arguments, and returns the positional ones.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{err.Error()}
		}
		args = fs.Args()
		if len(args) == 0 {
			return rest, nil
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

// jsonArg reads a flag's JSON value, or stdin when the value is -.
func jsonArg(env Env, flagName, value string) ([]byte, error) {
	switch value {
	case "":
		return nil, usagef("--%s is required", flagName)
	case "-":
		if env.Stdin == nil {
			return nil, usagef("--%s - needs JSON on stdin", flagName)
		}
		data, err := io.ReadAll(env.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	return []byte(value), nil
}

// ─── Output shapes ───────────────────────────────────────────────────────────

type skillOut struct {
	Name          string    `json:"name"`
	Category      string    `json:"category"`
	Stat          string    `json:"stat"`
	Level         int       `json:"level"`
	TotalXP       int       `json:"total_xp"`
	State         string    `json:"state"` // ACTIVE or FROZEN
	LastPracticed string    `json:"last_practiced,omitempty"`
	RustyNodes    int       `json:"rusty_nodes"`
	Era           int       `json:"era"`        // the latest era of the tree
	NodesDone     int       `json:"nodes_done"` // across every era
	NodesTotal    int       `json:"nodes_total"`
	CanExpand     bool      `json:"can_expand"` // expand-tree may add the next era now
	OpenNodes     []nodeOut `json:"open_nodes,omitempty"`
}

type nodeOut struct {
	ID            int64     `json:"id"`
	ParentID      int64     `json:"parent_id"`
	ExtraParentID int64     `json:"extra_parent_id,omitempty"` // a second parent; the node waits for both
	Era           int       `json:"era"`
	Capstone      bool      `json:"capstone,omitempty"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	State         string    `json:"state"` // DONE, OPEN, RUSTY, LOCKED or SHROUDED
	RequiredLevel int       `json:"required_level"`
	Progress      int       `json:"progress"`
	XPNeeded      int       `json:"xp_needed"`
	Tasks         []taskOut `json:"tasks,omitempty"`     // tree: the node's curated queue
	NextTask      *taskOut  `json:"next_task,omitempty"` // status: the next curated task
}

type taskOut struct {
	ID          int64  `json:"id"`
	Position    int    `json:"position"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Difficulty  int    `json:"difficulty"`
	Minutes     int    `json:"minutes"`
	Completed   bool   `json:"completed"`
}

func toTask(t db.NodeTask) taskOut {
	return taskOut{ID: t.ID, Position: t.Position, Title: t.Title, Description: t.Description, Details: t.Details,
		Difficulty: t.Difficulty, Minutes: t.Minutes, Completed: t.Completed}
}

// nextTask is the node's next unfinished curated task, if any.
func nextTask(next map[int64]db.NodeTask, nodeID int64) *taskOut {
	t, ok := next[nodeID]
	if !ok {
		return nil
	}
	out := toTask(t)
	return &out
}

type directiveOut struct {
	ID          int64  `json:"id"`
	Tier        string `json:"tier"`
	Skill       string `json:"skill"`
	Node        string `json:"node,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Difficulty  int    `json:"difficulty"`
	Minutes     int    `json:"minutes"`
	XP          int    `json:"xp"`
	Done        bool   `json:"done"`
	State       string `json:"state"`                // OPEN, IN_PROGRESS or DONE
	StartedAt   string `json:"started_at,omitempty"` // local time, once started
	Note        string `json:"completion_note,omitempty"`
}

type noteOut struct {
	ID        int64  `json:"id"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Text      string `json:"text"`
	Status    string `json:"status,omitempty"` // "" until analyzed
	Shielded  bool   `json:"shielded"`
	Skill     string `json:"skill,omitempty"`
	Minutes   int    `json:"minutes,omitempty"`
	FollowUp  string `json:"follow_up,omitempty"`
	ClaimedXP int    `json:"claimed_xp,omitempty"`
}

// stats are the RPG attribute of each category.
var stats = map[string]string{"Mind": "INT", "Body": "STR", "Craft": "DEX", "Social": "CHA", "Life": "WIS"}

// tierKeys are the tier names the CLI prints and set-directives reads.
var tierKeys = map[rpg.Tier]string{
	rpg.TierRecon: "recon", rpg.TierCoreOp: "core_op", rpg.TierDeepPush: "deep_push",
	rpg.TierOvertime: "bonus", rpg.TierFocus: "focus", rpg.TierRecovery: "recovery",
}

// nodeState is a node's state as the CLI prints it.
func nodeState(n db.Node, st rpg.Status, level int) string {
	if rpg.Shrouded(st, n.RequiredLevel, level) {
		return "SHROUDED"
	}
	return st.String()
}

func toNodeOut(n db.Node, st rpg.Status, level int) nodeOut {
	return nodeOut{ID: n.ID, ParentID: n.ParentID, ExtraParentID: n.ExtraParentID, Era: max(n.Era, 1),
		Capstone: n.IsCapstone, Name: n.Name, Description: n.Description,
		State: nodeState(n, st, level), RequiredLevel: n.RequiredLevel, Progress: n.Progress, XPNeeded: n.XPNeeded}
}

func toDirectives(list []db.Challenge) []directiveOut {
	out := make([]directiveOut, 0, len(list))
	for _, c := range list {
		d := directiveOut{ID: c.ID, Tier: tierKeys[c.Tier], Skill: c.SkillName, Node: c.NodeName,
			Title: c.Title, Description: c.Description, Details: c.Details, Difficulty: c.Difficulty,
			Minutes: c.Minutes, XP: c.XP, Done: c.Done, State: "OPEN", Note: c.CompletionNote}
		if c.Started() {
			d.State = "IN_PROGRESS"
			d.StartedAt = c.StartedAt.Local().Format("2006-01-02 15:04")
		}
		if c.Done {
			d.State = "DONE"
		}
		out = append(out, d)
	}
	return out
}

func toNote(n db.Note) noteOut {
	at := n.CreatedAt.Local()
	return noteOut{ID: n.ID, Date: at.Format("2006-01-02"), Time: at.Format("15:04"), Text: n.Body,
		Status: n.Analysis.Status, Shielded: n.Analysis.Shield, Skill: n.Analysis.Skill,
		Minutes: n.Analysis.Minutes, FollowUp: n.Analysis.FollowUp, ClaimedXP: n.ClaimedXP}
}

// ─── Commands ────────────────────────────────────────────────────────────────

func runStatus(ctx context.Context, env Env, args []string) (any, error) {
	if len(args) > 0 {
		return nil, usagef("status takes no arguments")
	}
	// Rust is applied on every load, as in the UI, so a neglected skill
	// shows it here too.
	if err := game.ApplyRust(ctx, env.Store, env.Now()); err != nil {
		return nil, err
	}
	trees, err := env.Store.SkillTrees(ctx)
	if err != nil {
		return nil, err
	}
	day := env.Now().Format("2006-01-02")
	st, err := env.Store.ChallengeStats(ctx, day)
	if err != nil {
		return nil, err
	}
	directives, err := env.Store.ListChallenges(ctx, day)
	if err != nil {
		return nil, err
	}
	notes, err := env.Store.ListNotes(ctx)
	if err != nil {
		return nil, err
	}
	next, err := env.Store.NextNodeTasks(ctx)
	if err != nil {
		return nil, err
	}

	total := 0
	skills := make([]skillOut, 0, len(trees))
	for _, t := range trees {
		total += t.Skill.TotalXP
		level := rpg.ProgressFor(t.Skill.TotalXP).Level
		statuses := rpg.NodeStatuses(game.TreeNodes(t.Nodes), level)
		s := skillOut{Name: t.Skill.Name, Category: t.Skill.Category, Stat: stats[t.Skill.Category],
			Level: level, TotalXP: t.Skill.TotalXP, State: "FROZEN"}
		if t.Skill.IsActive {
			s.State = "ACTIVE"
		}
		era := game.Eras(t)
		s.Era, s.NodesDone, s.NodesTotal, s.CanExpand = era.Latest, era.Done, era.Total, era.CanExpand
		if !t.Skill.LastPracticed.IsZero() {
			s.LastPracticed = t.Skill.LastPracticed.Local().Format("2006-01-02")
		}
		for _, n := range t.Nodes {
			st := statuses[n.ID]
			if st == rpg.Rusty {
				s.RustyNodes++
			}
			if st == rpg.Rusty || st == rpg.Unlocked {
				out := toNodeOut(n, st, level)
				out.NextTask = nextTask(next, n.ID)
				s.OpenNodes = append(s.OpenNodes, out)
			}
		}
		skills = append(skills, s)
	}

	p := rpg.ProgressFor(total)
	character := map[string]any{
		"level": p.Level, "rank": rpg.RankTitle(p.Level), "total_xp": total,
		"xp_into_level": p.Into, "xp_for_level": p.Span,
	}
	if next, from, ok := rpg.NextRank(p.Level); ok {
		character["next_rank"] = map[string]any{"rank": next, "level": from}
	}

	recent := make([]noteOut, 0, recentNotes)
	for _, n := range notes[:min(len(notes), recentNotes)] {
		recent = append(recent, toNote(n))
	}
	done := 0
	for _, c := range directives {
		if c.Done {
			done++
		}
	}

	return map[string]any{
		"ok":        true,
		"date":      day,
		"character": character,
		"streak":    map[string]any{"days": st.Streak, "today_shielded": st.TodayShielded},
		"score":     st.Score,
		"today": map[string]any{
			"done": done, "total": len(directives), "directives": toDirectives(directives),
		},
		"skills":       skills,
		"recent_notes": recent,
	}, nil
}

// treeOutput is a skill and every node in its tree, parents first.
func treeOutput(ctx context.Context, store *db.Store, name string) (map[string]any, error) {
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := store.ListNodeTasks(ctx)
	if err != nil {
		return nil, err
	}
	eras, err := store.ListEras(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range trees {
		if !strings.EqualFold(t.Skill.Name, strings.TrimSpace(name)) {
			continue
		}
		level := rpg.ProgressFor(t.Skill.TotalXP).Level
		statuses := rpg.NodeStatuses(game.TreeNodes(t.Nodes), level)
		nodes := make([]nodeOut, 0, len(t.Nodes))
		for _, n := range t.Nodes {
			out := toNodeOut(n, statuses[n.ID], level)
			for _, task := range tasks {
				if task.NodeID == n.ID {
					out.Tasks = append(out.Tasks, toTask(task))
				}
			}
			nodes = append(nodes, out)
		}
		state := "FROZEN"
		if t.Skill.IsActive {
			state = "ACTIVE"
		}
		era := game.Eras(t)
		titles := map[int]string{}
		for _, e := range eras {
			if e.SkillID == t.Skill.ID && e.Title != "" {
				titles[e.Era] = e.Title
			}
		}
		return map[string]any{
			"ok": true,
			"skill": map[string]any{"name": t.Skill.Name, "category": t.Skill.Category,
				"stat": stats[t.Skill.Category], "level": level, "total_xp": t.Skill.TotalXP, "state": state,
				"era": era.Latest, "era_titles": titles, "nodes_done": era.Done, "nodes_total": era.Total,
				"can_expand": era.CanExpand},
			"nodes": nodes,
		}, nil
	}
	return nil, fmt.Errorf("no skill named %q; lazyplaner status lists them", name)
}

func runTree(ctx context.Context, env Env, args []string) (any, error) {
	if len(args) != 1 {
		return nil, usagef("tree takes one skill name (quote names with spaces)")
	}
	return treeOutput(ctx, env.Store, args[0])
}

func runSetTree(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("set-tree")
	level := fs.Int("level", 0, "start level, 1-5")
	goal := fs.String("goal", "", "what the tree builds toward")
	nodesJSON := fs.String("nodes-json", "", "the 8 nodes as JSON")
	category := fs.String("category", "", "Mind, Body, Craft, Social or Life")
	frozen := fs.Bool("frozen", false, "create the skill frozen")
	replace := fs.Bool("replace", false, "replace an existing skill's tree")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, usagef("set-tree takes one skill name (quote names with spaces)")
	}
	if *level == 0 && !*replace {
		return nil, usagef("--level is required for a new skill")
	}
	data, err := jsonArg(env, "nodes-json", *nodesJSON)
	if err != nil {
		return nil, err
	}
	var nodes []rpg.ExternalNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		var wrapped struct {
			Nodes []rpg.ExternalNode `json:"nodes"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil || wrapped.Nodes == nil {
			return nil, fmt.Errorf("--nodes-json: %w", err)
		}
		nodes = wrapped.Nodes
	}

	skill, err := game.ImportTree(ctx, env.Store, game.TreeImport{
		Skill: pos[0], Level: *level, Category: *category, Frozen: *frozen, Replace: *replace, Nodes: nodes,
	})
	if err != nil {
		return nil, err
	}
	out, err := treeOutput(ctx, env.Store, skill.Name)
	if err != nil {
		return nil, err
	}
	out["action"] = "created"
	if *replace {
		out["action"] = "replaced"
	}
	addCapNote(out, skill, *frozen || *replace)
	if g := strings.TrimSpace(*goal); g != "" {
		out["goal"] = g
	}
	return out, nil
}

func runApplyBlueprint(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("apply-blueprint")
	file := fs.String("file", "", "path to the blueprint JSON, or - for stdin")
	raw := fs.String("json", "", "the blueprint JSON, or - for stdin")
	replace := fs.Bool("replace", false, "replace the tree of an existing skill")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) > 0 {
		return nil, usagef("apply-blueprint takes --file or --json, not positional arguments")
	}
	var b game.Blueprint
	if err := decodeFileOrJSON(env, *file, *raw, "blueprint", &b); err != nil {
		return nil, err
	}
	skill, plan, err := game.ApplyBlueprint(ctx, env.Store, b, *replace)
	if err != nil {
		return nil, err
	}

	out, err := treeOutput(ctx, env.Store, skill.Name)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, n := range out["nodes"].([]nodeOut) {
		count += len(n.Tasks)
	}
	out["action"] = "created"
	if *replace {
		out["action"] = "replaced"
	}
	addCapNote(out, skill, b.Frozen || *replace)
	if g := strings.TrimSpace(b.Goal); g != "" {
		out["goal"] = g
	}
	out["tasks_queued"] = count
	out["master_plan"] = map[string]any{"id": plan.ID, "subject": plan.Subject}
	return out, nil
}

// addCapNote says, on a new skill that was asked to be active, that it
// started frozen because rpg.MaxActiveSkills were active already.
func addCapNote(out map[string]any, skill db.Skill, frozenAsked bool) {
	if frozenAsked || skill.IsActive {
		return
	}
	out["note"] = fmt.Sprintf("%d skills are already active, so %s starts FROZEN: it gets no directives "+
		"and does not rust. The person can swap it in for an active skill in the TUI.", rpg.MaxActiveSkills, skill.Name)
}

// decodeFileOrJSON reads JSON from --file (a path, or - for stdin) or
// --json (the JSON, or - for stdin), exactly one of them, into v. Unknown
// fields are refused, so a typo fails loudly. what names the JSON in errors.
func decodeFileOrJSON(env Env, file, raw, what string, v any) error {
	var data []byte
	var err error
	switch {
	case (file == "") == (raw == ""):
		return usagef("pass exactly one of --file and --json")
	case file == "-" || raw != "":
		value := raw
		if file == "-" {
			value = "-"
		}
		if data, err = jsonArg(env, "json", value); err != nil {
			return err
		}
	default:
		if data, err = os.ReadFile(file); err != nil {
			return fmt.Errorf("read %s: %w", what, err)
		}
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s JSON: %w", what, err)
	}
	return nil
}

func runExpandTree(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("expand-tree")
	file := fs.String("file", "", "path to the era JSON, or - for stdin")
	raw := fs.String("json", "", "the era JSON, or - for stdin")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, usagef("expand-tree takes one skill name (quote names with spaces)")
	}
	var e game.Expansion
	if err := decodeFileOrJSON(env, *file, *raw, "expansion", &e); err != nil {
		return nil, err
	}
	res, err := game.ExpandTree(ctx, env.Store, pos[0], e)
	if err != nil {
		return nil, err
	}

	out, err := treeOutput(ctx, env.Store, res.Skill.Name)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, n := range out["nodes"].([]nodeOut) {
		if n.Era == res.Era {
			count += len(n.Tasks)
		}
	}
	out["action"] = "expanded"
	out["era"] = map[string]any{"number": res.Era, "title": res.Title}
	out["tasks_queued"] = count
	if res.Plan.ID != 0 {
		out["master_plan"] = map[string]any{"id": res.Plan.ID, "subject": res.Plan.Subject}
	}
	return out, nil
}

func runSetDirectives(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("set-directives")
	raw := fs.String("json", "", "the directives as JSON")
	replace := fs.Bool("replace", false, "replace today's unstarted set")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) > 0 {
		return nil, usagef("set-directives takes no positional arguments")
	}
	data, err := jsonArg(env, "json", *raw)
	if err != nil {
		return nil, err
	}
	var items []game.DirectiveImport
	if err := json.Unmarshal(data, &items); err != nil {
		var wrapped struct {
			Directives []game.DirectiveImport `json:"directives"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil || wrapped.Directives == nil {
			return nil, fmt.Errorf("--json: %w", err)
		}
		items = wrapped.Directives
	}

	day := env.Now().Format("2006-01-02")
	list, err := game.ImportDirectives(ctx, env.Store, day, items, *replace)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "date": day, "directives": toDirectives(list)}, nil
}

func runComplete(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("complete")
	note := fs.String("note", "", "what the person did, kept with the XP")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, usagef("complete takes one directive id")
	}
	id, err := strconv.ParseInt(pos[0], 10, 64)
	if err != nil {
		return nil, usagef("directive id must be a number, got %q", pos[0])
	}
	ev, err := env.Store.CompleteChallenge(ctx, id, *note)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true, "directive_id": id,
		"xp_event": map[string]any{"skill": ev.SkillName, "node": ev.NodeName, "xp": ev.XP,
			"comeback": ev.Comeback, "activity": ev.Activity, "note": ev.Note},
	}, nil
}

func runAddPlan(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("add-plan")
	subject := fs.String("subject", "", "plan title")
	desc := fs.String("desc", "", "what the plan is")
	noXP := fs.Bool("no-xp", false, "save without asking the model for XP")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) > 0 {
		return nil, usagef("add-plan takes --subject and --desc, not positional arguments")
	}
	s, d := strings.TrimSpace(*subject), strings.TrimSpace(*desc)
	if s == "" || d == "" {
		return nil, usagef("--subject and --desc are required")
	}
	if len([]rune(s)) > 120 {
		return nil, usagef("--subject is over 120 characters")
	}

	plan, err := env.Store.CreatePlan(ctx, s, d, d)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"ok": true, "plan": map[string]any{
		"id": plan.ID, "subject": plan.Subject, "description": plan.Description,
		"created": plan.CreatedAt.Local().Format("2006-01-02 15:04"),
	}}
	switch {
	case *noXP:
	case env.Client == nil:
		out["xp_error"] = "no model is set up"
	default:
		// As in the UI: the model picks the skill and node, the formula sets
		// the XP. The plan stays saved if this fails.
		ev, err := game.AwardPlanXP(env.Client, env.Store, plan)
		if err != nil {
			out["xp_error"] = err.Error()
		} else {
			out["xp_event"] = map[string]any{"skill": ev.SkillName, "node": ev.NodeName, "xp": ev.XP, "comeback": ev.Comeback}
		}
	}
	return out, nil
}

func runAddNote(ctx context.Context, env Env, args []string) (any, error) {
	fs := newFlags("add-note")
	noAnalyze := fs.Bool("no-analyze", false, "save without asking the model to read it")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(strings.Join(pos, " "))
	if text == "" {
		return nil, usagef("add-note needs the note text")
	}
	note, err := env.Store.CreateNote(ctx, text)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"ok": true}
	switch {
	case *noAnalyze:
	case env.Client == nil:
		out["analysis_error"] = "no model is set up"
	default:
		if analyzed, err := analyzeNote(ctx, env, note); err != nil {
			out["analysis_error"] = err.Error()
		} else {
			note = analyzed
		}
	}
	out["note"] = toNote(note)
	return out, nil
}

// analyzeNote reads a note with the model, as saving one in the UI does.
func analyzeNote(ctx context.Context, env Env, note db.Note) (db.Note, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	skills, err := env.Store.ListSkills(ctx)
	if err != nil {
		return note, err
	}
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	a, err := rpg.AnalyzeNote(ctx, env.Client, note.Body, names)
	if err != nil {
		return note, err
	}
	return env.Store.SetNoteAnalysis(ctx, note.ID, a)
}

// Main runs the CLI with the process's stdin and stdout.
func Main(store *db.Store, client *llm.Client, args []string) int {
	return Run(context.Background(), Env{Store: store, Client: client, Stdin: os.Stdin, Stdout: os.Stdout}, args)
}
