package rpg

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNodeStatuses(t *testing.T) {
	nodes := []TreeNode{
		{ID: 1, ParentID: 0, Required: 1, Progress: 0, XPNeeded: 40},  // basics, unlocked at level 1
		{ID: 2, ParentID: 1, Required: 2, Progress: 0, XPNeeded: 80},  // needs level 2
		{ID: 3, ParentID: 2, Required: 1, Progress: 80, XPNeeded: 80}, // done, but parent is locked below
		{ID: 4, ParentID: 0, Required: 1, Progress: 40, XPNeeded: 40}, // done directly off the skill
		{ID: 5, ParentID: 99, Required: 1, Progress: 0, XPNeeded: 40}, // parent missing, stays locked
		{ID: 6, ParentID: 1, Required: 1, Progress: 10, XPNeeded: 40}, // waits for node 1 to be done
	}

	st := NodeStatuses(nodes, 1)
	want := map[int64]Status{1: Unlocked, 2: Locked, 3: Locked, 4: Done, 5: Locked, 6: Locked}
	for id, w := range want {
		if st[id] != w {
			t.Errorf("level 1: node %d = %v, want %v", id, st[id], w)
		}
	}

	// The level alone does not open a child while its parent is unfinished.
	st = NodeStatuses(nodes, 2)
	if st[2] != Locked {
		t.Errorf("level 2, parent open: node 2 = %v, want Locked", st[2])
	}

	nodes[0].Progress = 40 // finish node 1
	st = NodeStatuses(nodes, 2)
	if st[6] != Unlocked {
		t.Errorf("level 2: node 6 = %v, want Unlocked", st[6])
	}
	if st[2] != Unlocked {
		t.Errorf("level 2: node 2 = %v, want Unlocked", st[2])
	}
	// Node 3 has its XP, but its parent (node 2) is still open.
	if st[3] != Locked {
		t.Errorf("level 2: node 3 = %v, want Locked", st[3])
	}
}

func TestNodeXPNeeded(t *testing.T) {
	// Lv 2 → 3 is a 183 XP climb, so a Lv 2 node costs ceil(0.6 × 183).
	cases := map[int]int{0: 60, 1: 60, 2: 110, 3: 143, 5: 200}
	for req, want := range cases {
		if got := NodeXPNeeded(req); got != want {
			t.Errorf("NodeXPNeeded(%d) = %d, want %d", req, got, want)
		}
	}
}

// The two nodes a fresh tree opens at a level always pay for the climb to
// the next one.
func TestTwoOpenNodesCoverTheNextLevel(t *testing.T) {
	for lv := 1; lv <= 20; lv++ {
		climb := TotalXPForLevel(lv+1) - TotalXPForLevel(lv)
		if two := 2 * NodeXPNeeded(lv); two < climb {
			t.Errorf("Lv %d: two open nodes give %d XP, the climb is %d", lv, two, climb)
		}
	}
}

func TestNextTierOpensWhenEverythingOpenIsDone(t *testing.T) {
	// A Lv 2 skill: both Lv 2 nodes done, the next tier needs Lv 3.
	nodes := []TreeNode{
		{ID: 1, Required: 1, XPNeeded: 60, Progress: 60},
		{ID: 2, ParentID: 1, Required: 2, XPNeeded: 110, Progress: 110},
		{ID: 3, ParentID: 1, Required: 2, XPNeeded: 110, Progress: 110},
		{ID: 4, ParentID: 2, Required: 3, XPNeeded: 143},
		{ID: 5, ParentID: 3, Required: 3, XPNeeded: 143},
		{ID: 6, ParentID: 4, Required: 5, XPNeeded: 200}, // parent not done: stays locked
		{ID: 7, ParentID: 1, Required: 6, XPNeeded: 240}, // a later tier: stays locked
	}
	st := NodeStatuses(nodes, 2)
	want := map[int64]Status{1: Done, 2: Done, 3: Done, 4: Unlocked, 5: Unlocked, 6: Locked, 7: Locked}
	for id, w := range want {
		if st[id] != w {
			t.Errorf("node %d = %v, want %v", id, st[id], w)
		}
	}

	// With one node still open, the level rule holds as usual.
	nodes[2].Progress = 0
	if st := NodeStatuses(nodes, 2); st[3] != Unlocked || st[4] != Locked || st[5] != Locked {
		t.Errorf("with work open nothing extra opens: %v", st)
	}
}

func TestClassifyPlacesPlanOnNode(t *testing.T) {
	skills := []SkillOutline{{Name: "Go", Nodes: []string{"Syntax", "Interfaces"}}}

	g := &fakeGen{reply: `{"skill":"go","category":"Craft","difficulty":2,"minutes":60,"node":"interfaces","parent":"syntax"}`}
	a, err := Classify(context.Background(), g, "Read about interfaces", "", skills)
	if err != nil {
		t.Fatal(err)
	}
	if a.Node != "Interfaces" || a.Parent != "Syntax" {
		t.Errorf("node/parent = %q/%q, want Interfaces/Syntax", a.Node, a.Parent)
	}

	// A parent without a node is meaningless and is dropped.
	g = &fakeGen{reply: `{"skill":"Go","category":"Craft","difficulty":2,"minutes":60,"node":"","parent":"Syntax"}`}
	a, err = Classify(context.Background(), g, "Misc", "", skills)
	if err != nil {
		t.Fatal(err)
	}
	if a.Node != "" || a.Parent != "" {
		t.Errorf("expected no node, got %q/%q", a.Node, a.Parent)
	}
}

func TestClassifySchemaHasNodeFields(t *testing.T) {
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(classifySchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(schema.Required, ","), "node") {
		t.Error("classify schema must require node")
	}
}
