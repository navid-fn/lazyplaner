package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

func setupModel(t *testing.T, w, h int) model {
	t.Helper()
	m := newFlowModel(t)
	m.width, m.height = w, h
	m.updateSizes()
	return send(t, m, roadLoadedMsg{}) // no skills yet, so setup opens
}

func TestLevelLocksWhileExperienceHasText(t *testing.T) {
	m := setupModel(t, 160, 44)
	if !strings.Contains(m.View(), "Starting level") {
		t.Fatal("with no experience the manual level selector should show")
	}

	m = tabTo(t, m, fieldExperience)
	m = typeText(t, m, "years of it")
	view := m.View()
	if !strings.Contains(view, "AI Auto-Calibration Active") {
		t.Error("experience text should replace the selector with the locked badge")
	}
	if strings.Contains(view, "Starting level") {
		t.Error("the manual selector should be hidden while the level is locked")
	}

	// Tab skips the locked level: experience → goal → list.
	m = send(t, m, key("tab"))
	if m.onboard.field != fieldGoal {
		t.Fatalf("after experience comes goal, got %v", m.onboard.field)
	}
	m = send(t, m, key("tab"))
	if m.onboard.field != fieldList {
		t.Errorf("tab should skip the locked level, got %v", m.onboard.field)
	}

	// Clearing the experience brings the selector back.
	m = tabTo(t, m, fieldExperience)
	for range len("years of it") {
		m = send(t, m, key("backspace"))
	}
	if !strings.Contains(m.View(), "Starting level") {
		t.Error("an empty experience should bring back the manual selector")
	}
}

func TestGoalIsKeptAndShownOnTheCard(t *testing.T) {
	m := setupModel(t, 160, 44)
	m = typeText(t, m, "Java")
	m = tabTo(t, m, fieldGoal)
	m = typeText(t, m, "Build an Android app")
	m = send(t, m, key("enter"))

	p := m.onboard.pending[0]
	if p.Goal != "Build an Android app" || p.Auditing {
		t.Errorf("pending = %+v, want the goal and no audit (no experience)", p)
	}
	if m.onboard.goal.Value() != "" {
		t.Error("the goal field should clear after adding")
	}
	if !strings.Contains(m.View(), "Build an Android app") {
		t.Error("the card should show the goal")
	}
}

func TestFirstThreeAreActiveFrontsTheRestGarrisoned(t *testing.T) {
	m := setupModel(t, 160, 50)
	for _, name := range []string{"Go", "Java", "Chess", "Piano", "Yoga"} {
		m = typeText(t, m, name)
		m = send(t, m, key("enter"))
	}
	for i := range m.onboard.pending {
		if garrisoned(i) != (i >= maxActiveFronts) {
			t.Errorf("skill %d garrisoned = %v", i, garrisoned(i))
		}
	}

	view := m.View()
	front := strings.Index(view, "Active Fronts")
	back := strings.Index(view, "Garrisoned / Backlog")
	if front < 0 || back < 0 || front > back {
		t.Fatalf("both sections should show, fronts first (fronts at %d, backlog at %d)", front, back)
	}
	for _, name := range []string{"Go", "Java", "Chess"} {
		if i := strings.Index(view, "─ "+name+" "); i < 0 || i > back {
			t.Errorf("%s should be under Active Fronts", name)
		}
	}
	for _, name := range []string{"Piano", "Yoga"} {
		if i := strings.Index(view, "─ "+name+" "); i < back {
			t.Errorf("%s should be under Garrisoned / Backlog", name)
		}
	}
	if strings.Count(view, "FROZEN") != 2 {
		t.Errorf("the two backlog skills should be marked FROZEN, got %d", strings.Count(view, "FROZEN"))
	}
}

func TestIdeasWrapInsteadOfOverflowing(t *testing.T) {
	for _, w := range []int{100, 140, 186} {
		m := setupModel(t, w, 44)
		view := m.View()
		for _, idea := range setupIdeas {
			if !strings.Contains(view, idea) {
				t.Errorf("width %d: idea %q is missing or cut", w, idea)
			}
		}
		for i, line := range strings.Split(view, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: line %d is %d wide", w, i, lw)
			}
		}
	}
}

func TestWrapPills(t *testing.T) {
	got := wrapPills([]string{"aaa", "bbb", "ccc"}, 7, " ")
	if got != "aaa bbb\nccc" {
		t.Errorf("wrapPills = %q", got)
	}
}

func TestSetupWithEightSkillsFits(t *testing.T) {
	m := setupModel(t, 186, 50)
	for i := range rpg.MaxStartSkills {
		m = typeText(t, m, "Skill"+string(rune('a'+i)))
		m = send(t, m, key("enter"))
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Errorf("view is %d lines, want %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}

func TestSpinnerKeepsTickingDuringAnAudit(t *testing.T) {
	m := setupModel(t, 160, 44)
	m = typeText(t, m, "Go")
	m = tabTo(t, m, fieldExperience)
	m = typeText(t, m, "four years at work")
	m = send(t, m, key("enter"))

	_, cmd := m.Update(spinner.TickMsg{ID: m.spinner.ID()})
	if cmd == nil {
		t.Error("while an audit runs, the spinner should keep animating")
	}
}
