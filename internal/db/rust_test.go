package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// levelFourTree has one node at each level from 1 to 6, in a chain.
func levelFourTree(t *testing.T, store *Store, rusty bool) Skill {
	t.Helper()
	var nodes []RoadmapNode
	for lv := 1; lv <= 6; lv++ {
		nodes = append(nodes, RoadmapNode{Parent: lv - 2, Name: "Node " + string(rune('0'+lv)), RequiredLevel: lv})
	}
	sk, err := store.CreateSkillTree(context.Background(), NewSkillTree{
		Name: "Python", Category: "Craft", StartLevel: 4, Rusty: rusty, Nodes: nodes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sk
}

func nodesByName(t *testing.T, store *Store) map[string]Node {
	t.Helper()
	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Node{}
	for _, n := range nodes {
		out[n.Name] = n
	}
	return out
}

func TestCreateSkillTreePremastersNodesBelowStartLevel(t *testing.T) {
	store := newTestStore(t)
	sk := levelFourTree(t, store, false)

	if sk.TotalXP != rpg.TotalXPForLevel(4) {
		t.Errorf("start XP = %d, want the XP for level 4 (%d)", sk.TotalXP, rpg.TotalXPForLevel(4))
	}

	nodes := nodesByName(t, store)
	var tree []rpg.TreeNode
	for lv := 1; lv <= 6; lv++ {
		n := nodes["Node "+string(rune('0'+lv))]
		wantDone := lv < 4
		if (n.Progress == n.XPNeeded) != wantDone {
			t.Errorf("level %d node: progress %d/%d, want done=%v", lv, n.Progress, n.XPNeeded, wantDone)
		}
		if lv >= 4 && n.Progress != 0 {
			t.Errorf("level %d node should start empty, progress %d", lv, n.Progress)
		}
		tree = append(tree, rpg.TreeNode{ID: n.ID, ParentID: n.ParentID, Required: n.RequiredLevel,
			Progress: n.Progress, XPNeeded: n.XPNeeded, Rusty: n.IsRusty})
	}

	st := rpg.NodeStatuses(tree, 4)
	want := []rpg.Status{rpg.Done, rpg.Done, rpg.Done, rpg.Unlocked, rpg.Locked, rpg.Locked}
	for i, w := range want {
		if got := st[tree[i].ID]; got != w {
			t.Errorf("level %d node status = %v, want %v", i+1, got, w)
		}
	}
}

func TestCreateSkillTreeStartsRustyAtTheFrontier(t *testing.T) {
	store := newTestStore(t)
	levelFourTree(t, store, true)

	nodes := nodesByName(t, store)
	for name, n := range nodes {
		want := name == "Node 4" // the open node at the start level
		if n.IsRusty != want {
			t.Errorf("%s rusty = %v, want %v", name, n.IsRusty, want)
		}
	}
}

func TestCreateSkillTreeStartsThePracticeClock(t *testing.T) {
	store := newTestStore(t)
	levelFourTree(t, store, false)
	skills, err := store.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !skills[0].IsActive {
		t.Error("a new skill should start in the active stance")
	}
	if time.Since(skills[0].LastPracticed) > time.Minute {
		t.Errorf("last practiced = %v, want about now", skills[0].LastPracticed)
	}
}

func TestPractisingARustyNodeGivesComebackXPAndClearsRust(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	levelFourTree(t, store, true)
	before := nodesByName(t, store)["Node 4"]

	ev, err := store.RecordXP(ctx, XPAward{Activity: "Practice", SkillName: "Python", Category: "Craft",
		Difficulty: 2, Minutes: 40, XP: 8, NodeName: "Node 4"})
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Comeback || ev.XP != rpg.ComebackXP(8) {
		t.Errorf("event = %+v, want comeback with %d XP", ev, rpg.ComebackXP(8))
	}

	after := nodesByName(t, store)["Node 4"]
	if after.IsRusty {
		t.Error("practising the node should clear its rust")
	}
	if after.Progress-before.Progress != rpg.ComebackXP(8) {
		t.Errorf("node gained %d, want the comeback XP %d", after.Progress-before.Progress, rpg.ComebackXP(8))
	}

	// The next award on the same node is back to normal.
	ev, err = store.RecordXP(ctx, XPAward{Activity: "Again", SkillName: "Python", Category: "Craft",
		Difficulty: 2, Minutes: 40, XP: 8, NodeName: "Node 4"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Comeback || ev.XP != 8 {
		t.Errorf("second award = %+v, want plain 8 XP", ev)
	}
}

func TestCompletingAChallengeOnARustyNodeCountsTheBonus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	levelFourTree(t, store, true)
	today := Today()
	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Python", NodeName: "Node 4", Title: "Warm up", Difficulty: 1, Minutes: 20, XP: 4},
	}); err != nil {
		t.Fatal(err)
	}
	list, _ := store.ListChallenges(ctx, today)

	ev, err := store.CompleteChallenge(ctx, list[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Comeback || ev.XP != rpg.ComebackXP(4) {
		t.Errorf("challenge award = %+v, want comeback %d", ev, rpg.ComebackXP(4))
	}
	st, _ := store.ChallengeStats(ctx, today)
	if st.Score != rpg.ComebackXP(4) {
		t.Errorf("score = %d, should count the bonus (%d)", st.Score, rpg.ComebackXP(4))
	}
	if nodesByName(t, store)["Node 4"].IsRusty {
		t.Error("finishing a challenge on the node should clear its rust")
	}
}

func TestRecordXPUpdatesLastPracticed(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	levelFourTree(t, store, false)
	old := time.Now().Add(-30 * 24 * time.Hour)
	if _, err := store.db.ExecContext(ctx, `UPDATE skills SET last_practiced_at = ?`, formatPracticed(old)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RecordXP(ctx, XPAward{Activity: "a", SkillName: "Python", Category: "Craft",
		Difficulty: 1, Minutes: 10, XP: 2}); err != nil {
		t.Fatal(err)
	}
	skills, _ := store.ListSkills(ctx)
	if time.Since(skills[0].LastPracticed) > time.Minute {
		t.Errorf("last practiced = %v, want about now after earning XP", skills[0].LastPracticed)
	}
}

func TestSetNodeRustyAndSkillStance(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	sk := levelFourTree(t, store, false)
	n := nodesByName(t, store)["Node 4"]

	if err := store.SetNodeRusty(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	if !nodesByName(t, store)["Node 4"].IsRusty {
		t.Error("SetNodeRusty(true) should mark the node")
	}
	if err := store.SetSkillActive(ctx, sk.ID, false); err != nil {
		t.Fatal(err)
	}
	skills, _ := store.ListSkills(ctx)
	if skills[0].IsActive {
		t.Error("SetSkillActive(false) should garrison the skill")
	}
	if err := store.SetNodeRusty(ctx, 9999, true); err == nil {
		t.Error("an unknown node should be an error")
	}
}

func TestUpdateSkillClearsRust(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	sk := levelFourTree(t, store, true)
	if err := store.UpdateSkill(ctx, sk.ID, "Python", "Craft"); err != nil {
		t.Fatal(err)
	}
	for name, n := range nodesByName(t, store) {
		if n.IsRusty {
			t.Errorf("%s still rusty after the skill was reset", name)
		}
	}
}

// TestMigrationFromOldDatabase opens a database made before stances and
// rust existed: the columns are added, and last practiced is backfilled
// from the newest XP event.
func TestMigrationFromOldDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE skills (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			category TEXT NOT NULL, total_xp INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE skill_nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, skill_id INTEGER NOT NULL,
			parent_id INTEGER NOT NULL DEFAULT 0, name TEXT NOT NULL COLLATE NOCASE, description TEXT NOT NULL DEFAULT '',
			required_level INTEGER NOT NULL, xp_needed INTEGER NOT NULL, progress INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE xp_events (id INTEGER PRIMARY KEY AUTOINCREMENT, skill_id INTEGER NOT NULL,
			plan_id INTEGER NOT NULL DEFAULT 0, activity TEXT NOT NULL, difficulty INTEGER NOT NULL,
			minutes INTEGER NOT NULL, xp INTEGER NOT NULL, created_at DATETIME NOT NULL)`,
		`INSERT INTO skills (name, category, total_xp) VALUES ('Go', 'Mind', 50), ('Chess', 'Mind', 0)`,
	}
	for _, s := range stmts {
		if _, err := raw.ExecContext(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	practised := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO xp_events (skill_id, activity, difficulty, minutes, xp, created_at) VALUES (1, 'a', 1, 10, 2, ?)`,
		practised); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("opening an old database should migrate it: %v", err)
	}
	defer store.Close()

	skills, err := store.ListSkills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Skill{}
	for _, s := range skills {
		byName[s.Name] = s
	}
	if !byName["Go"].LastPracticed.Equal(practised) {
		t.Errorf("Go last practiced = %v, want backfilled %v", byName["Go"].LastPracticed, practised)
	}
	if !byName["Chess"].LastPracticed.IsZero() {
		t.Errorf("a skill with no XP should stay never-practised, got %v", byName["Chess"].LastPracticed)
	}
	if !byName["Go"].IsActive {
		t.Error("old skills should default to the active stance")
	}
}

func TestCreateSkillTreeGarrisoned(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	sk, err := store.CreateSkillTree(ctx, NewSkillTree{Name: "Piano", Category: "Craft", StartLevel: 1, Garrisoned: true})
	if err != nil {
		t.Fatal(err)
	}
	if sk.IsActive {
		t.Error("returned skill should be garrisoned")
	}
	skills, _ := store.ListSkills(ctx)
	if skills[0].IsActive {
		t.Error("stored skill should be garrisoned (is_active 0)")
	}
}
