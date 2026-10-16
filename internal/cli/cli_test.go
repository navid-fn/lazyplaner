package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/navid-fn/lazyplaner/internal/db"
)

func newEnv(t *testing.T) Env {
	t.Helper()
	store, err := db.NewStore(filepath.Join(t.TempDir(), "cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return Env{Store: store}
}

// run runs one command and decodes its JSON output.
func run(t *testing.T, env Env, stdin string, args ...string) (map[string]any, int) {
	t.Helper()
	var out bytes.Buffer
	env.Stdout, env.Stdin = &out, strings.NewReader(stdin)
	code := Run(context.Background(), env, args)
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("%v: output is not JSON: %v\n%s", args, err, out.String())
	}
	return v, code
}

func javaNodes() string {
	var nodes []map[string]string
	for i := range 8 {
		nodes = append(nodes, map[string]string{"name": fmt.Sprintf("Java Step %d", i+1), "description": "Ship it"})
	}
	b, _ := json.Marshal(nodes)
	return string(b)
}

// nodeIDs returns the ids of the tree's nodes in a state.
func nodeIDs(tree map[string]any, state string) []int64 {
	var ids []int64
	for _, n := range tree["nodes"].([]any) {
		node := n.(map[string]any)
		if node["state"] == state {
			ids = append(ids, int64(node["id"].(float64)))
		}
	}
	return ids
}

func TestSetTreeThenTree(t *testing.T) {
	env := newEnv(t)
	out, code := run(t, env, "", "set-tree", "Java", "--level", "1", "--category", "Craft", "--nodes-json", javaNodes())
	if code != 0 || out["action"] != "created" {
		t.Fatalf("set-tree: code %d out %v", code, out)
	}

	tree, code := run(t, env, "", "tree", "java")
	if code != 0 {
		t.Fatalf("tree: %v", tree)
	}
	if n := len(tree["nodes"].([]any)); n != 8 {
		t.Fatalf("got %d nodes", n)
	}
	// A level-1 tree: two open roots, three locked, and the far ones in fog.
	if open, locked, fog := nodeIDs(tree, "OPEN"), nodeIDs(tree, "LOCKED"), nodeIDs(tree, "SHROUDED"); len(open) != 2 || len(locked) != 3 || len(fog) != 3 {
		t.Errorf("open %v locked %v shrouded %v", open, locked, fog)
	}

	// The same name again needs --replace.
	if _, code := run(t, env, "", "set-tree", "JAVA", "--level", "1", "--nodes-json", javaNodes()); code != 1 {
		t.Errorf("duplicate skill: code %d, want 1", code)
	}
	out, code = run(t, env, javaNodes(), "set-tree", "Java", "--replace", "--nodes-json", "-")
	if code != 0 || out["action"] != "replaced" {
		t.Errorf("replace from stdin: code %d out %v", code, out)
	}
}

func TestSetTreeRejectsBadInput(t *testing.T) {
	env := newEnv(t)
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"set-tree", "Java", "--nodes-json", javaNodes()}, 2},                                        // no level
		{[]string{"set-tree", "Java", "--level", "9", "--category", "Craft", "--nodes-json", javaNodes()}, 1}, // level out of range
		{[]string{"set-tree", "Java", "--level", "1", "--category", "Craft", "--nodes-json", `[{"name":"x"}]`}, 1},
		{[]string{"set-tree", "Basket weaving", "--level", "1", "--nodes-json", javaNodes()}, 1}, // no category known
		{[]string{"set-tree", "Java", "--level", "1", "--nodes-json", "not json"}, 1},
	}
	for _, c := range cases {
		out, code := run(t, env, "", c.args...)
		if code != c.code || out["ok"] != false {
			t.Errorf("%v: code %d out %v, want code %d", c.args, code, out, c.code)
		}
	}
}

