package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
)

// homeModel is the home screen with two skills, two directives (one done),
// one plan with an XP award, and some history.
func homeModel(t *testing.T, w, h int) model {
	t.Helper()
	m := newFlowModel(t)
	m.width, m.height = w, h
	m.updateSizes()
	m.skills = []db.Skill{
		{ID: 1, Name: "Go", Category: "Craft", TotalXP: 520, IsActive: true},
		{ID: 2, Name: "Piano", Category: "Craft", TotalXP: 100, IsActive: false},
	}
	m.nodes = []db.Node{{ID: 1, SkillID: 1, SkillName: "Go", Name: "Errors", RequiredLevel: 4, XPNeeded: 160, IsRusty: true}}
	m.challenges = []db.Challenge{
		{ID: 1, SkillName: "Go", Category: "Craft", NodeName: "Errors", Title: "Wrap an error",
			Description: "Write a function that wraps an error with %w and unwrap it in a test.", Difficulty: 3, Minutes: 20, XP: 8},
		{ID: 2, SkillName: "Go", Category: "Craft", Title: "Read the errors package", Difficulty: 1, Minutes: 10, XP: 2, Done: true},
	}
	m.challengeStats = db.ChallengeStats{Done: 1, Total: 2, Score: 40, Streak: 4}
	m.xpEvents = []db.XPEvent{
		{SkillName: "Go", Category: "Craft", PlanID: 7, Activity: "Learn generics", XP: 72, CreatedAt: time.Now()},
		{SkillName: "Go", Category: "Craft", Activity: "Challenge: Read the errors package", XP: 2, CreatedAt: time.Now()},
	}
	m.setPlans([]db.Plan{{ID: 7, Subject: "Learn generics", CreatedAt: time.Now()}})
	return m
}

func TestHUDShowsCharacterAndToday(t *testing.T) {
	m := homeModel(t, 186, 50)
	hud := m.renderHUD()
	for _, want := range []string{"LV ", "Active: 1", "Today: 1/2", "Streak: 4d", "Score: 40", "Rusty: 1"} {
		if !strings.Contains(hud, want) {
			t.Errorf("HUD should show %q: %q", want, hud)
		}
	}
	if strings.Contains(hud, "\n") {
		t.Error("the HUD is a single line")
	}
}

func TestDirectivesShowPromptWhenEmpty(t *testing.T) {
	m := homeModel(t, 160, 40)
	m.challenges = nil
	if !strings.Contains(m.View(), "to generate today's 3 directives") {
		t.Error("with no challenges, the directives panel should say how to get them")
	}
}

func TestTabSwitchesFocusAndKeysFollowIt(t *testing.T) {
	m := homeModel(t, 160, 40)
	if m.mainFocus != focusDirectives {
		t.Fatal("home starts on the directives")
	}

	m = send(t, m, key("j"))
	if m.challengeCursor != 1 {
		t.Errorf("j should move down the directives, cursor = %d", m.challengeCursor)
	}
	m = send(t, m, key("k"))
	if m.challengeCursor != 0 {
		t.Errorf("k should move back up, cursor = %d", m.challengeCursor)
	}

	// Plan actions do nothing while the directives have focus.
	m = send(t, m, key("d"))
	if m.confirmDelete != nil {
		t.Error("d on the directives must not start deleting a plan")
	}

	m = send(t, m, key("tab"))
	if m.mainFocus != focusPlans {
		t.Fatal("tab should move focus to the plans")
	}
	m = send(t, m, key("d"))
	if m.confirmDelete == nil {
		t.Error("d on the plans should ask to delete the selected plan")
	}
}

func TestEnterCompletesDirectiveFromHome(t *testing.T) {
	m := homeModel(t, 160, 40)
	m = send(t, m, key("enter"))
	if !m.isLoading {
		t.Fatal("enter on an open directive should start completing it")
	}

	m = homeModel(t, 160, 40)
	m.challengeCursor = 1 // the done one
	m = send(t, m, key("enter"))
	if m.isLoading || m.statusText != "Already done." {
		t.Errorf("a done directive should not complete again, status %q", m.statusText)
	}
}

func TestPlanRowsShowXPBadge(t *testing.T) {
	m := homeModel(t, 160, 40)
	items := m.planItems()
	if len(items) != 1 {
		t.Fatalf("got %d plan rows", len(items))
	}
	if title := items[0].(planItem).Title(); title != "Learn generics  [Go +72xp]" {
		t.Errorf("plan row title = %q", title)
	}
}

func TestBriefingOrPlanOnTheRight(t *testing.T) {
	m := homeModel(t, 186, 50)
	view := m.View()
	for _, want := range []string{"Strategic Briefing", "Active Skills", "Selected", "Recent XP",
		"Wrap an error", "★★★☆☆", "+8 XP", "Rusty node", "unwrap it in a test"} {
		if !strings.Contains(view, want) {
			t.Errorf("briefing should show %q", want)
		}
	}
	if strings.Contains(view, "Piano") && strings.Index(view, "Piano") < strings.Index(view, "Selected") {
		t.Error("a garrisoned skill should not be listed among active skills")
	}

	// With the plans focused and a plan selected, the plan replaces it.
	m = send(t, m, key("tab"))
	view = m.View()
	if strings.Contains(view, "Strategic Briefing") || !strings.Contains(view, "📜 Plan") {
		t.Error("with a plan selected, the right panel should show the plan")
	}

	// With the plans focused but none written, the briefing stays.
	m.setPlans(nil)
	if !strings.Contains(m.View(), "Strategic Briefing") {
		t.Error("with no plans, the briefing should show even when plans have focus")
	}
}

func TestHomeFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{186, 50}, {120, 36}, {80, 24}} {
		m := homeModel(t, size[0], size[1])
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Errorf("%dx%d: view is %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > m.width {
				t.Errorf("%dx%d: line %d is %d wide", size[0], size[1], i, w)
			}
		}
	}
}

func TestBriefingShowsNextMoves(t *testing.T) {
	m := homeModel(t, 186, 50)
	view := m.View()
	if !strings.Contains(view, "Next Moves") {
		t.Fatal("the briefing should have a Next Moves section")
	}
	// Go has a rusty node, so its next move is clearing the rust.
	if !strings.Contains(view, "⚠ rusty ▸ Errors") {
		t.Error("a rusty skill's next move should be its rusty node")
	}
}

func TestLongBusyTextDoesNotWidenTheDirectivesPanel(t *testing.T) {
	m := homeModel(t, 120, 32)
	leftW, _ := m.columnWidths()
	m.dailyBusy = "Rerolling a directive with a very long title that keeps on going…"
	panelTop := strings.Split(m.renderDirectives(leftW, 8), "\n")[0]
	if w := lipgloss.Width(panelTop); w != leftW {
		t.Errorf("panel top is %d wide, want %d", w, leftW)
	}
}

func TestKeyBarFitsAndKeepsTheEssentials(t *testing.T) {
	for _, w := range []int{70, 100, 190} {
		m := homeModel(t, w, 30)
		m.selectedModel = "llama3.2:latest"
		lines := strings.Split(m.renderBottomBar(), "\n")
		bar := lines[len(lines)-1]
		if got := lipgloss.Width(bar); got > w {
			t.Errorf("width %d: key bar is %d wide", w, got)
		}
		for _, want := range []string{"enter", "reroll", "help", "quit", "llama3.2"} {
			if !strings.Contains(bar, want) {
				t.Errorf("width %d: key bar is missing %q: %q", w, want, bar)
			}
		}
		if strings.Contains(bar, ":latest") {
			t.Errorf("width %d: the default tag should be dropped", w)
		}
	}
}
