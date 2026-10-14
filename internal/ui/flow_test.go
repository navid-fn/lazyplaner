package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
)

func newFlowModel(t *testing.T) model {
	t.Helper()
	store, err := db.NewStore(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	m := NewModel(store, nil)
	m.width, m.height = 100, 30
	m.updateSizes()
	return m
}

// key builds a key message from its name, or from the literal text when it
// is not a named key.
func key(s string) tea.KeyMsg {
	named := map[string]tea.KeyType{
		"enter":     tea.KeyEnter,
		"esc":       tea.KeyEsc,
		"tab":       tea.KeyTab,
		"left":      tea.KeyLeft,
		"right":     tea.KeyRight,
		"up":        tea.KeyUp,
		"down":      tea.KeyDown,
		"ctrl+s":    tea.KeyCtrlS,
		"ctrl+d":    tea.KeyCtrlD,
		"backspace": tea.KeyBackspace,
	}
	if t, ok := named[s]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send feeds one message through Update and keeps the returned model.
func send(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(model)
}

// tabTo presses tab until the setup form's focus is on field.
func tabTo(t *testing.T, m model, field onboardField) model {
	t.Helper()
	for range fieldCount {
		if m.onboard.field == field {
			return m
		}
		m = send(t, m, key("tab"))
	}
	t.Fatalf("could not tab to field %v", field)
	return m
}

func typeText(t *testing.T, m model, text string) model {
	t.Helper()
	for _, r := range text {
		m = send(t, m, key(string(r)))
	}
	return m
}

func TestFirstLaunchOpensSetupWhenNoSkills(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	if m.screen != screenOnboard {
		t.Fatalf("screen = %v, want onboarding", m.screen)
	}
	if !m.onboardChecked {
		t.Error("onboardChecked should be set after the first load")
	}
}

func TestSetupDoesNotReopenWhenSkillsExist(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{skills: []db.Skill{{Name: "Go"}}})
	if m.screen == screenOnboard {
		t.Fatal("setup should not open when a skill already exists")
	}
}

func TestSetupAddsSkillsAndRejectsDuplicates(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})

	m = typeText(t, m, "Go")
	m = send(t, m, key("enter"))
	if len(m.onboard.pending) != 1 || m.onboard.pending[0].Name != "Go" {
		t.Fatalf("pending = %+v, want one skill named Go", m.onboard.pending)
	}

	// The name field is cleared, so a duplicate needs typing again.
	m = typeText(t, m, "go")
	m = send(t, m, key("enter"))
	if len(m.onboard.pending) != 1 {
		t.Errorf("duplicate was added: %+v", m.onboard.pending)
	}
	if !strings.Contains(m.onboard.note, "already") || !m.onboard.noteError {
		t.Errorf("note = %q (error %v), want a duplicate message", m.onboard.note, m.onboard.noteError)
	}
}

func TestSetupLevelChangeWithArrows(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})

	m = tabTo(t, m, fieldLevel)
	for i := 0; i < 10; i++ {
		m = send(t, m, key("right"))
	}
	if m.onboard.level != 5 {
		t.Errorf("level = %d, want clamped to 5", m.onboard.level)
	}
}

func TestSetupBuildNeedsASkill(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = send(t, m, key("ctrl+s"))
	if m.isLoading {
		t.Error("building with no skills should not start")
	}
	if !m.onboard.noteError || !strings.Contains(m.onboard.note, "Add at least one") {
		t.Errorf("note = %q, want a message asking for a skill", m.onboard.note)
	}
}

func TestEscSkipsSetupAndDoesNotReopenInSameSession(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = send(t, m, key("esc"))
	if m.screen != screenMain {
		t.Fatalf("screen = %v after esc, want main", m.screen)
	}
	// A later road reload must not pull the person back into setup.
	m = send(t, m, roadLoadedMsg{})
	if m.screen == screenOnboard {
		t.Error("setup reopened on a later load")
	}
}

