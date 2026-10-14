package ui

import (
	"context"
	"testing"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// The model only sees active skills and their open or rusty nodes, never a
// locked node (Java Web Development at Lv 9) or a garrisoned skill.
func TestDirectiveOutlinesSkipLockedNodesAndInactiveSkills(t *testing.T) {
	ctx := context.Background()
	m := newFlowModel(t)
	store := m.store

	java := db.NewSkillTree{Name: "Java", Category: "Craft", StartLevel: 1, Nodes: []db.RoadmapNode{
		{Parent: -1, Name: "JVM Memory", RequiredLevel: 1},
		{Parent: 0, Name: "Streams", RequiredLevel: 1}, // waits for JVM Memory
		{Parent: -1, Name: "Maven", RequiredLevel: 1},
		{Parent: -1, Name: "Java Web Development", RequiredLevel: 9},
	}}
	body := db.NewSkillTree{Name: "Body", Category: "Body", StartLevel: 1, Garrisoned: true,
		Nodes: []db.RoadmapNode{{Parent: -1, Name: "Core Rotation", RequiredLevel: 1}}}
	for _, tr := range []db.NewSkillTree{java, body} {
		if _, err := store.CreateSkillTree(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		t.Fatal(err)
	}

	out := directiveOutlines(trees)
	if len(out) != 1 || out[0].Name != "Java" {
		t.Fatalf("only the active skill should be offered, got %+v", out)
	}
	want := []string{"JVM Memory", "Maven"}
	if len(out[0].Nodes) != len(want) || out[0].Nodes[0] != want[0] || out[0].Nodes[1] != want[1] {
		t.Errorf("open nodes = %v, want %v", out[0].Nodes, want)
	}

	slots := rpg.DailySlots(out)
	if len(slots) != 3 || slots[0].Tier != rpg.TierRecon || slots[2].Tier != rpg.TierDeepPush {
		t.Errorf("one active skill still gets the three-tier set, got %+v", slots)
	}

	// A reroll keeps its node while it is open.
	slot := rerollSlot(db.Challenge{Tier: rpg.TierCoreOp, SkillName: "Java", NodeName: "Maven"}, trees)
	if len(slot.Nodes) != 1 || slot.Nodes[0] != "Maven" || slot.Tier != rpg.TierCoreOp {
		t.Errorf("reroll slot = %+v", slot)
	}
	// One that points at a locked node falls back to the open ones.
	slot = rerollSlot(db.Challenge{SkillName: "Java", NodeName: "Java Web Development"}, trees)
	if len(slot.Nodes) != 2 || slot.Tier != rpg.TierCoreOp {
		t.Errorf("reroll off a locked node = %+v", slot)
	}
}

func TestBonusGoesToTheLeastWorkedSkill(t *testing.T) {
	outlines := []rpg.SkillOutline{{Name: "Go"}, {Name: "Java"}, {Name: "Drawing"}}
	list := []db.Challenge{{SkillName: "Go"}, {SkillName: "Java"}, {SkillName: "Go"}}
	if s := bonusSlot(outlines, list); s.Skill != "Drawing" || s.Tier != rpg.TierOvertime {
		t.Errorf("bonus slot = %+v, want Drawing overtime", s)
	}
}
