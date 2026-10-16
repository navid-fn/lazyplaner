package rpg

import (
	"fmt"
	"testing"
)

func agentNodes() []ExternalNode {
	var nodes []ExternalNode
	for i := range RoadmapSize {
		nodes = append(nodes, ExternalNode{Name: fmt.Sprintf("Node %d", i+1), Description: "Ship something"})
	}
	return nodes
}

func TestShapeExternalTreeUsesTheFixedShape(t *testing.T) {
	got, err := ShapeExternalTree(agentNodes(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range got {
		if n.Parent != treeParents[i] || n.RequiredLevel != nodeLevel(i, 1) {
			t.Errorf("node %d: parent %d level %d, want %d %d", i, n.Parent, n.RequiredLevel, treeParents[i], nodeLevel(i, 1))
		}
	}

	bad := map[string][]ExternalNode{
		"too few":   agentNodes()[:7],
		"duplicate": append(agentNodes()[:7], ExternalNode{Name: "node 1"}),
		"unnamed":   append(agentNodes()[:7], ExternalNode{Name: "  "}),
	}
	for name, nodes := range bad {
		if _, err := ShapeExternalTree(nodes, 1); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestCheckDaySet(t *testing.T) {
	ok := [][]Tier{
		{TierRecon, TierCoreOp, TierDeepPush},
		{TierDeepPush, TierCoreOp, TierRecon, TierCoreOp},
		{TierRecovery},
		{TierRecovery, TierRecovery},
	}
	for _, tiers := range ok {
		if err := CheckDaySet(tiers); err != nil {
			t.Errorf("%v: %v", tiers, err)
		}
	}
	bad := [][]Tier{
		{TierRecon, TierCoreOp},
		{TierRecon, TierRecon, TierCoreOp, TierDeepPush},
		{TierRecon, TierCoreOp, TierDeepPush, TierRecovery},
		{TierRecovery, TierRecovery, TierRecovery},
		{TierRecon, TierCoreOp, TierCoreOp, TierCoreOp, TierDeepPush},
	}
	for _, tiers := range bad {
		if err := CheckDaySet(tiers); err == nil {
			t.Errorf("%v: want an error", tiers)
		}
	}
}

func TestExternalDirectiveIsHeldToItsTier(t *testing.T) {
	d, err := ExternalDirective(TierRecon, "Go", "Syntax", "Write a loop challenge", "", "", 5, 200)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Write a loop" || d.Difficulty != 2 || d.Minutes != 15 || d.XP != Award(Activity{Difficulty: 2, Minutes: 15}) {
		t.Errorf("draft = %+v", d)
	}
	if _, err := ExternalDirective(TierOvertime, "Go", "", "Write a loop", "", "", 0, 0); err == nil {
		t.Error("the bonus tier is earned, not set")
	}
	for _, name := range []string{"recon", "Core Op", "deep-push", "RECOVERY"} {
		if _, err := ParseTier(name); err != nil {
			t.Errorf("ParseTier(%q): %v", name, err)
		}
	}
	if _, err := ParseTier("bonus"); err == nil {
		t.Error("bonus is not a tier an agent sets")
	}
}
