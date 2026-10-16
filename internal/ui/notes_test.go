package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// sendRun feeds msg through Update, then runs the command it returns and
// feeds its notes and character results back too, as the program would.
// Other results, such as spinner ticks, are dropped.
func sendRun(t *testing.T, m model, msg any) model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(model)
	for _, out := range runCmd(cmd) {
		switch out.(type) {
		case noteSavedMsg, noteDeletedMsg, notesLoadedMsg, noteClaimedMsg, planFromNoteMsg, roadLoadedMsg, modelSavedMsg:
			m = sendRun(t, m, out)
		}
	}
	return m
}

// runCmd runs cmd and returns its messages, opening batches.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	out := cmd()
	if batch, ok := out.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, runCmd(c)...)
		}
		return msgs
	}
	return []tea.Msg{out}
}

func TestNotesAddEditDelete(t *testing.T) {
	m := newFlowModel(t)
	m.onboardChecked = true // no skills, but setup is not what this tests
	m = send(t, m, key("n"))
	if m.screen != screenNotes {
		t.Fatalf("n should open notes, screen = %v", m.screen)
	}

	// Write a note.
	m = send(t, m, key("a"))
	if m.screen != screenNoteEdit {
		t.Fatalf("a should open the editor, screen = %v", m.screen)
	}
	m = typeText(t, m, "Ran 5k")
	m = sendRun(t, m, key("ctrl+s"))
	if m.screen != screenNotes || len(m.notes.list) != 1 || m.notes.list[0].Body != "Ran 5k" {
		t.Fatalf("after save: screen %v notes %+v", m.screen, m.notes.list)
	}
	if !strings.Contains(m.View(), "Today") || !strings.Contains(m.View(), time.Now().Format("Monday, 2 January 2006")) {
		t.Error("the note should show under Today with its full date")
	}

	// A second note goes on top and gets the cursor.
	m = send(t, m, key("a"))
	m = typeText(t, m, "Read a book")
	m = sendRun(t, m, key("ctrl+s"))
	if len(m.notes.list) != 2 || m.notes.cursor != 0 || m.notes.list[0].Body != "Read a book" {
		t.Fatalf("second note: cursor %d notes %+v", m.notes.cursor, m.notes.list)
	}

	// Edit the older one.
	m = send(t, m, key("j"))
	m = send(t, m, key("e"))
	if got := m.notes.editor.Value(); got != "Ran 5k" {
		t.Fatalf("editor = %q, want the note's text", got)
	}
	m = typeText(t, m, " fast")
	m = sendRun(t, m, key("ctrl+s"))
	if m.notes.list[1].Body != "Ran 5k fast" || m.notes.cursor != 1 || m.statusText != "Note updated." {
		t.Fatalf("after edit: cursor %d status %q notes %+v", m.notes.cursor, m.statusText, m.notes.list)
	}

	// An empty note is refused and the editor stays open.
	m = send(t, m, key("a"))
	m = sendRun(t, m, key("ctrl+s"))
	if m.screen != screenNoteEdit || m.errorText == "" {
		t.Errorf("an empty note should be refused, screen %v error %q", m.screen, m.errorText)
	}
	m = send(t, m, key("esc"))

	// Delete asks first; any key but y cancels.
	m = send(t, m, key("d"))
	if !strings.Contains(m.View(), "Delete this note?") {
		t.Error("d should ask before deleting")
	}
	m = send(t, m, key("x"))
	if len(m.notes.list) != 2 {
		t.Fatal("x should cancel the delete")
	}
	m = send(t, m, key("d"))
	m = sendRun(t, m, key("y"))
	if len(m.notes.list) != 1 || m.notes.list[0].Body != "Read a book" || m.notes.cursor != 0 {
		t.Fatalf("after delete: cursor %d notes %+v", m.notes.cursor, m.notes.list)
	}

	// The notes are in the database, not just on screen.
	m = sendRun(t, m, loadNotesCmd(m.store, 0)())
	if len(m.notes.list) != 1 {
		t.Errorf("reloaded %d notes, want 1", len(m.notes.list))
	}

	m = send(t, m, key("n"))
	if m.screen != screenMain {
		t.Errorf("n should go back home, screen = %v", m.screen)
	}
}