func TestDirectivesCompleteAndStatus(t *testing.T) {
	env := newEnv(t)
	env.Now = time.Now
	run(t, env, "", "set-tree", "Java", "--level", "1", "--category", "Craft", "--nodes-json", javaNodes())
	tree, _ := run(t, env, "", "tree", "Java")
	open, locked := nodeIDs(tree, "OPEN"), nodeIDs(tree, "LOCKED")

	set := func(a, b, c int64) string {
		return fmt.Sprintf(`{"directives":[{"tier":"recon","node_id":%d,"title":"Read the JVM spec intro"},`+
			`{"tier":"core_op","node_id":%d,"title":"Write a Gradle build"},`+
			`{"tier":"deep_push","node_id":%d,"title":"Ship a CLI jar"}]}`, a, b, c)
	}
	if out, code := run(t, env, "", "set-directives", "--json", set(open[0], open[1], locked[0])); code != 1 ||
		!strings.Contains(out["error"].(string), "LOCKED") {
		t.Errorf("a locked node should be refused: %v", out)
	}
	if out, code := run(t, env, "", "set-directives", "--json", `[{"tier":"recon","node_id":1,"title":"Read docs"}]`); code != 1 ||
		!strings.Contains(out["error"].(string), "deep_push") {
		t.Errorf("an incomplete set should be refused: %v", out)
	}

	out, code := run(t, env, "", "set-directives", "--json", set(open[0], open[1], open[0]))
	if code != 0 {
		t.Fatalf("set-directives: %v", out)
	}
	list := out["directives"].([]any)
	if len(list) != 3 || list[0].(map[string]any)["tier"] != "recon" {
		t.Fatalf("directives = %v", list)
	}

	if out, code := run(t, env, "", "set-directives", "--json", set(open[0], open[1], open[0])); code != 1 ||
		!strings.Contains(out["error"].(string), "--replace") {
		t.Errorf("a second set should point at --replace: %v", out)
	}
	if _, code := run(t, env, "", "set-directives", "--replace", "--json", set(open[1], open[0], open[1])); code != 0 {
		t.Error("an unstarted set can be replaced")
	}

	status, _ := run(t, env, "", "status")
	today := status["today"].(map[string]any)
	first := int64(today["directives"].([]any)[0].(map[string]any)["id"].(float64))
	out, code = run(t, env, "", "complete", fmt.Sprint(first), "--note", "read chapter 1")
	if code != 0 || out["xp_event"].(map[string]any)["xp"].(float64) <= 0 ||
		out["xp_event"].(map[string]any)["note"] != "read chapter 1" {
		t.Fatalf("complete: %v", out)
	}
	status, _ = run(t, env, "", "status")
	done := status["today"].(map[string]any)["directives"].([]any)[0].(map[string]any)
	if done["state"] != "DONE" || done["completion_note"] != "read chapter 1" {
		t.Errorf("finished directive = %v", done)
	}
	if _, code := run(t, env, "", "complete", fmt.Sprint(first)); code != 1 {
		t.Error("a directive completes once")
	}
	if _, code := run(t, env, "", "set-directives", "--replace", "--json", set(open[0], open[1], open[0])); code != 1 {
		t.Error("a started set cannot be replaced")
	}

	status, _ = run(t, env, "", "status")
	if status["today"].(map[string]any)["done"].(float64) != 1 || status["streak"].(map[string]any)["days"].(float64) != 1 {
		t.Errorf("status today %v streak %v", status["today"], status["streak"])
	}
	skill := status["skills"].([]any)[0].(map[string]any)
	if skill["state"] != "ACTIVE" || skill["stat"] != "DEX" || len(skill["open_nodes"].([]any)) != 2 {
		t.Errorf("skill = %v", skill)
	}
	for _, key := range []string{"character", "recent_notes", "score", "date"} {
		if _, ok := status[key]; !ok {
			t.Errorf("status is missing %q", key)
		}
	}
}

func TestPlansAndNotesWithoutAModel(t *testing.T) {
	env := newEnv(t)
	out, code := run(t, env, "", "add-plan", "--subject", "Fix CI", "--desc", "Repair the pipeline")
	if code != 0 || out["xp_error"] == nil || out["plan"].(map[string]any)["subject"] != "Fix CI" {
		t.Errorf("add-plan: code %d out %v", code, out)
	}
	if _, code := run(t, env, "", "add-plan", "--subject", "Fix CI"); code != 2 {
		t.Error("add-plan needs --desc")
	}

	out, code = run(t, env, "", "add-note", "I was so sick today")
	if code != 0 || out["note"].(map[string]any)["text"] != "I was so sick today" || out["analysis_error"] == nil {
		t.Errorf("add-note: code %d out %v", code, out)
	}
	out, _ = run(t, env, "", "add-note", "--no-analyze", "Ran", "5k")
	if out["note"].(map[string]any)["text"] != "Ran 5k" || out["analysis_error"] != nil {
		t.Errorf("add-note --no-analyze: %v", out)
	}

	status, _ := run(t, env, "", "status")
	if n := len(status["recent_notes"].([]any)); n != 2 {
		t.Errorf("status has %d notes, want 2", n)
	}
}