func TestTreeNavigationCyclesSkills(t *testing.T) {
	m := newFlowModel(t)
	m.skills = []db.Skill{{ID: 1, Name: "Go"}, {ID: 2, Name: "Running"}}
	m.openTree()
	if m.screen != screenTree {
		t.Fatalf("screen = %v, want tree", m.screen)
	}

	m = send(t, m, key("right"))
	if m.treeSkill != 1 {
		t.Errorf("treeSkill = %d, want 1", m.treeSkill)
	}
	m = send(t, m, key("right"))
	if m.treeSkill != 0 {
		t.Errorf("treeSkill = %d, want wrap to 0", m.treeSkill)
	}
	m = send(t, m, key("t"))
	if m.screen != screenMain {
		t.Errorf("t should return to the plan list, screen = %v", m.screen)
	}
}

func TestDailyCursorStaysInRange(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily
	m.challenges = []db.Challenge{{ID: 1, Title: "a"}, {ID: 2, Title: "b"}}

	for i := 0; i < 5; i++ {
		m = send(t, m, key("down"))
	}
	if m.challengeCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.challengeCursor)
	}
	for i := 0; i < 5; i++ {
		m = send(t, m, key("up"))
	}
	if m.challengeCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.challengeCursor)
	}
}

func TestDailyTickOffStartsCompletion(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily
	m.challenges = []db.Challenge{{ID: 1, Title: "Write a loop", XP: 3}}

	m = send(t, m, key("enter"))
	if !m.isLoading {
		t.Error("enter should start completing the selected challenge")
	}

	m = send(t, m, challengeDoneMsg{event: db.XPEvent{XP: 3, SkillName: "Go"}})
	if m.isLoading {
		t.Error("loading should stop once the completion result arrives")
	}
	if !strings.Contains(m.statusText, "+3 XP to Go") {
		t.Errorf("statusText = %q", m.statusText)
	}
}

func TestDailyDoesNotTickDoneChallenge(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily
	m.challenges = []db.Challenge{{ID: 1, Title: "Done already", Done: true}}

	m = send(t, m, key("enter"))
	if m.isLoading {
		t.Error("a done challenge should not start another completion")
	}
	if m.statusText != "Already done." {
		t.Errorf("statusText = %q", m.statusText)
	}
}

func TestSetupTypingWorksAfterChangingLevel(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})

	m = tabTo(t, m, fieldLevel)
	m = send(t, m, key("right"))
	m = typeText(t, m, "Drawing")
	if m.onboard.field != fieldName {
		t.Fatalf("typing should move focus to the name field, got %v", m.onboard.field)
	}
	m = send(t, m, key("enter"))

	if len(m.onboard.pending) != 1 {
		t.Fatalf("pending = %+v, want Drawing added", m.onboard.pending)
	}
	got := m.onboard.pending[0]
	if got.Name != "Drawing" || got.Level != 4 {
		t.Errorf("added %+v, want Drawing at level 4", got)
	}
}

func TestSetupEnterFromLevelAddsTypedName(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})

	m = typeText(t, m, "Chess")
	m = tabTo(t, m, fieldLevel)
	m = send(t, m, key("enter"))
	if len(m.onboard.pending) != 1 || m.onboard.pending[0].Name != "Chess" {
		t.Errorf("enter on another field should add the typed name, pending = %+v", m.onboard.pending)
	}
}

func TestSetupShowsWhyANameIsRejected(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})

	m = typeText(t, m, "I want to get fit")
	m = send(t, m, key("enter"))
	if len(m.onboard.pending) != 0 {
		t.Fatalf("invalid name was added: %+v", m.onboard.pending)
	}
	if !m.onboard.noteError || !strings.Contains(m.onboard.note, "3 words") {
		t.Errorf("note = %q, want the word-count explanation", m.onboard.note)
	}
	// The rejected text stays so it can be fixed.
	if m.onboard.name.Value() != "I want to get fit" {
		t.Errorf("field = %q, the rejected text should stay", m.onboard.name.Value())
	}

	// Editing clears the error and brings back the hint.
	m = send(t, m, key("backspace"))
	if m.onboard.noteError {
		t.Error("error should clear once the name is edited")
	}
}