func TestNotesScreenFitsTheTerminal(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 80, 24
	m.updateSizes()
	now := time.Now()
	for i := range 30 {
		at := now.Add(-time.Duration(i) * 7 * time.Hour)
		m.notes.list = append(m.notes.list, db.Note{ID: int64(i + 1), Body: strings.Repeat("long words ", 20), CreatedAt: at, UpdatedAt: at})
	}
	m.openNotes()
	m.notes.cursor = 25

	for _, s := range []screen{screenNotes, screenNoteEdit} {
		m.screen = s
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Errorf("screen %v: view is %d lines, want %d", s, len(lines), m.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > m.width {
				t.Errorf("screen %v: line %d is %d wide", s, i, w)
			}
		}
	}
	m.screen = screenNotes
	if !strings.Contains(m.renderNoteList(36, 20), "▸") {
		t.Error("the selected note should be scrolled into view")
	}
}

// fieldLogModel opens the field log over a store with a Go skill and one
// note per body, analyzed as given.
func fieldLogModel(t *testing.T, notes map[string]rpg.NoteAnalysis) model {
	t.Helper()
	m := newFlowModel(t)
	m.width, m.height = 120, 32
	m.updateSizes()
	ctx := context.Background()
	if _, err := m.store.RecordXP(ctx, db.XPAward{SkillName: "Go", Category: "Craft", Activity: "seed", Difficulty: 1, Minutes: 5, XP: 1}); err != nil {
		t.Fatal(err)
	}
	for body, a := range notes {
		n, err := m.store.CreateNote(ctx, body)
		if err != nil {
			t.Fatal(err)
		}
		if a.Status != "" {
			if _, err := m.store.SetNoteAnalysis(ctx, n.ID, a); err != nil {
				t.Fatal(err)
			}
		}
	}
	return sendRun(t, m, key("n"))
}

func TestFieldLogShowsShieldSignifierAndSnapshot(t *testing.T) {
	m := fieldLogModel(t, map[string]rpg.NoteAnalysis{
		"I was so sick today, I could not do anything": {Status: rpg.StatusSick, Shield: true},
	})
	view := ansi.Strip(m.View())
	for _, want := range []string{"Field Log", "[🛡️ Sick]", "! ", "Illness detected", "streak is protected",
		"Strategist Analysis", "Day Snapshot", "XP earned", "+1", "Skills", "Go", "shielded (Sick)"} {
		if !strings.Contains(view, want) {
			t.Errorf("field log is missing %q", want)
		}
	}
	m = sendRun(t, m, loadRoadCmd(m.store)()) // the stats load at start-up and after each analysis
	if !strings.Contains(ansi.Strip(m.renderHUD()), "🛡️ Streak") {
		t.Error("the HUD should show today's streak as shielded")
	}
	for _, want := range []string{"x analyze", "p to plan", "j/k move", "n back"} {
		if !strings.Contains(ansi.Strip(m.renderKeys()), want) {
			t.Errorf("key bar is missing %q", want)
		}
	}
	if strings.Contains(m.renderKeys(), "claim") {
		t.Error("a sick note has nothing to claim")
	}
	lines := strings.Split(m.View(), "\n")
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("line %d is %d wide, wider than %d", i, w, m.width)
		}
	}
}

