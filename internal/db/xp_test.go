package db

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestRecordXPCreatesAndAccumulatesSkills(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.RecordXP(ctx, XPAward{Activity: "Learn generics", SkillName: "Go", Category: "Craft", Difficulty: 3, Minutes: 120, XP: 36}); err != nil {
		t.Fatal(err)
	}
	// Same skill in a different case should land on the existing row.
	ev, err := store.RecordXP(ctx, XPAward{Activity: "Build CLI", SkillName: "go", Category: "Craft", Difficulty: 2, Minutes: 60, XP: 15})
	if err != nil {
		t.Fatal(err)
	}
	if ev.SkillName != "Go" {
		t.Errorf("event skill = %q, want existing spelling %q", ev.SkillName, "Go")
	}

	if _, err := store.RecordXP(ctx, XPAward{Activity: "Run", SkillName: "Running", Category: "Body", Difficulty: 3, Minutes: 35, XP: 11}); err != nil {
		t.Fatal(err)
	}

	skills, err := store.ListSkills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %+v", len(skills), skills)
	}
	if skills[0].Name != "Go" || skills[0].TotalXP != 51 {
		t.Errorf("top skill = %+v, want Go with 51 XP", skills[0])
	}
	if skills[1].Name != "Running" || skills[1].TotalXP != 11 {
		t.Errorf("second skill = %+v, want Running with 11 XP", skills[1])
	}
}

func TestListXPEventsNewestFirst(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	for _, activity := range []string{"first", "second", "third"} {
		if _, err := store.RecordXP(ctx, XPAward{Activity: activity, SkillName: "Go", Category: "Craft", Difficulty: 1, Minutes: 10, XP: 2}); err != nil {
			t.Fatal(err)
		}
	}

	events, err := store.ListXPEvents(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Activity != "third" || events[1].Activity != "second" {
		t.Errorf("unexpected order: %q, %q", events[0].Activity, events[1].Activity)
	}
}

func TestRecordXPRejectsInvalidInput(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.RecordXP(ctx, XPAward{SkillName: "", XP: 5}); err == nil {
		t.Error("expected error for empty skill name")
	}
	if _, err := store.RecordXP(ctx, XPAward{SkillName: "Go", XP: 0}); err == nil {
		t.Error("expected error for zero xp")
	}
}