func TestSetupLimitsStartSkills(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	for i := 0; i < 8; i++ {
		m = typeText(t, m, string(rune('a'+i))+"skill")
		m = send(t, m, key("enter"))
	}
	if len(m.onboard.pending) != 8 {
		t.Fatalf("pending = %d, want 8", len(m.onboard.pending))
	}
	m = typeText(t, m, "Ninth")
	m = send(t, m, key("enter"))
	if len(m.onboard.pending) != 8 || !m.onboard.noteError {
		t.Errorf("ninth skill should be refused with a message, pending %d note %q", len(m.onboard.pending), m.onboard.note)
	}
}

func TestSetupHelpOpensAndCloses(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})

	m = send(t, m, key("?"))
	if m.screen != screenHelp || m.prevScreen != screenOnboard {
		t.Fatalf("? should open help over setup, screen=%v prev=%v", m.screen, m.prevScreen)
	}
	if !strings.Contains(m.View(), "Not accepted") {
		t.Error("help popup should list what is not accepted")
	}
	// Esc closes help and returns to setup without skipping it.
	m = send(t, m, key("esc"))
	if m.screen != screenOnboard {
		t.Errorf("esc should return to setup, screen=%v", m.screen)
	}
}

func TestHelpPopupFromMainWithTabs(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{skills: []db.Skill{{Name: "Go"}}})
	m = send(t, m, key("h"))
	if m.screen != screenHelp || m.prevScreen != screenMain {
		t.Fatalf("h should open help over the plan list, screen=%v", m.screen)
	}
	if !strings.Contains(m.View(), "Skills") || !strings.Contains(m.View(), "Keys") {
		t.Error("help should show the tab row")
	}

	if m = send(t, m, key("right")); m.helpTab != 1 {
		t.Errorf("right: helpTab = %d, want 1", m.helpTab)
	}
	if m = send(t, m, key("left")); m.helpTab != 0 {
		t.Errorf("left: helpTab = %d, want 0", m.helpTab)
	}
	if m = send(t, m, key("left")); m.helpTab != len(helpTabs)-1 {
		t.Errorf("left from first tab should wrap, helpTab = %d", m.helpTab)
	}

	m = send(t, m, key("h"))
	if m.screen != screenMain {
		t.Errorf("h should close help, screen=%v", m.screen)
	}
}

func TestEachHelpTabHasContent(t *testing.T) {
	for _, name := range helpTabs {
		if out := helpSection(name, 60); strings.TrimSpace(out) == "" {
			t.Errorf("help tab %q has no content", name)
		}
	}
}

func TestSetupEditsAStartSkillFromTheList(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = typeText(t, m, "Go")
	m = send(t, m, key("enter"))
	m = typeText(t, m, "Running")
	m = send(t, m, key("enter"))

	// Tab to the list (name → level → list) and pick the first row.
	m = tabTo(t, m, fieldList)
	m = send(t, m, key("up"))
	m = send(t, m, key("up"))
	m = send(t, m, key("enter"))
	if m.onboard.editing != 0 || m.onboard.name.Value() != "Go" {
		t.Fatalf("enter on a row should load it for editing, editing=%d name=%q", m.onboard.editing, m.onboard.name.Value())
	}

	// Renaming the skill to its own name is allowed; a clash is not.
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = typeText(t, m, "Running")
	m = send(t, m, key("enter"))
	if !m.onboard.noteError || m.onboard.pending[0].Name != "Go" {
		t.Fatalf("renaming onto another skill should be refused, pending=%+v note=%q", m.onboard.pending, m.onboard.note)
	}

	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = typeText(t, m, "Golang")
	m = send(t, m, key("enter"))
	if m.onboard.pending[0].Name != "Golang" || m.onboard.editing != -1 {
		t.Errorf("save should replace the skill, pending=%+v editing=%d", m.onboard.pending, m.onboard.editing)
	}
	if len(m.onboard.pending) != 2 {
		t.Errorf("editing should not add a skill, pending=%d", len(m.onboard.pending))
	}
}

