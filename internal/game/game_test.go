package game

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

func newStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.NewStore(filepath.Join(t.TempDir(), "game.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// javaBlueprint has 8 nodes with 3 tasks each, sized recon, core op and
// deep push.
func javaBlueprint() Blueprint {
	b := Blueprint{SkillName: "Java", StartLevel: 1, Category: "Craft", Goal: "Java backend job"}
	b.MasterPlan.Subject = "Java Backend Ramp-Up"
	b.MasterPlan.MarkdownBody = "# Roadmap\n\nWeek 1: the JVM."
	for i := range 8 {
		n := rpg.ExternalNode{Name: fmt.Sprintf("Java Step %d", i+1), Description: "Ship it"}
		for j, mins := range []int{15, 40, 60} {
			n.Tasks = append(n.Tasks, rpg.CuratedTask{Title: fmt.Sprintf("Step %d Part %d", i+1, j+1),
				Difficulty: 2 + j, Minutes: mins})
		}
		b.Nodes = append(b.Nodes, n)
	}
	return b
}

func TestApplyBlueprintSavesEverything(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	skill, plan, err := ApplyBlueprint(ctx, store, javaBlueprint(), false)
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "Java" || plan.Subject != "Java Backend Ramp-Up" {
		t.Errorf("skill %+v plan %+v", skill, plan)
	}
	tasks, _ := store.ListNodeTasks(ctx)
	if len(tasks) != 24 {
		t.Errorf("got %d tasks, want 24", len(tasks))
	}
	plans, _ := store.ListPlans(ctx)
	if len(plans) != 1 || !strings.Contains(plans[0].Description, "# Roadmap") {
		t.Errorf("plans = %+v", plans)
	}

	// A broken blueprint saves nothing: the skill, tasks and plan go together.
	bad := javaBlueprint()
	bad.SkillName = "Kotlin"
	bad.Nodes[7].Tasks = []rpg.CuratedTask{{Title: "Only one"}}
	if _, _, err := ApplyBlueprint(ctx, store, bad, false); err == nil {
		t.Fatal("want an error")
	}
	if skills, _ := store.ListSkills(ctx); len(skills) != 1 {
		t.Errorf("a failed blueprint left %d skills", len(skills))
	}

	// Replacing keeps XP and swaps the queue.
	re := javaBlueprint()
	re.Nodes[0].Tasks = re.Nodes[0].Tasks[:2]
	if _, _, err := ApplyBlueprint(ctx, store, re, true); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := store.ListNodeTasks(ctx); len(tasks) != 23 {
		t.Errorf("after replace: %d tasks, want 23", len(tasks))
	}
}

func TestBuildDayPullsFromTheQueueBeforeTheModel(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	if _, _, err := ApplyBlueprint(ctx, store, javaBlueprint(), false); err != nil {
		t.Fatal(err)
	}
	trees, _ := store.SkillTrees(ctx)
	outlines := []rpg.SkillOutline{{Name: "Java", Level: 1, Nodes: []string{"Java Step 1", "Java Step 2"}}}
	slots := rpg.DailySlots(outlines)
	queue, err := Queue(ctx, store, trees, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 {
		t.Fatalf("queue = %+v, want the first task of the two open roots", queue)
	}

	// No model: the two queued tasks fill two slots; the third is left out.
	drafts, curated, err := BuildDay(ctx, nil, slots, queue, nil)
	if err != nil || curated != 2 || len(drafts) != 2 {
		t.Fatalf("drafts %+v curated %d err %v", drafts, curated, err)
	}
	if err := store.SaveChallenges(ctx, db.Today(), NewChallenges(drafts)); err != nil {
		t.Fatal(err)
	}
	list, _ := store.ListChallenges(ctx, db.Today())
	if list[0].NodeTaskID == 0 {
		t.Fatal("a curated directive should link to its task")
	}

	// Finishing it finishes the task, and the node's next task comes up.
	if _, err := store.CompleteChallenge(ctx, list[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	next, _ := store.NextNodeTasks(ctx)
	found := false
	for _, task := range next {
		if task.Position == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("next tasks = %+v, want a node at position 2", next)
	}

	// Tasks already handed out today are not handed out again.
	queue, _ = Queue(ctx, store, trees, list)
	for _, q := range queue {
		for _, c := range list {
			if q.TaskID == c.NodeTaskID {
				t.Errorf("task %d is in today's list already", q.TaskID)
			}
		}
	}

	// With neither a queue nor a model there is nothing to build.
	if _, _, err := BuildDay(ctx, nil, slots, nil, nil); err == nil {
		t.Error("want an error with no queue and no model")
	}
}

func TestBuildDayUsesTheModelForTheRest(t *testing.T) {
	slots := []rpg.DirectiveSlot{
		{Tier: rpg.TierRecon, Skill: "Java", Nodes: []string{"JVM"}},
		{Tier: rpg.TierCoreOp, Skill: "Go", Nodes: []string{"Channels"}},
	}
	queue := []rpg.QueuedTask{{TaskID: 1, Skill: "Java", Node: "JVM", Title: "Read the JVM spec", Difficulty: 1, Minutes: 15}}
	gen := fakeGen(`{"directives":[{"slot":1,"node":"Channels","title":"Write a worker pool","description":"Done when it runs","difficulty":3,"minutes":30}]}`)
	drafts, curated, err := BuildDay(context.Background(), gen, slots, queue, nil)
	if err != nil || curated != 1 || len(drafts) != 2 {
		t.Fatalf("drafts %+v curated %d err %v", drafts, curated, err)
	}
	if drafts[0].TaskID != 1 || drafts[1].Skill != "Go" || drafts[1].TaskID != 0 {
		t.Errorf("drafts = %+v", drafts)
	}
}

type fakeGen string

func (f fakeGen) GenerateJSON(context.Context, string, string, json.RawMessage) (string, error) {
	return string(f), nil
}

// eraTwo is a valid next era for the Java blueprint, with 2 tasks a node.
func eraTwo() Expansion {
	e := Expansion{EraTitle: "Spring in Production"}
	for i := range 8 {
		n := rpg.ExternalNode{Name: fmt.Sprintf("Spring Step %d", i+1), Description: "Ship more"}
		for j, mins := range []int{15, 45} {
			n.Tasks = append(n.Tasks, rpg.CuratedTask{Title: fmt.Sprintf("Era Step %d Part %d", i+1, j+1),
				Difficulty: 2 + j, Minutes: mins})
		}
		e.Nodes = append(e.Nodes, n)
	}
	return e
}

// finishNodes marks nodes done by giving them their full XP cost, and the
// skill enough XP for its level to keep up.
func finishNodes(t *testing.T, store *db.Store, skill string, upTo int) {
	t.Helper()
	ctx := context.Background()
	trees, _ := store.SkillTrees(ctx)
	for _, tr := range trees {
		if tr.Skill.Name != skill {
			continue
		}
		for _, n := range tr.Nodes[:upTo] {
			if _, err := store.RecordXP(ctx, db.XPAward{SkillName: skill, Category: tr.Skill.Category,
				Activity: "finish " + n.Name, Difficulty: 3, Minutes: 30, XP: n.XPNeeded, NodeID: n.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestExpandTreeAppendsTheNextEra(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	if _, _, err := ApplyBlueprint(ctx, store, javaBlueprint(), false); err != nil {
		t.Fatal(err)
	}

	// Too early: the first era is barely started.
	if _, err := ExpandTree(ctx, store, "Java", eraTwo()); err == nil || !strings.Contains(err.Error(), "still has") {
		t.Fatalf("err = %v, want a not-ready error", err)
	}

	finishNodes(t, store, "Java", 8)
	e := eraTwo()
	e.MasterPlan = &ExpansionPlan{Subject: "Java Era II", MarkdownBody: "# Era II"}
	res, err := ExpandTree(ctx, store, "java", e)
	if err != nil {
		t.Fatal(err)
	}
	if res.Era != 2 || res.Title != "Spring in Production" || res.Plan.ID == 0 {
		t.Errorf("result = %+v", res)
	}

	trees, _ := store.SkillTrees(ctx)
	st := Eras(trees[0])
	if st.Latest != 2 || st.Total != 16 || st.Done != 8 || st.CanExpand {
		t.Errorf("era state: latest %d, %d/%d done, can expand %v", st.Latest, st.Done, st.Total, st.CanExpand)
	}
	var capstone1 int64
	for _, n := range trees[0].Nodes {
		if n.Era == 1 && n.IsCapstone {
			capstone1 = n.ID
		}
	}
	for _, n := range trees[0].Nodes[8:10] {
		if n.ParentID != capstone1 {
			t.Errorf("%s hangs off %d, want the era I capstone %d", n.Name, n.ParentID, capstone1)
		}
		if st.Statuses[n.ID] != rpg.Unlocked {
			t.Errorf("%s is %v; with the capstone done the foundations open", n.Name, st.Statuses[n.ID])
		}
	}
	if tasks, _ := store.ListNodeTasks(ctx); len(tasks) != 24+16 {
		t.Errorf("got %d tasks, want the old 24 kept and 16 added", len(tasks))
	}

	// The new era may not reuse a name, and the era after needs this one done.
	if _, err := ExpandTree(ctx, store, "Java", eraTwo()); err == nil {
		t.Error("want an error: era II is not finished")
	}
}

func TestExpandTreeRefusesRepeatedNames(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	if _, _, err := ApplyBlueprint(ctx, store, javaBlueprint(), false); err != nil {
		t.Fatal(err)
	}
	finishNodes(t, store, "Java", 7) // one node left is enough
	e := eraTwo()
	e.Nodes[3].Name = "Java Step 2"
	if _, err := ExpandTree(ctx, store, "Java", e); err == nil || !strings.Contains(err.Error(), "already in this tree") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ExpandTree(ctx, store, "Java", eraTwo()); err != nil {
		t.Fatalf("with one node left the era should open: %v", err)
	}
}

func TestGenerateEraUsesTheModel(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	if _, _, err := ApplyBlueprint(ctx, store, javaBlueprint(), false); err != nil {
		t.Fatal(err)
	}
	finishNodes(t, store, "Java", 8)
	var nodes []string
	for i := range 8 {
		nodes = append(nodes, fmt.Sprintf(`{"name":"Deep %d","description":"go further"}`, i))
	}
	gen := fakeGen(`{"category":"Craft","nodes":[` + strings.Join(nodes, ",") + `]}`)
	res, err := GenerateEra(ctx, gen, store, "Java", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Era != 2 {
		t.Errorf("era = %d", res.Era)
	}
	all, _ := store.ListNodes(ctx)
	if len(all) != 16 {
		t.Errorf("got %d nodes, want 16", len(all))
	}
}

func TestRomanEra(t *testing.T) {
	for n, want := range map[int]string{1: "I", 2: "II", 4: "IV", 9: "IX", 12: "XII"} {
		if got := RomanEra(n); got != want {
			t.Errorf("RomanEra(%d) = %q, want %q", n, got, want)
		}
	}
}
