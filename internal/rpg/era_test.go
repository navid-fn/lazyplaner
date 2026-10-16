package rpg

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestShapeTreeClosesTheDiamond(t *testing.T) {
	_, nodes, err := BuildRoadmap(context.Background(), &fakeGen{reply: eightNodes("Craft")}, "Python", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	// Node 5 joins 2 and 3, node 6 joins 3 and 4, the capstone joins 5 and 6.
	wantMore := map[int][]int{5: {2}, 6: {3}, 7: {6}}
	for i, n := range nodes {
		if !slices.Equal(n.MoreParents, wantMore[i]) {
			t.Errorf("node %d extra parents = %v, want %v", i, n.MoreParents, wantMore[i])
		}
		if n.Capstone != (i == RoadmapSize-1) {
			t.Errorf("node %d capstone = %v", i, n.Capstone)
		}
	}

	// No node but the capstone is a dead end.
	hasChild := map[int]bool{}
	for _, n := range nodes {
		hasChild[n.Parent] = true
		for _, p := range n.MoreParents {
			hasChild[p] = true
		}
	}
	for i := range RoadmapSize - 1 {
		if !hasChild[i] {
			t.Errorf("node %d leads nowhere", i)
		}
	}
}

func TestShortTreeKeepsOnlyJoinsThatExist(t *testing.T) {
	nodes := shapeTree([]RoadNode{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "E"}, {Name: "F"}}, 1)
	for i, n := range nodes {
		for _, p := range n.MoreParents {
			if p >= i {
				t.Errorf("node %d joins %d, which does not come before it", i, p)
			}
		}
	}
	if !nodes[5].Capstone || nodes[4].Capstone {
		t.Error("a short tree ends on its last node")
	}
}

func TestANodeWithTwoParentsWaitsForBoth(t *testing.T) {
	nodes := []TreeNode{
		{ID: 1, Required: 1, XPNeeded: 10, Progress: 10},
		{ID: 2, Required: 1, XPNeeded: 10},
		{ID: 3, ParentID: 1, ExtraParentID: 2, Required: 1, XPNeeded: 10},
	}
	if st := NodeStatuses(nodes, 5)[3]; st != Locked {
		t.Errorf("with one parent open the join is %v, want LOCKED", st)
	}
	nodes[1].Progress = 10
	if st := NodeStatuses(nodes, 5)[3]; st != Unlocked {
		t.Errorf("with both parents done the join is %v, want OPEN", st)
	}
}

func TestShapeEraClimbsFromTheBase(t *testing.T) {
	var in []RoadNode
	for i := range RoadmapSize {
		in = append(in, RoadNode{Name: fmt.Sprintf("E%d", i)})
	}
	base := EraBaseLevel(4, 6) // capstone at Lv 6, so the era starts at Lv 7
	if base != 7 {
		t.Fatalf("base = %d, want 7", base)
	}
	if got := EraBaseLevel(9, 6); got != 9 {
		t.Errorf("a skill already past the capstone starts the era at its level, got %d", got)
	}
	nodes := ShapeEra(in, base)
	want := []int{7, 7, 8, 8, 8, 10, 10, 12}
	for i, n := range nodes {
		if n.RequiredLevel != want[i] {
			t.Errorf("node %d needs Lv %d, want %d", i, n.RequiredLevel, want[i])
		}
	}
	if nodes[0].Parent != -1 || nodes[1].Parent != -1 {
		t.Error("an era's foundations hang off the capstone before it (-1)")
	}
	if !nodes[7].Capstone {
		t.Error("an era ends on a capstone")
	}
}

func TestCanExpand(t *testing.T) {
	statuses := map[int64]Status{1: Done, 2: Done, 3: Unlocked, 4: Locked}
	if CanExpand(statuses, []int64{1, 2, 3, 4}, 4) {
		t.Error("two nodes left and the capstone locked: not ready")
	}
	statuses[3] = Done
	if !CanExpand(statuses, []int64{1, 2, 3, 4}, 4) {
		t.Error("one node left: ready")
	}
	if !CanExpand(map[int64]Status{1: Locked, 2: Done}, []int64{1, 2}, 2) {
		t.Error("capstone done: ready")
	}
	if CanExpand(nil, nil, 0) {
		t.Error("no tree: nothing to expand")
	}
}

func TestShapeExternalEraRefusesNamesInTheTree(t *testing.T) {
	var nodes []ExternalNode
	for i := range RoadmapSize {
		nodes = append(nodes, ExternalNode{Name: fmt.Sprintf("Era Two %d", i)})
	}
	if _, err := ShapeExternalEra(nodes, 3, []string{"Basics"}); err != nil {
		t.Fatal(err)
	}
	_, err := ShapeExternalEra(nodes, 3, []string{"era two 4"})
	if err == nil || !strings.Contains(err.Error(), "already in this tree") {
		t.Errorf("err = %v, want a clash with the existing node", err)
	}
}

func TestBuildEraRoadmapDropsRepeatedNodes(t *testing.T) {
	g := &fakeGen{reply: eightNodes("Craft")}
	nodes, err := BuildEraRoadmap(context.Background(), g, "Python", 2, 7, "", []string{"N0", "N3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != RoadmapSize-2 {
		t.Fatalf("got %d nodes, want the 6 that are new", len(nodes))
	}
	for _, n := range nodes {
		if n.Name == "N0" || n.Name == "N3" {
			t.Errorf("%s is already in the tree", n.Name)
		}
		if n.RequiredLevel < 7 {
			t.Errorf("%s needs Lv %d, below the era's base", n.Name, n.RequiredLevel)
		}
	}
}

func TestAFinishedCapstoneOpensTheNextEraBelowItsLevel(t *testing.T) {
	// The capstone (Lv 6) got its XP while the skill is only Lv 3, so it is
	// done by the never-stuck rule; the next era (Lv 7) must open behind it.
	nodes := []TreeNode{
		{ID: 1, Required: 1, XPNeeded: 10, Progress: 10},
		{ID: 2, ParentID: 1, Required: 6, XPNeeded: 10, Progress: 10},
		{ID: 3, ParentID: 2, Required: 7, XPNeeded: 10},
		{ID: 4, ParentID: 2, Required: 7, XPNeeded: 10},
	}
	st := NodeStatuses(nodes, 3)
	if st[2] != Done || st[3] != Unlocked || st[4] != Unlocked {
		t.Errorf("statuses = %v, want the capstone done and both foundations open", st)
	}
}