func TestSetupEscCancelsEditInsteadOfSkipping(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = typeText(t, m, "Go")
	m = send(t, m, key("enter"))
	m = tabTo(t, m, fieldList)
	m = send(t, m, key("enter"))
	m = send(t, m, key("esc"))
	if m.screen != screenOnboard || m.onboard.editing != -1 {
		t.Errorf("esc during edit should cancel the edit only, screen=%v editing=%d", m.screen, m.onboard.editing)
	}
}

func TestSetupRemovesSelectedSkill(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = typeText(t, m, "Go")
	m = send(t, m, key("enter"))
	m = typeText(t, m, "Running")
	m = send(t, m, key("enter"))

	m = tabTo(t, m, fieldList)
	m = send(t, m, key("up")) // select Go
	m = send(t, m, key("ctrl+d"))
	if len(m.onboard.pending) != 1 || m.onboard.pending[0].Name != "Running" {
		t.Errorf("ctrl+d on the list should remove the selected skill, pending=%+v", m.onboard.pending)
	}
}

func TestSkillEditResetsAndRenames(t *testing.T) {
	m := newFlowModel(t)
	m.skills = []db.Skill{{ID: 7, Name: "Go", Category: "Craft", TotalXP: 900}, {ID: 8, Name: "Running", Category: "Body"}}
	m.openTree()

	cmd := m.openSkillEdit()
	if cmd == nil || m.screen != screenSkillEdit {
		t.Fatalf("e should open the skill editor, screen=%v", m.screen)
	}
	if m.skillEdit.name.Value() != "Go" || m.skillEdit.skillID != 7 {
		t.Fatalf("editor loaded %+v", m.skillEdit)
	}

	// Renaming onto another skill is refused and the editor stays open.
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = typeText(t, m, "Running")
	m = send(t, m, key("enter"))
	if m.screen != screenSkillEdit || !m.skillEdit.noteError {
		t.Errorf("clash should keep the editor open with a message, screen=%v note=%q", m.screen, m.skillEdit.note)
	}

	// A valid name saves: the save is async, so feed its result back in.
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = send(t, m, key("backspace"))
	m = typeText(t, m, "Golang")
	m = send(t, m, key("enter"))
	if !m.isLoading {
		t.Fatal("a valid save should start the reset")
	}
	m = send(t, m, skillUpdatedMsg{name: "Golang"})
	if m.screen != screenTree || !strings.Contains(m.statusText, "level 1") {
		t.Errorf("after save: screen=%v status=%q", m.screen, m.statusText)
	}
}

func TestSkillEditShowsResetWarning(t *testing.T) {
	m := newFlowModel(t)
	m.skills = []db.Skill{{ID: 1, Name: "Go", Category: "Craft"}}
	m.openTree()
	m.openSkillEdit()
	if !strings.Contains(m.View(), "resets this skill to level 1") {
		t.Error("the edit popup should warn that saving resets the skill")
	}
}