func TestUsage(t *testing.T) {
	env := newEnv(t)
	out, code := run(t, env, "", "nope")
	if code != 2 || out["ok"] != false {
		t.Errorf("unknown command: code %d out %v", code, out)
	}
	if _, code := run(t, env, "", "complete", "abc"); code != 2 {
		t.Error("a non-numeric id is a usage error")
	}
	if !IsCommand("status") || !IsCommand("set-tree") || IsCommand("--debug") {
		t.Error("IsCommand")
	}
}

func blueprintJSON(t *testing.T) string {
	t.Helper()
	type task struct {
		Title      string `json:"title"`
		Difficulty int    `json:"difficulty"`
		Minutes    int    `json:"minutes"`
	}
	type node struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Tasks       []task `json:"tasks"`
	}
	b := map[string]any{
		"skill_name": "Java", "start_level": 1, "category": "Craft", "goal": "Java backend job",
		"master_plan": map[string]string{"subject": "Java Backend Ramp-Up", "markdown_body": "# Plan"},
	}
	var nodes []node
	for i := range 8 {
		nodes = append(nodes, node{Name: fmt.Sprintf("Java Step %d", i+1), Description: "Ship it", Tasks: []task{
			{fmt.Sprintf("Read step %d docs", i+1), 1, 15},
			{fmt.Sprintf("Build step %d", i+1), 3, 40},
			{fmt.Sprintf("Ship step %d", i+1), 4, 60},
		}})
	}
	b["nodes"] = nodes
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyBlueprint(t *testing.T) {
	env := newEnv(t)
	path := filepath.Join(t.TempDir(), "bp.json")
	if err := os.WriteFile(path, []byte(blueprintJSON(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, env, "", "apply-blueprint", "--file", path)
	if code != 0 || out["tasks_queued"].(float64) != 24 || out["master_plan"].(map[string]any)["subject"] != "Java Backend Ramp-Up" {
		t.Fatalf("apply-blueprint: code %d out %v", code, out)
	}
	if out["nodes"].([]any)[0].(map[string]any)["tasks"] == nil {
		t.Error("the tree should list each node's tasks")
	}

	status, _ := run(t, env, "", "status")
	open := status["skills"].([]any)[0].(map[string]any)["open_nodes"].([]any)
	if open[0].(map[string]any)["next_task"].(map[string]any)["title"] != "Read step 1 docs" {
		t.Errorf("status should show each open node's next task: %v", open[0])
	}

	if _, code := run(t, env, blueprintJSON(t), "apply-blueprint", "--json", "-"); code != 1 {
		t.Error("the same skill again needs --replace")
	}
	if out, code := run(t, env, blueprintJSON(t), "apply-blueprint", "--json", "-", "--replace"); code != 0 || out["action"] != "replaced" {
		t.Errorf("replace: code %d out %v", code, out)
	}
	if out, code := run(t, env, `{"skill_name":"Go","bogus":1}`, "apply-blueprint", "--json", "-"); code != 1 ||
		!strings.Contains(out["error"].(string), "bogus") {
		t.Errorf("unknown fields should be refused: %v", out)
	}
	if _, code := run(t, env, "", "apply-blueprint"); code != 2 {
		t.Error("apply-blueprint needs --file or --json")
	}
}

func TestExpandTreeAddsAnEra(t *testing.T) {
	env := newEnv(t)
	bp := map[string]any{
		"skill_name": "Java", "start_level": 1, "category": "Craft", "goal": "job",
		"master_plan": map[string]any{"subject": "Java", "markdown_body": "# Java"},
		"nodes":       json.RawMessage(javaNodes()),
	}
	data, _ := json.Marshal(bp)
	if out, code := run(t, env, string(data), "apply-blueprint", "--json", "-"); code != 0 {
		t.Fatalf("apply-blueprint: %v", out)
	}

	var nodes []map[string]any
	for i := range 8 {
		nodes = append(nodes, map[string]any{"name": fmt.Sprintf("Spring Step %d", i+1), "description": "More"})
	}
	era, _ := json.Marshal(map[string]any{"era_title": "Spring", "nodes": nodes})

	// Not ready yet: the first era has barely started.
	out, code := run(t, env, string(era), "expand-tree", "Java", "--json", "-")
	if code != 1 || !strings.Contains(out["error"].(string), "still has") {
		t.Fatalf("early expand = %v (exit %d)", out, code)
	}

	// Finish the first era: every node gets its XP.
	tree, _ := run(t, env, "", "tree", "Java")
	for _, n := range tree["nodes"].([]any) {
		node := n.(map[string]any)
		if _, err := env.Store.RecordXP(context.Background(), db.XPAward{SkillName: "Java", Category: "Craft",
			Activity: "finish", Difficulty: 3, Minutes: 30, XP: int(node["xp_needed"].(float64)),
			NodeID: int64(node["id"].(float64))}); err != nil {
			t.Fatal(err)
		}
	}
	status, _ := run(t, env, "", "status")
	skill := status["skills"].([]any)[0].(map[string]any)
	if skill["can_expand"] != true || skill["era"].(float64) != 1 {
		t.Fatalf("status skill = %v", skill)
	}

	out, code = run(t, env, string(era), "expand-tree", "Java", "--json", "-")
	if code != 0 {
		t.Fatalf("expand-tree: %v", out)
	}
	if out["action"] != "expanded" || out["era"].(map[string]any)["number"].(float64) != 2 {
		t.Errorf("out = %v", out)
	}
	if got := len(out["nodes"].([]any)); got != 16 {
		t.Errorf("tree has %d nodes, want 16", got)
	}
	if open := nodeIDs(out, "OPEN"); len(open) != 2 {
		t.Errorf("open nodes = %v, want the two foundations of era II", open)
	}

	// Bad command lines are usage errors.
	if _, code := run(t, env, "", "expand-tree", "--json", "{}"); code != 2 {
		t.Errorf("no skill: exit %d, want 2", code)
	}
	if out, code := run(t, env, "", "expand-tree", "Java", "--json", `{"nodes":[],"typo":1}`); code != 1 ||
		!strings.Contains(out["error"].(string), "typo") {
		t.Errorf("unknown field: %v (exit %d)", out, code)
	}
}

func TestTitlesMustBeShortNames(t *testing.T) {
	env := newEnv(t)
	run(t, env, "", "set-tree", "Java", "--level", "1", "--category", "Craft", "--nodes-json", javaNodes())
	tree, _ := run(t, env, "", "tree", "Java")
	open := nodeIDs(tree, "OPEN")
	long := fmt.Sprintf(`[{"tier":"recon","node_id":%[1]d,"title":"Fill a page with ghosted straight lines between dots"},`+
		`{"tier":"core_op","node_id":%[1]d,"title":"Gradle Build"},{"tier":"deep_push","node_id":%[1]d,"title":"CLI Jar"}]`, open[0])
	out, code := run(t, env, "", "set-directives", "--json", long)
	if code != 1 || !strings.Contains(out["error"].(string), "2-5 word") {
		t.Errorf("a sentence title should be refused: %v", out)
	}
	ok := fmt.Sprintf(`[{"tier":"recon","node_id":%[1]d,"title":"Ghosted Line Drills","details":"- Dot pairs\n- Ghost, then draw"},`+
		`{"tier":"core_op","node_id":%[1]d,"title":"Gradle Build"},{"tier":"deep_push","node_id":%[1]d,"title":"CLI Jar"}]`, open[0])
	out, code = run(t, env, "", "set-directives", "--json", ok)
	if code != 0 {
		t.Fatalf("set-directives: %v", out)
	}
	first := out["directives"].([]any)[0].(map[string]any)
	if first["details"] != "- Dot pairs\n- Ghost, then draw" || first["state"] != "OPEN" {
		t.Errorf("directive = %v", first)
	}
}

func TestAFourthSkillStartsFrozen(t *testing.T) {
	env := newEnv(t)
	for i, name := range []string{"Java", "Go", "Rust", "Zig"} {
		out, code := run(t, env, "", "set-tree", name, "--level", "1", "--category", "Craft", "--nodes-json", javaNodes())
		if code != 0 {
			t.Fatalf("%s: %v", name, out)
		}
		state := out["skill"].(map[string]any)["state"]
		if i < 3 && (state != "ACTIVE" || out["note"] != nil) {
			t.Errorf("%s = %v, want ACTIVE", name, state)
		}
		if i == 3 && (state != "FROZEN" || !strings.Contains(fmt.Sprint(out["note"]), "already active")) {
			t.Errorf("the 4th skill = %v note %v, want FROZEN with a note", state, out["note"])
		}
	}
}
