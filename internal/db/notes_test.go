package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
)

func TestNotesRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.CreateNote(ctx, "   "); err == nil {
		t.Error("an empty note should be refused")
	}
	first, err := store.CreateNote(ctx, "Ran 5k")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateNote(ctx, "Read chapter 3")
	if err != nil {
		t.Fatal(err)
	}

	notes, err := store.ListNotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].ID != second.ID || notes[1].ID != first.ID {
		t.Fatalf("notes = %+v, want newest first", notes)
	}
	if time.Since(notes[0].CreatedAt) > time.Minute {
		t.Errorf("created_at = %v, want about now", notes[0].CreatedAt)
	}

	edited, err := store.UpdateNote(ctx, first.ID, "Ran 6k")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Body != "Ran 6k" || !edited.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("edited = %+v, want new body and the original date", edited)
	}
	if edited.UpdatedAt.Before(first.UpdatedAt) {
		t.Errorf("updated_at went back: %v before %v", edited.UpdatedAt, first.UpdatedAt)
	}

	if err := store.DeleteNote(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteNote(ctx, second.ID); err == nil {
		t.Error("deleting a missing note should fail")
	}
	if _, err := store.UpdateNote(ctx, second.ID, "gone"); err == nil {
		t.Error("editing a missing note should fail")
	}
	notes, _ = store.ListNotes(ctx)
	if len(notes) != 1 || notes[0].ID != first.ID {
		t.Errorf("notes = %+v, want only the first", notes)
	}
}

func TestNoteAnalysisShieldsTodayAndIsClearedByAnEdit(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	n, err := store.CreateNote(ctx, "I was so sick today")
	if err != nil {
		t.Fatal(err)
	}
	n, err = store.SetNoteAnalysis(ctx, n.ID, rpg.NoteAnalysis{Status: rpg.StatusSick, Shield: true})
	if err != nil {
		t.Fatal(err)
	}
	if n.Analysis.Status != rpg.StatusSick || !n.Analysis.Shield {
		t.Fatalf("analysis not saved: %+v", n.Analysis)
	}

	days, err := store.ShieldedDays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if days[Today()] != rpg.StatusSick {
		t.Errorf("shielded days = %v, want today as Sick", days)
	}
	st, err := store.ChallengeStats(ctx, Today())
	if err != nil {
		t.Fatal(err)
	}
	if !st.TodayShielded {
		t.Error("stats should say today is shielded")
	}

	n, err = store.UpdateNote(ctx, n.ID, "Actually I felt fine")
	if err != nil {
		t.Fatal(err)
	}
	if n.Analysis.Status != "" || n.Analysis.Shield {
		t.Errorf("an edit should clear the old analysis, got %+v", n.Analysis)
	}
	if days, _ := store.ShieldedDays(ctx); len(days) != 0 {
		t.Errorf("shielded days = %v, want none after the edit", days)
	}
}

func TestClaimNoteXPOnce(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)

	n, err := store.CreateNote(ctx, "Wrote a worker pool in Go\nfor an hour")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNoteXP(ctx, n.ID); !errors.Is(err, ErrNothingToClaim) {
		t.Errorf("claim before analysis: err = %v, want ErrNothingToClaim", err)
	}

	a := rpg.NoteAnalysis{Status: rpg.StatusNormal, Skill: "Go", Minutes: 60}
	if _, err := store.SetNoteAnalysis(ctx, n.ID, a); err != nil {
		t.Fatal(err)
	}
	ev, err := store.ClaimNoteXP(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.XP != a.ClaimXP() || ev.SkillName != "Go" || ev.Activity != "📝 Wrote a worker pool in Go" {
		t.Errorf("event = %+v", ev)
	}
	if _, err := store.ClaimNoteXP(ctx, n.ID); !errors.Is(err, ErrNoteClaimed) {
		t.Errorf("second claim: err = %v, want ErrNoteClaimed", err)
	}

	notes, _ := store.ListNotes(ctx)
	if notes[0].ClaimedXP != ev.XP {
		t.Errorf("ClaimedXP = %d, want %d", notes[0].ClaimedXP, ev.XP)
	}
	// Editing the note keeps the claim, so it cannot be claimed again.
	if _, err := store.UpdateNote(ctx, n.ID, "Wrote a worker pool"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetNoteAnalysis(ctx, n.ID, a); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNoteXP(ctx, n.ID); !errors.Is(err, ErrNoteClaimed) {
		t.Errorf("claim after edit: err = %v, want ErrNoteClaimed", err)
	}

	// A claim never creates a skill.
	other, _ := store.CreateNote(ctx, "Juggled")
	if _, err := store.SetNoteAnalysis(ctx, other.ID, rpg.NoteAnalysis{Status: rpg.StatusNormal, Skill: "Juggling", Minutes: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimNoteXP(ctx, other.ID); err == nil {
		t.Error("claiming on a missing skill should fail")
	}
}

func TestDayLogsCountDirectivesAndXP(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	seedGoTree(t, store)

	today := Today()
	if err := store.SaveChallenges(ctx, today, []NewChallenge{
		{SkillName: "Go", Title: "Write a loop", Difficulty: 1, Minutes: 15, XP: 3},
		{SkillName: "Go", Title: "Read a post", Difficulty: 2, Minutes: 20, XP: 8},
	}); err != nil {
		t.Fatal(err)
	}
	list, _ := store.ListChallenges(ctx, today)
	if _, err := store.CompleteChallenge(ctx, list[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordXP(ctx, XPAward{SkillName: "Running", Category: "Body", Activity: "run", Difficulty: 2, Minutes: 30, XP: 10}); err != nil {
		t.Fatal(err)
	}

	logs, err := store.DayLogs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	l := logs[today]
	if l.DirectivesDone != 1 || l.DirectivesTotal != 2 {
		t.Errorf("directives %d/%d, want 1/2", l.DirectivesDone, l.DirectivesTotal)
	}
	if l.XP != 13 || len(l.Skills) != 2 || l.Skills[0] != "Go" || l.Skills[1] != "Running" {
		t.Errorf("log = %+v, want 13 XP on Go and Running", l)
	}
}

func TestStoreUsesWAL(t *testing.T) {
	store := newTestStore(t)
	var mode string
	var timeout int
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" || timeout != 5000 {
		t.Errorf("journal_mode %q busy_timeout %d, want wal and 5000", mode, timeout)
	}
}