func TestDailyNewSetOnlyWhenAllDone(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily

	// Still open: r is refused and nothing starts.
	m.challenges = []db.Challenge{{ID: 1, Title: "Open"}, {ID: 2, Title: "Done", Done: true}}
	m = send(t, m, key("r"))
	if m.isLoading || !strings.Contains(m.statusText, "Finish today's challenges") {
		t.Errorf("r with open challenges should be refused, loading=%v status=%q", m.isLoading, m.statusText)
	}

	// All done: r starts a new set.
	m.challenges = []db.Challenge{{ID: 1, Title: "Done", Done: true}}
	m = send(t, m, key("r"))
	if m.screen != screenDailyPick || !m.dailyPickRenew {
		t.Fatalf("r after all challenges are done should ask how many to make, screen=%v", m.screen)
	}
	m = send(t, m, key("enter"))
	if !m.isLoading {
		t.Error("confirming the picker should start the new set")
	}
}

func TestDailyRenewHintOnlyWhenAllDone(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily
	m.challenges = []db.Challenge{{ID: 1, Title: "Open"}}
	if strings.Contains(m.View(), "new set") {
		t.Error("the new-set key should not show while challenges are open")
	}
	m.challenges[0].Done = true
	if !strings.Contains(m.View(), "new set") {
		t.Error("the new-set key should show once everything is done")
	}
}

func TestDailyPickerSizeAndCancel(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily
	m.dailyDefault = 3
	m.openDailyPicker(false)
	if m.dailyPickSize != 3 {
		t.Fatalf("picker should start at the default 3, got %d", m.dailyPickSize)
	}

	for i := 0; i < 5; i++ {
		m = send(t, m, key("right"))
	}
	if m.dailyPickSize != 5 {
		t.Errorf("size should clamp at 5, got %d", m.dailyPickSize)
	}
	for i := 0; i < 9; i++ {
		m = send(t, m, key("left"))
	}
	if m.dailyPickSize != 1 {
		t.Errorf("size should clamp at 1, got %d", m.dailyPickSize)
	}

	m = send(t, m, key("esc"))
	if m.screen != screenDaily || m.isLoading {
		t.Errorf("esc should go back to the daily popup without generating, screen=%v loading=%v", m.screen, m.isLoading)
	}
}

func TestDailyOpensPickerWhenTodayIsEmpty(t *testing.T) {
	m := newFlowModel(t)
	m.screen = screenDaily
	m.dailyOpening = true
	m.isLoading = true
	m = send(t, m, challengesLoadedMsg{count: 4})
	if m.screen != screenDailyPick {
		t.Fatalf("opening on an empty day should ask how many, screen=%v", m.screen)
	}
	if m.dailyPickSize != 4 {
		t.Errorf("picker should default to the last choice 4, got %d", m.dailyPickSize)
	}
}

func TestTreeVimKeysMatchArrows(t *testing.T) {
	m := newFlowModel(t)
	m.skills = []db.Skill{{ID: 1, Name: "Go"}, {ID: 2, Name: "Running"}}
	m.nodes = []db.Node{
		{ID: 1, SkillID: 1, Name: "Basics", RequiredLevel: 1, XPNeeded: 40},
		{ID: 2, SkillID: 1, ParentID: 1, Name: "Loops", RequiredLevel: 1, XPNeeded: 40},
	}
	m.openTree()

	m = send(t, m, key("l"))
	if m.treeSkill != 1 {
		t.Errorf("l should move to the next skill, treeSkill = %d", m.treeSkill)
	}
	m = send(t, m, key("h"))
	if m.treeSkill != 0 {
		t.Errorf("h should move to the previous skill, treeSkill = %d", m.treeSkill)
	}

	m = send(t, m, key("j"))
	if m.treeNode != 1 {
		t.Errorf("j should move down a node, treeNode = %d", m.treeNode)
	}
	m = send(t, m, key("k"))
	if m.treeNode != 0 {
		t.Errorf("k should move up a node, treeNode = %d", m.treeNode)
	}
	m = send(t, m, key("up"))
	if m.treeNode != 0 {
		t.Errorf("up should stay at the first node, treeNode = %d", m.treeNode)
	}

	// On the tree screen ? opens help; h is a move, not help.
	m = send(t, m, key("?"))
	if m.screen != screenHelp {
		t.Errorf("? should open help on the tree screen, screen = %v", m.screen)
	}
}

