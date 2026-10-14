package ui

import (
	"errors"
	"fmt"
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

	// Down skips the locked level: experience → goal → back to name.
	m = send(t, m, key("down"))
	if m.onboard.field != fieldGoal {
		t.Fatalf("after experience comes goal, got %v", m.onboard.field)
	}
	m = send(t, m, key("down"))
	if m.onboard.field != fieldName {
		t.Errorf("down should skip the locked level and wrap to the name, got %v", m.onboard.field)
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
	for i, p := range m.onboard.pending {
		if p.Frozen != (i >= maxActiveFronts) {
			t.Errorf("skill %d frozen = %v", i, p.Frozen)
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

func TestCtrlRSwitchesBetweenFormAndRoster(t *testing.T) {
	m := setupModel(t, 160, 44)
	m = send(t, m, key("tab"))
	if m.onboard.field != fieldExperience {
		t.Fatalf("tab should move to the next form field, got %v", m.onboard.field)
	}
	m = send(t, m, key("shift+tab"))
	if m.onboard.field != fieldName {
		t.Fatalf("shift+tab should move back, got %v", m.onboard.field)
	}

	m = tabTo(t, m, fieldGoal)
	m = send(t, m, key("ctrl+r"))
	if m.onboard.field != fieldList {
		t.Fatalf("ctrl+r should focus the roster, got %v", m.onboard.field)
	}
	if !strings.Contains(m.renderKeys(), "toggle active/frozen") {
		t.Error("the footer should list f while the roster has focus")
	}
	m = send(t, m, key("tab"))
	if m.onboard.field != fieldList {
		t.Errorf("tab must not leave the roster, got %v", m.onboard.field)
	}
	m = send(t, m, key("ctrl+r"))
	if m.onboard.field != fieldGoal {
		t.Errorf("ctrl+r should go back to the field it left, got %v", m.onboard.field)
	}
	if !strings.Contains(m.renderKeys(), "ctrl+r") || strings.Contains(m.renderKeys(), "tab focus") {
		t.Error("the footer should say ctrl+r focuses the roster")
	}
}

func TestToggleSwapsActiveAndFrozen(t *testing.T) {
	m := setupModel(t, 160, 50)
	for _, name := range []string{"Body", "Java", "Chess", "Piano", "Golang"} {
		m = typeText(t, m, name)
		m = send(t, m, key("enter"))
	}
	m = tabTo(t, m, fieldList)
	for range 5 {
		m = send(t, m, key("down")) // Golang, last
	}
	m = send(t, m, key("f"))

	var order []string
	for _, p := range m.onboard.pending {
		order = append(order, fmt.Sprintf("%s:%v", p.Name, p.Frozen))
	}
	want := "Body:false Java:false Golang:false Chess:true Piano:true"
	if got := strings.Join(order, " "); got != want {
		t.Errorf("after promoting Golang: %s, want %s", got, want)
	}
	if m.onboard.pending[m.onboard.cursor].Name != "Golang" {
		t.Error("the cursor should follow the toggled skill")
	}
	if !strings.Contains(m.onboard.note, "Golang is now active; Chess moved to the garrison") {
		t.Errorf("note = %q", m.onboard.note)
	}

	// Freezing an active skill frees a front; a new skill takes it.
	m = send(t, m, key("up"))
	m = send(t, m, key(" ")) // Java
	if activeCount(m.onboard.pending) != 2 || !m.onboard.pending[2].Frozen || m.onboard.pending[2].Name != "Java" {
		t.Fatalf("Java should lead the garrison: %+v", m.onboard.pending)
	}
	m = send(t, m, key("ctrl+r"))
	m = typeText(t, m, "Yoga")
	m = send(t, m, key("enter"))
	if p := m.onboard.pending[2]; p.Name != "Yoga" || p.Frozen {
		t.Errorf("a new skill should take the free front, got %+v", p)
	}
}

func TestNamesAreCapitalizedOnAdd(t *testing.T) {
	m := setupModel(t, 160, 44)
	m = typeText(t, m, "public speaking")
	m = send(t, m, key("enter"))
	m = typeText(t, m, "SQL")
	m = send(t, m, key("enter"))
	if m.onboard.pending[0].Name != "Public Speaking" || m.onboard.pending[1].Name != "SQL" {
		t.Errorf("names = %q, %q", m.onboard.pending[0].Name, m.onboard.pending[1].Name)
	}
}

func TestManualSkillGetsACategory(t *testing.T) {
	m := setupModel(t, 160, 44)
	m = typeText(t, m, "Cooking")
	m = send(t, m, key("enter"))
	p := m.onboard.pending[0]
	if !p.Classifying || p.Category != "" {
		t.Fatalf("a skill without experience should be sent to the classifier, got %+v", p)
	}
	// No model in tests, so the command falls back to the keyword guess.
	msg := categorizeSkillCmd(nil, "Cooking")()
	m = send(t, m, msg)
	if p := m.onboard.pending[0]; p.Classifying || p.Category != "Craft" {
		t.Errorf("after the classifier: %+v", p)
	}
	if strings.Contains(m.View(), " auto ") {
		t.Error("the card should show the category, not auto")
	}
}

func TestAuditNoteClearsWhenTheNextSkillStarts(t *testing.T) {
	m := setupModel(t, 160, 44)
	m = typeText(t, m, "Golang")
	m = tabTo(t, m, fieldExperience)
	m = typeText(t, m, "four years at work")
	m = send(t, m, key("enter"))
	if !strings.Contains(m.onboard.note, "auditor is reading") {
		t.Fatalf("note = %q", m.onboard.note)
	}
	m = send(t, m, auditDoneMsg{name: "Golang", audit: rpg.Audit{StartLevel: 4, Category: "Craft"}})
	if m.onboard.note != onboardHint {
		t.Errorf("the card shows the audit; the form should go back to the hint, got %q", m.onboard.note)
	}

	m.onboard.note = "Added Golang. Add another, or ctrl+s to build."
	m = typeText(t, m, "P")
	if m.onboard.note != onboardHint {
		t.Errorf("typing a new skill should bring back the hint, got %q", m.onboard.note)
	}
}

func TestBuildLocksTheFormAndShowsProgressOnce(t *testing.T) {
	m := setupModel(t, 160, 44)
	for _, name := range []string{"Go", "Python"} {
		m = typeText(t, m, name)
		m = send(t, m, key("enter"))
	}
	m = send(t, m, key("ctrl+s"))
	if !m.onboard.building || m.onboard.name.Focused() {
		t.Fatalf("ctrl+s should lock the form, building=%v focused=%v", m.onboard.building, m.onboard.name.Focused())
	}
	m = typeText(t, m, "x")
	if m.onboard.name.Value() != "" {
		t.Error("typing must not edit the form during the build")
	}
	if n := strings.Count(m.View(), "Designing skill trees (1/2: Go)"); n != 1 {
		t.Errorf("progress should show once, got %d", n)
	}

	m = send(t, m, treeBuiltMsg{index: 0})
	if n := strings.Count(m.View(), "Designing skill trees (2/2: Python)"); n != 1 {
		t.Errorf("progress should move on to Python, got %d", n)
	}

	// A failure keeps the unsaved skills for a retry.
	m = send(t, m, treeBuiltMsg{index: 1, err: errors.New("model timed out")})
	if m.onboard.building || len(m.onboard.pending) != 1 || m.onboard.pending[0].Name != "Python" {
		t.Errorf("after the error: building=%v pending=%+v", m.onboard.building, m.onboard.pending)
	}
	if !m.onboard.name.Focused() {
		t.Error("the form should unlock after an error")
	}
}
