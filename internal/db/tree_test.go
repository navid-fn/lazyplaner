package db

import (
	"context"
	"testing"
	"time"
)

func seedGoTree(t *testing.T, store *Store) {
	t.Helper()
	// Start level 2 is 100 XP. Syntax (level 1) is below it, so it starts
	// done; Interfaces (level 2) starts open.
	_, err := store.CreateSkillTree(context.Background(), NewSkillTree{
		Name: "Go", Category: "Craft", StartLevel: 2,
		Nodes: []RoadmapNode{
			{Parent: -1, Name: "Syntax", Description: "Basics", RequiredLevel: 1},
			{Parent: 0, Name: "Interfaces", Description: "Implicit", RequiredLevel: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateSkillTreeStoresSkillAndNodes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)

	trees, err := store.SkillTrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 1 || trees[0].Skill.TotalXP != 100 {
		t.Fatalf("unexpected trees %+v", trees)
	}
	nodes := trees[0].Nodes
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	if nodes[0].ParentID != 0 || nodes[1].ParentID != nodes[0].ID {
		t.Errorf("parent links wrong: %+v", nodes)
	}
	if nodes[1].XPNeeded != 80 {
		t.Errorf("default XP cost = %d, want 80", nodes[1].XPNeeded)
	}

	if _, err := store.CreateSkillTree(ctx, NewSkillTree{Name: "go", Category: "Craft"}); err == nil {
		t.Error("expected duplicate skill name to be rejected")
	}
}

func TestCreateSkillTreeRejectsForwardParent(t *testing.T) {
	store := newTestStore(t)
	_, err := store.CreateSkillTree(context.Background(), NewSkillTree{
		Name: "X", Category: "Mind",
		Nodes: []RoadmapNode{{Parent: 1, Name: "Bad", RequiredLevel: 1}},
	})
	if err == nil {
		t.Error("expected error for a parent that comes later")
	}
}

func TestRecordXPCreatesNodeAndCreditsProgress(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)

	ev, err := store.RecordXP(ctx, XPAward{
		Activity: "Read about closures", SkillName: "Go", Category: "Craft",
		Difficulty: 2, Minutes: 60, XP: 24,
		NodeName: "Closures", ParentNode: "Syntax", NodeRequired: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.NodeName != "Closures" {
		t.Errorf("event node = %q, want Closures", ev.NodeName)
	}

	trees, err := store.SkillTrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var closures *Node
	for i, n := range trees[0].Nodes {
		if n.Name == "Closures" {
			closures = &trees[0].Nodes[i]
		}
	}
	if closures == nil {
		t.Fatal("node Closures was not created")
	}
	if closures.Progress != 24 {
		t.Errorf("node progress = %d, want 24", closures.Progress)
	}
	if closures.ParentID == 0 {
		t.Error("node should be a child of Syntax, not the skill root")
	}

	// Reusing the name credits the same node rather than creating another.
	if _, err := store.RecordXP(ctx, XPAward{
		Activity: "More closures", SkillName: "Go", Category: "Craft", Difficulty: 1, Minutes: 10, XP: 5,
		NodeName: "closures",
	}); err != nil {
		t.Fatal(err)
	}
	nodes, err := store.ListNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, n := range nodes {
		if n.Name == "Closures" {
			count++
			if n.Progress != 29 {
				t.Errorf("progress after second award = %d, want 29", n.Progress)
			}
		}
	}
	if count != 1 {
		t.Errorf("found %d Closures nodes, want 1", count)
	}
}

func TestCompleteChallengeAwardsOnceAndCountsScore(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)

	today := Today()
	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Go", NodeName: "Syntax", Title: "Write a loop", Difficulty: 1, Minutes: 15, XP: 3},
		{SkillName: "Go", Title: "Read a blog post", Difficulty: 2, Minutes: 20, XP: 8},
	}); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListChallenges(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].NodeName != "Syntax" {
		t.Fatalf("unexpected challenges %+v", list)
	}

	ev, err := store.CompleteChallenge(ctx, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.XP != 3 || ev.NodeName != "Syntax" {
		t.Errorf("event = %+v", ev)
	}
	if _, err := store.CompleteChallenge(ctx, list[0].ID); err == nil {
		t.Error("completing the same challenge twice should fail")
	}

	st, err := store.ChallengeStats(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if st.Done != 1 || st.Total != 2 || st.Score != 3 || st.Streak != 1 {
		t.Errorf("stats = %+v, want Done 1 Total 2 Score 3 Streak 1", st)
	}

	skills, err := store.ListSkills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if skills[0].TotalXP != 103 {
		t.Errorf("skill XP = %d, want 103 (100 start + 3)", skills[0].TotalXP)
	}
}

func TestStreakEndingAt(t *testing.T) {
	day := func(s string) string {
		t, _ := time.Parse(dayLayout, s)
		return t.Format(dayLayout)
	}
	today, _ := time.Parse(dayLayout, "2026-10-13")

	cases := []struct {
		name string
		days []string
		want int
	}{
		{"none", nil, 0},
		{"today only", []string{"2026-10-13"}, 1},
		{"three in a row", []string{"2026-10-11", "2026-10-12", "2026-10-13"}, 3},
		{"gap breaks it", []string{"2026-10-10", "2026-10-12", "2026-10-13"}, 2},
		{"yesterday keeps it alive before today is done", []string{"2026-10-11", "2026-10-12"}, 2},
		{"old streak is over", []string{"2026-10-09", "2026-10-10"}, 0},
	}
	for _, c := range cases {
		set := map[string]bool{}
		for _, d := range c.days {
			set[day(d)] = true
		}
		if got := streakEndingAt(set, today); got != c.want {
			t.Errorf("%s: streak = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestUpdateSkillResetsToLevelOne(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)

	if _, err := store.RecordXP(ctx, XPAward{
		Activity: "Learn", SkillName: "Go", Category: "Craft", Difficulty: 3, Minutes: 120, XP: 300,
		NodeName: "Syntax",
	}); err != nil {
		t.Fatal(err)
	}

	skills, _ := store.ListSkills(ctx)
	if err := store.UpdateSkill(ctx, skills[0].ID, "Golang", "Mind"); err != nil {
		t.Fatal(err)
	}

	trees, err := store.SkillTrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sk := trees[0].Skill
	if sk.Name != "Golang" || sk.Category != "Mind" || sk.TotalXP != 0 {
		t.Errorf("skill after edit = %+v, want Golang/Mind at 0 XP", sk)
	}
	for _, n := range trees[0].Nodes {
		if n.Progress != 0 {
			t.Errorf("node %s progress = %d, want 0", n.Name, n.Progress)
		}
	}
	if len(trees[0].Nodes) != 2 {
		t.Errorf("tree should be kept, got %d nodes", len(trees[0].Nodes))
	}

	// History is kept even though the XP total is reset.
	events, _ := store.ListXPEvents(ctx, 10)
	if len(events) != 1 || events[0].XP != 300 {
		t.Errorf("XP history should be kept, got %+v", events)
	}
}

func TestUpdateSkillRejectsClash(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	if _, err := store.CreateSkillTree(ctx, NewSkillTree{Name: "Running", Category: "Body"}); err != nil {
		t.Fatal(err)
	}
	skills, _ := store.ListSkills(ctx)
	var goID int64
	for _, s := range skills {
		if s.Name == "Go" {
			goID = s.ID
		}
	}
	if err := store.UpdateSkill(ctx, goID, "running", "Craft"); err == nil {
		t.Error("renaming to an existing skill (any case) should fail")
	}
	// Keeping the same name is allowed.
	if err := store.UpdateSkill(ctx, goID, "Go", "Body"); err != nil {
		t.Errorf("same name should be allowed: %v", err)
	}
}

func TestNewBatchReplacesTodaysListButKeepsScore(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	today := Today()

	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Go", Title: "First set", Difficulty: 1, Minutes: 10, XP: 2},
	}); err != nil {
		t.Fatal(err)
	}
	first, _ := store.ListChallenges(ctx, today)
	if _, err := store.CompleteChallenge(ctx, first[0].ID); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Go", Title: "Second set A", Difficulty: 1, Minutes: 10, XP: 3},
		{SkillName: "Go", Title: "Second set B", Difficulty: 1, Minutes: 10, XP: 3},
	}); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListChallenges(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Title != "Second set A" {
		t.Fatalf("today should show only the latest set, got %+v", list)
	}

	st, err := store.ChallengeStats(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 2 || st.Done != 0 {
		t.Errorf("stats should describe the latest set, got %+v", st)
	}
	if st.Score != 2 {
		t.Errorf("score should keep the finished challenge from the first set, got %d", st.Score)
	}
}

func TestChallengeDescriptionIsStoredAndListed(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	today := Today()

	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Go", Title: "Write a loop", Description: "Write a for loop that sums 1 to 10 and prints it.", Difficulty: 1, Minutes: 15, XP: 3},
	}); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListChallenges(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d challenges, want 1", len(list))
	}
	if list[0].Description != "Write a for loop that sums 1 to 10 and prints it." {
		t.Errorf("description = %q", list[0].Description)
	}
}