func characterModel(t *testing.T, w, h int) model {
	t.Helper()
	m := newFlowModel(t)
	m.width, m.height = w, h
	m.updateSizes()
	for i, name := range []string{"Python", "Reading", "Listen", "Go", "Cooking"} {
		m.skills = append(m.skills, db.Skill{ID: int64(i + 1), Name: name, Category: "Mind", TotalXP: 500 - i*50})
	}
	// One event just after midnight today and one late yesterday, so the
	// day labels hold whatever time the test runs.
	y, mo, d := time.Now().Date()
	midnight := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	m.xpEvents = []db.XPEvent{
		{SkillName: "Go", Category: "Mind", Activity: "Challenge: Write a loop", XP: 8, CreatedAt: midnight.Add(time.Minute)},
		{SkillName: "Go", Category: "Mind", Activity: "Learn Go generics", XP: 36, CreatedAt: midnight.Add(-time.Hour)},
	}
	m.screen = screenRoad
	return m
}

func TestCharacterGridMovesAndOpensTree(t *testing.T) {
	m := characterModel(t, 186, 50) // two cards per row at this width

	m = send(t, m, key("l"))
	if m.charCursor != 1 {
		t.Fatalf("l should select the next card, cursor = %d", m.charCursor)
	}
	m = send(t, m, key("j"))
	if m.charCursor != 3 {
		t.Fatalf("j should move down a row (two columns), cursor = %d", m.charCursor)
	}
	m = send(t, m, key("down"))
	if m.charCursor != 3 {
		t.Errorf("down from the last full row should stay put, cursor = %d", m.charCursor)
	}

	m = send(t, m, key("enter"))
	if m.screen != screenTree || m.treeSkill != 3 {
		t.Fatalf("enter should open the selected skill's tree, screen=%v skill=%d", m.screen, m.treeSkill)
	}
	m = send(t, m, key("esc"))
	if m.screen != screenRoad {
		t.Errorf("leaving the tree should return to the character screen, screen=%v", m.screen)
	}
}

func TestCharacterScreenFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{186, 50}, {120, 36}, {80, 24}} {
		m := characterModel(t, size[0], size[1])
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Errorf("%dx%d: view has %d lines", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > m.width {
				t.Errorf("%dx%d: line %d is %d wide", size[0], size[1], i, w)
			}
		}
		for _, want := range []string{"LV ", "Python", "Today", "Yesterday"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: character screen should show %q", size[0], size[1], want)
			}
		}
	}
}

func TestDayLabel(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	cases := map[time.Time]string{
		time.Date(2026, 10, 13, 1, 0, 0, 0, time.Local):  "Today",
		time.Date(2026, 10, 12, 23, 0, 0, 0, time.Local): "Yesterday",
		time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local): "Sat Oct 10",
	}
	for when, want := range cases {
		if got := dayLabel(when, now); got != want {
			t.Errorf("dayLabel(%v) = %q, want %q", when, got, want)
		}
	}
}

func TestRoadEntryMarksChallengesAndPlans(t *testing.T) {
	challenge := roadEntry(db.XPEvent{SkillName: "Go", Category: "Mind", Activity: "Challenge: Write a loop", XP: 8}, 70)
	if !strings.Contains(challenge, "★") || strings.Contains(challenge, "Challenge:") {
		t.Errorf("a challenge should show ★ without the prefix: %q", challenge)
	}
	plan := roadEntry(db.XPEvent{SkillName: "Go", Category: "Mind", Activity: "Learn generics", XP: 36}, 70)
	if !strings.Contains(plan, "✎") {
		t.Errorf("a plan should show ✎: %q", plan)
	}
	if w := lipgloss.Width(plan); w != 70 {
		t.Errorf("an entry should fill its width, got %d", w)
	}
}
