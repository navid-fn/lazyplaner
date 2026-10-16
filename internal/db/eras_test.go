package db

import (
	"context"
	"fmt"
	"testing"
)

// oldWiredTree creates a skill whose eight nodes are wired the way trees
// were before the diamond was closed: no second parents, no capstone.
func oldWiredTree(t *testing.T, store *Store, name string) {
	t.Helper()
	var nodes []RoadmapNode
	for i, p := range oldTreeParents {
		nodes = append(nodes, RoadmapNode{Parent: p, Name: fmt.Sprintf("%s %d", name, i), RequiredLevel: 1})
	}
	if _, err := store.CreateSkillTree(context.Background(),
		NewSkillTree{Name: name, Category: "Craft", StartLevel: 1, Nodes: nodes}); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillClosesOldDiamonds(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	oldWiredTree(t, store, "Go")
	if err := store.backfillDiamonds(ctx); err != nil {
		t.Fatal(err)
	}
	// Running it again changes nothing.
	if err := store.backfillDiamonds(ctx); err != nil {
		t.Fatal(err)
	}
	nodes, _ := store.ListNodes(ctx)
	if len(nodes) != 8 {
		t.Fatalf("got %d nodes", len(nodes))
	}
	want := map[int]int{5: 2, 6: 3, 7: 6}
	for i, n := range nodes {
		var wantID int64
		if j, ok := want[i]; ok {
			wantID = nodes[j].ID
		}
		if n.ExtraParentID != wantID {
			t.Errorf("node %d extra parent = %d, want %d", i, n.ExtraParentID, wantID)
		}
		if n.IsCapstone != (i == 7) {
			t.Errorf("node %d capstone = %v", i, n.IsCapstone)
		}
		if n.Era != 1 {
			t.Errorf("node %d is in era %d, want 1", i, n.Era)
		}
	}
}

func TestBackfillMarksACapstoneOnOtherTrees(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store) // two nodes, not the old shape
	if err := store.backfillDiamonds(ctx); err != nil {
		t.Fatal(err)
	}
	nodes, _ := store.ListNodes(ctx)
	if nodes[0].IsCapstone || !nodes[1].IsCapstone {
		t.Errorf("the highest node should be the capstone: %+v", nodes)
	}
}

func TestAppendEraHangsOffTheCapstone(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	oldWiredTree(t, store, "Go")
	if err := store.backfillDiamonds(ctx); err != nil {
		t.Fatal(err)
	}
	nodes, _ := store.ListNodes(ctx)
	capstone := nodes[7]

	era := []RoadmapNode{
		{Parent: -1, Name: "Era2 A", RequiredLevel: 7},
		{Parent: -1, Name: "Era2 B", RequiredLevel: 7},
		{Parent: 0, Name: "Era2 C", RequiredLevel: 8, Capstone: true, MoreParents: []int{1},
			Tasks: []NewNodeTask{{Title: "Do one", Difficulty: 1, Minutes: 15}, {Title: "Do two", Difficulty: 3, Minutes: 40}}},
	}
	plan, err := store.AppendEra(ctx, NewEra{SkillID: capstone.SkillID, Era: 2, CapstoneID: capstone.ID,
		Title: "Concurrency", Nodes: era, Level: 1, PlanSubject: "Era II", PlanBody: "# Next"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID == 0 {
		t.Error("the era's master plan should be saved")
	}

	nodes, _ = store.ListNodes(ctx)
	if len(nodes) != 11 {
		t.Fatalf("got %d nodes, want the 8 old ones kept and 3 added", len(nodes))
	}
	a, b, c := nodes[8], nodes[9], nodes[10]
	if a.ParentID != capstone.ID || b.ParentID != capstone.ID {
		t.Errorf("foundations hang off %d and %d, want the capstone %d", a.ParentID, b.ParentID, capstone.ID)
	}
	if c.ParentID != a.ID || c.ExtraParentID != b.ID || !c.IsCapstone || c.Era != 2 {
		t.Errorf("era node = %+v", c)
	}
	if a.Progress != 0 {
		t.Error("a new era starts with nothing done")
	}
	tasks, _ := store.ListNodeTasks(ctx)
	if len(tasks) != 2 || tasks[0].NodeID != c.ID {
		t.Errorf("tasks = %+v", tasks)
	}
	eras, _ := store.ListEras(ctx)
	if len(eras) != 1 || eras[0].Title != "Concurrency" || eras[0].Era != 2 {
		t.Errorf("eras = %+v", eras)
	}

	// The same era cannot be added twice.
	if _, err := store.AppendEra(ctx, NewEra{SkillID: capstone.SkillID, Era: 2, CapstoneID: c.ID,
		Nodes: []RoadmapNode{{Parent: -1, Name: "Again", RequiredLevel: 9}}, Level: 1}); err == nil {
		t.Error("want an error for an era out of order")
	}
	// A capstone from another skill is refused.
	oldWiredTree(t, store, "Rust")
	if _, err := store.AppendEra(ctx, NewEra{SkillID: capstone.SkillID, Era: 3, CapstoneID: 999,
		Nodes: []RoadmapNode{{Parent: -1, Name: "Lost", RequiredLevel: 9}}, Level: 1}); err == nil {
		t.Error("want an error for a capstone outside the skill")
	}

	// Replacing the tree clears every era.
	if _, err := store.ReplaceSkillNodes(ctx, "Go", []RoadmapNode{{Parent: -1, Name: "Fresh", RequiredLevel: 1}}); err != nil {
		t.Fatal(err)
	}
	if eras, _ := store.ListEras(ctx); len(eras) != 0 {
		t.Errorf("eras after replace = %+v", eras)
	}
}
