package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
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
	if m.isLoading || m.proof != nil {
		t.Fatal("the first enter only starts the directive")
	}
	m.challenges[0].StartedAt = time.Now().Add(-time.Minute)
	if !strings.Contains(m.View(), "[▶]") {
		t.Error("a started directive shows [▶] in its row")
	}
	m = send(t, m, key("enter"))
	if m.proof == nil || !strings.Contains(m.View(), "Completed in <2m") {
		t.Fatal("finishing within 2 minutes should warn in the proof popup")
	}
	m = typeText(t, m, "short")
	m = send(t, m, key("enter"))
	if m.isLoading {
		t.Error("a quick finish needs a note of at least 10 characters")
	}
	m = typeText(t, m, " but real")
	m = send(t, m, key("enter"))
	if !m.isLoading {
		t.Fatal("a long enough note locks in the XP")
	}
	m = send(t, m, key("esc"))

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

func TestBriefingShowsTheExecutionGuide(t *testing.T) {
	m := homeModel(t, 186, 50)
	m.challenges[0].Details = "- Write `wrap(err)` with **%w**\n- Run go test ./...\n- Done when errors.Is passes"
	view := m.View()
	if strings.Contains(view, "Next Moves") {
		t.Error("Next Moves is replaced by the execution guide")
	}
	for _, want := range []string{"Execution Guide", "• Write wrap(err) with %w", "• Done when errors.Is passes"} {
		if !strings.Contains(view, want) {
			t.Errorf("the briefing should show %q", want)
		}
	}
}

func TestDirectivesListScrollsWithTheCursor(t *testing.T) {
	m := homeModel(t, 120, 36)
	m.challenges = nil
	for i := range 12 {
		m.challenges = append(m.challenges, db.Challenge{ID: int64(i + 1), Tier: rpg.TierFocus,
			SkillName: "Go", Category: "Craft", Title: fmt.Sprintf("Drill Number %02d", i+1), XP: 5})
	}
	view := m.View()
	if !strings.Contains(view, "Drill Number 01") || strings.Contains(view, "Drill Number 12") {
		t.Fatal("the list should start at the top")
	}
	if !strings.Contains(view, "↓ more (1/12)") {
		t.Error("hidden rows should be announced")
	}
	for range 11 {
		m = send(t, m, key("down"))
	}
	view = m.View()
	if !strings.Contains(view, "Drill Number 12") || strings.Contains(view, "Drill Number 01") {
		t.Error("the list should scroll to keep the cursor in view")
	}
	if !strings.Contains(view, "↑ more (12/12)") {
		t.Error("the indicator should say where the cursor is")
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

func TestEnterOnAPlanOpensTheReader(t *testing.T) {
	m := homeModel(t, 160, 40)
	m.setPlans([]db.Plan{{ID: 7, Subject: "Learn generics", CreatedAt: time.Now(),
		Description: "# Generics Plan\n\n## Steps\n\n- Read the **spec**\n- Write `Map[K, V]`\n\n| Week | Topic |\n| --- | --- |\n| 1 | Constraints |\n" +
			strings.Repeat("\nMore notes line.\n", 40)}})
	m = send(t, m, key("tab"))
	m = send(t, m, key("enter"))
	if m.screen != screenPlanView || m.isEditing {
		t.Fatalf("enter on a plan should open the reader, not the editor: screen=%v editing=%v", m.screen, m.isEditing)
	}
	view := m.View()
	for _, want := range []string{"Learn generics", "Generics Plan", "Read the spec", "Constraints", "esc back"} {
		if !strings.Contains(ansi.Strip(view), want) {
			t.Errorf("the reader should show %q", want)
		}
	}
	if strings.Contains(view, "**spec**") || strings.Contains(view, "| --- |") {
		t.Error("markdown should be rendered, not shown raw")
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d wide, wider than %d", i, w, m.width)
		}
	}

	// It scrolls, and esc goes back to the plan list.
	m = send(t, m, key("G"))
	if m.planView.vp.YOffset == 0 {
		t.Error("G should scroll to the end")
	}
	m = send(t, m, key("esc"))
	if m.screen != screenMain || m.mainFocus != focusPlans {
		t.Error("esc should return to the plan list")
	}
}
