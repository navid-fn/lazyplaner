package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
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
	if nodes[1].XPNeeded != rpg.NodeXPNeeded(2) {
		t.Errorf("default XP cost = %d, want %d", nodes[1].XPNeeded, rpg.NodeXPNeeded(2))
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

	ev, err := store.CompleteChallenge(ctx, list[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if ev.XP != 3 || ev.NodeName != "Syntax" {
		t.Errorf("event = %+v", ev)
	}
	if _, err := store.CompleteChallenge(ctx, list[0].ID, ""); err == nil {
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
	shieldCases := []struct {
		name    string
		days    []string
		shields []string
		want    int
	}{
		{"a shielded sick day bridges the gap", []string{"2026-10-10", "2026-10-11", "2026-10-13"}, []string{"2026-10-12"}, 3},
		{"a shielded yesterday keeps it alive", []string{"2026-10-10", "2026-10-11"}, []string{"2026-10-12"}, 2},
		{"a shielded today with nothing done", []string{"2026-10-12"}, []string{"2026-10-13"}, 1},
		{"a shield does not count as a day", nil, []string{"2026-10-12", "2026-10-13"}, 0},
	}
	for _, c := range shieldCases {
		set, shields := map[string]bool{}, map[string]bool{}
		for _, d := range c.days {
			set[day(d)] = true
		}
		for _, d := range c.shields {
			shields[day(d)] = true
		}
		if got := streakEndingAt(set, shields, today); got != c.want {
			t.Errorf("%s: streak = %d, want %d", c.name, got, c.want)
		}
	}
	for _, c := range cases {
		set := map[string]bool{}
		for _, d := range c.days {
			set[day(d)] = true
		}
		if got := streakEndingAt(set, nil, today); got != c.want {
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

func TestADayGetsOneSetAndOneBonus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	today := Today()

	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{Tier: rpg.TierRecon, SkillName: "Go", Title: "First", Difficulty: 1, Minutes: 10, XP: 2},
		{Tier: rpg.TierCoreOp, SkillName: "Go", Title: "Second", Difficulty: 3, Minutes: 30, XP: 9},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Go", Title: "Farm", Difficulty: 1, Minutes: 10, XP: 2},
	}); !errors.Is(err, ErrDaySet) {
		t.Fatalf("a second set the same day should fail with ErrDaySet, got %v", err)
	}

	bonus := NewChallenge{SkillName: "Go", NodeName: "Syntax", Title: "Extra", Difficulty: 3, Minutes: 30, XP: 9}
	if err := store.AddBonusChallenge(ctx, today, bonus); !errors.Is(err, ErrSetUnfinished) {
		t.Fatalf("bonus before the set is done: got %v, want ErrSetUnfinished", err)
	}

	list, _ := store.ListChallenges(ctx, today)
	if list[0].Tier != rpg.TierRecon || list[1].Tier != rpg.TierCoreOp {
		t.Errorf("tiers should round-trip, got %v and %v", list[0].Tier, list[1].Tier)
	}
	for _, c := range list {
		if _, err := store.CompleteChallenge(ctx, c.ID, ""); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.AddBonusChallenge(ctx, today, bonus); err != nil {
		t.Fatal(err)
	}
	list, _ = store.ListChallenges(ctx, today)
	if len(list) != 3 || list[2].Tier != rpg.TierOvertime || list[2].NodeName != "Syntax" {
		t.Fatalf("the bonus should join the end of today's list, got %+v", list)
	}
	if _, err := store.CompleteChallenge(ctx, list[2].ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AddBonusChallenge(ctx, today, bonus); !errors.Is(err, ErrBonusUsed) {
		t.Fatalf("a second bonus should fail with ErrBonusUsed, got %v", err)
	}

	st, _ := store.ChallengeStats(ctx, today)
	if st.Total != 3 || st.Done != 3 || st.Score != 20 {
		t.Errorf("stats = %+v, want 3/3 done and score 20", st)
	}
}

func TestRerollReplacesOnlyAnOpenDirective(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	today := Today()

	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{Tier: rpg.TierRecon, SkillName: "Go", Title: "Keep", Difficulty: 1, Minutes: 10, XP: 2},
		{Tier: rpg.TierCoreOp, SkillName: "Go", Title: "Swap", Difficulty: 3, Minutes: 30, XP: 9},
	}); err != nil {
		t.Fatal(err)
	}
	list, _ := store.ListChallenges(ctx, today)

	if err := store.RerollChallenge(ctx, list[1].ID, NewChallenge{Tier: rpg.TierCoreOp, SkillName: "Go",
		NodeName: "Syntax", Title: "Write a table-driven test", Difficulty: 3, Minutes: 40, XP: 12}); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ListChallenges(ctx, today)
	if len(after) != 2 || after[0].Title != "Keep" || after[1].ID != list[1].ID ||
		after[1].Title != "Write a table-driven test" || after[1].NodeName != "Syntax" || after[1].XP != 12 {
		t.Fatalf("reroll should replace only the one directive in place, got %+v", after)
	}

	if _, err := store.CompleteChallenge(ctx, list[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.RerollChallenge(ctx, list[0].ID, NewChallenge{SkillName: "Go", Title: "x"}); err == nil {
		t.Error("a finished directive must not be rerolled")
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

func TestNodeDirectivesAreOnePerNodeAndNotASet(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	today := Today()

	focus := NewChallenge{SkillName: "Go", NodeName: "Interfaces", Title: "Write a Stringer", Difficulty: 3, Minutes: 30, XP: 9}
	if err := store.AddNodeChallenge(ctx, today, focus); err != nil {
		t.Fatal(err)
	}
	// Asking again replaces the open focus directive instead of adding one.
	again := focus
	again.Title, again.Details = "Embed an Interface", "- step one"
	if err := store.AddNodeChallenge(ctx, today, again); err != nil {
		t.Fatalf("a second ask should reroll the open one: %v", err)
	}
	if list, _ := store.ListChallenges(ctx, today); len(list) != 1 || list[0].Title != "Embed an Interface" ||
		list[0].Details != "- step one" {
		t.Fatalf("list = %+v, want the one directive rerolled", list)
	}
	// Once it is finished, the node is done for the day.
	list, _ := store.ListChallenges(ctx, today)
	if _, err := store.CompleteChallenge(ctx, list[0].ID, "embedded io.Reader"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddNodeChallenge(ctx, today, focus); !errors.Is(err, ErrNodeHasDirective) {
		t.Fatalf("a node with a finished directive: got %v, want ErrNodeHasDirective", err)
	}
	if err := store.AddNodeChallenge(ctx, today, NewChallenge{SkillName: "Go", Title: "x"}); err == nil {
		t.Error("a node directive must name a node")
	}

	// The day's set can still be generated, and both show together.
	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{Tier: rpg.TierRecon, SkillName: "Go", Title: "Recon", Difficulty: 1, Minutes: 10, XP: 2},
	}); err != nil {
		t.Fatalf("a focus directive must not block the set: %v", err)
	}
	list, _ = store.ListChallenges(ctx, today)
	if len(list) != 2 || list[0].Tier != rpg.TierFocus || list[1].Tier != rpg.TierRecon {
		t.Errorf("list = %+v", list)
	}
}

func TestStartThenCompleteKeepsTheProof(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)
	today := Today()
	if err := store.SaveChallenges(ctx, today, []NewChallenge{{Tier: rpg.TierRecon, SkillName: "Go",
		NodeName: "Interfaces", Title: "Stringer Drill", Details: "- step", Difficulty: 1, Minutes: 10, XP: 2}}); err != nil {
		t.Fatal(err)
	}
	list, _ := store.ListChallenges(ctx, today)
	if list[0].Started() || list[0].Details != "- step" {
		t.Fatalf("a new directive = %+v", list[0])
	}
	first, err := store.StartChallenge(ctx, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := store.StartChallenge(ctx, list[0].ID)
	if !again.Equal(first.Truncate(0)) && again.Sub(first).Abs() > time.Millisecond {
		t.Errorf("starting twice keeps the first start: %v then %v", first, again)
	}
	list, _ = store.ListChallenges(ctx, today)
	if !list[0].Started() {
		t.Fatal("the directive should be in progress")
	}

	ev, err := store.CompleteChallenge(ctx, list[0].ID, "  printed Point(1,2)  ")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Note != "printed Point(1,2)" {
		t.Errorf("event note = %q", ev.Note)
	}
	list, _ = store.ListChallenges(ctx, today)
	if list[0].CompletionNote != "printed Point(1,2)" || !list[0].Done {
		t.Errorf("challenge = %+v", list[0])
	}
	events, _ := store.ListXPEvents(ctx, 5)
	if events[0].Note != "printed Point(1,2)" {
		t.Errorf("feed note = %q", events[0].Note)
	}
	if _, err := store.StartChallenge(ctx, list[0].ID); err == nil {
		t.Error("a done directive cannot be started")
	}
}

func TestFocusDirectivesStopAtTheDailyCap(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	today := Today()
	// Six skills, one open node each; each gets one focus directive.
	for i := range 6 {
		name := fmt.Sprintf("Skill %d", i)
		if _, err := store.CreateSkillTree(ctx, NewSkillTree{Name: name, Category: "Craft", StartLevel: 1,
			Nodes: []RoadmapNode{{Parent: -1, Name: "Root", RequiredLevel: 1}}}); err != nil {
			t.Fatal(err)
		}
		err := store.AddNodeChallenge(ctx, today, NewChallenge{SkillName: name, NodeName: "Root", Title: "Drill It", XP: 3})
		switch {
		case i < MaxOpenDirectives && err != nil:
			t.Fatalf("directive %d: %v", i+1, err)
		case i >= MaxOpenDirectives && !errors.Is(err, ErrDailyCap):
			t.Fatalf("directive %d: got %v, want ErrDailyCap", i+1, err)
		}
	}
	if list, _ := store.ListChallenges(ctx, today); len(list) != MaxOpenDirectives {
		t.Errorf("got %d directives, want %d", len(list), MaxOpenDirectives)
	}
}

func TestNewSkillsPastTheCapStartFrozen(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for i := range 5 {
		sk, err := store.CreateSkillTree(ctx, NewSkillTree{Name: fmt.Sprintf("Skill %d", i), Category: "Craft",
			StartLevel: 1, Nodes: []RoadmapNode{{Parent: -1, Name: "Root", RequiredLevel: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		if want := i < rpg.MaxActiveSkills; sk.IsActive != want {
			t.Errorf("skill %d active = %v, want %v", i, sk.IsActive, want)
		}
	}
	skills, _ := store.ListSkills(ctx)
	var frozen int64
	active := 0
	for _, s := range skills {
		if s.IsActive {
			active++
		} else {
			frozen = s.ID
		}
	}
	if active != rpg.MaxActiveSkills {
		t.Fatalf("%d active, want %d", active, rpg.MaxActiveSkills)
	}
	if err := store.SetSkillActive(ctx, frozen, true); !errors.Is(err, ErrActiveCap) {
		t.Errorf("a 4th active skill: got %v, want ErrActiveCap", err)
	}
	// Freezing one makes room: the swap is two steps.
	for _, s := range skills {
		if s.IsActive {
			if err := store.SetSkillActive(ctx, s.ID, false); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := store.SetSkillActive(ctx, frozen, true); err != nil {
		t.Errorf("after freezing one: %v", err)
	}
}