func TestFieldLogClaimsXPOnce(t *testing.T) {
	m := fieldLogModel(t, map[string]rpg.NoteAnalysis{
		"Wrote a worker pool in Go for an hour": {Status: rpg.StatusEnergized, Skill: "Go", Minutes: 60},
	})
	if !strings.Contains(ansi.Strip(m.View()), "★") || !strings.Contains(m.renderKeys(), "claim") {
		t.Fatal("a win should show ★ and offer the claim")
	}
	m = sendRun(t, m, key("c"))
	want := rpg.NoteAnalysis{Skill: "Go", Minutes: 60}.ClaimXP()
	if m.notes.list[0].ClaimedXP != want || !strings.Contains(m.statusText, "XP to Go") {
		t.Fatalf("after claim: ClaimedXP %d status %q", m.notes.list[0].ClaimedXP, m.statusText)
	}
	if m.skills[0].TotalXP != 1+want {
		t.Errorf("Go XP = %d, want %d", m.skills[0].TotalXP, 1+want)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Claimed +") {
		t.Error("the analysis should say the XP is claimed")
	}

	m = sendRun(t, m, key("enter"))
	if !strings.Contains(m.statusText, "Already claimed") || m.skills[0].TotalXP != 1+want {
		t.Errorf("a second claim should do nothing, status %q xp %d", m.statusText, m.skills[0].TotalXP)
	}
}

func TestFieldLogNoteToPlan(t *testing.T) {
	m := fieldLogModel(t, map[string]rpg.NoteAnalysis{
		"Spent an hour on Go but the CI is broken, need to fix it": {Status: rpg.StatusBlocked, FollowUp: "Fix the broken CI build"},
		"Nice walk": {},
	})
	// The notes share a creation second, so pick each one by its text.
	selectNote := func(prefix string) {
		for i, n := range m.notes.list {
			if strings.HasPrefix(n.Body, prefix) {
				m.notes.cursor = i
				return
			}
		}
		t.Fatalf("no note %q", prefix)
	}
	selectNote("Spent an hour")
	m = sendRun(t, m, key("p"))
	if len(m.plans) != 1 || m.plans[0].Subject != "Fix the broken CI build" {
		t.Fatalf("plans = %+v, want the follow-up as a plan", m.plans)
	}
	if !strings.Contains(m.plans[0].Description, "> Spent an hour on Go") {
		t.Errorf("the plan should quote the note, got %q", m.plans[0].Description)
	}

	// A note without a follow-up becomes a plan named after its first line.
	selectNote("Nice walk")
	m = sendRun(t, m, key("p"))
	if len(m.plans) != 2 || m.plans[0].Subject != "Nice walk" {
		t.Errorf("plans = %+v, want the note's first line as the subject", m.plans)
	}
}

func TestFieldLogAnalyzeWithoutModel(t *testing.T) {
	m := fieldLogModel(t, map[string]rpg.NoteAnalysis{"Nice walk": {}})
	if !strings.Contains(ansi.Strip(m.View()), "Not analyzed yet") {
		t.Error("an unread note should say so")
	}
	next, cmd := m.Update(key("x"))
	m = next.(model)
	if m.notes.analyzing == 0 {
		t.Fatal("x should start the analysis")
	}
	for _, out := range runCmd(cmd) {
		if msg, ok := out.(noteAnalyzedMsg); ok {
			m = send(t, m, msg)
		}
	}
	if m.notes.analyzing != 0 || !strings.Contains(m.errorText, "not configured") {
		t.Errorf("analyzing %d error %q, want the failure reported", m.notes.analyzing, m.errorText)
	}
}

func TestRecoveryDayFollowsAShieldedDay(t *testing.T) {
	m := fieldLogModel(t, map[string]rpg.NoteAnalysis{
		"Exhausted, burnt out": {Status: rpg.StatusExhausted, Shield: true},
	})
	ctx := context.Background()
	now := time.Now()
	for _, c := range []struct {
		day  time.Time
		want bool
	}{
		{now, true},                    // shielded before the set was made
		{now.AddDate(0, 0, 1), true},   // the day after
		{now.AddDate(0, 0, 2), false},  // back to normal
		{now.AddDate(0, 0, -1), false}, // the day before
	} {
		got, err := recoveryDay(ctx, m.store, c.day.Format("2006-01-02"))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("recoveryDay(%s) = %v, want %v", c.day.Format("2006-01-02"), got, c.want)
		}
	}
}
