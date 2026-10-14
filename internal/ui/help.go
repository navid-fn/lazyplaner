package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// helpPanelH is the outer height of the help popup.
const helpPanelH = 20

// helpTabs are the sections of the help popup, in order.
var helpTabs = []string{"Skills", "Levels", "Categories", "Plans", "Keys"}

// openHelp shows the help popup over the current screen. Closing it returns
// to the same screen.
func (m *model) openHelp() {
	m.prevScreen = m.screen
	m.screen = screenHelp
	m.helpTab = 0
}

func (m model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(helpTabs)
	switch msg.String() {
	case "esc", "h", "?":
		m.screen = m.prevScreen
	case "right", "tab", "l":
		m.helpTab = (m.helpTab + 1) % n
	case "left", "shift+tab":
		m.helpTab = (m.helpTab - 1 + n) % n
	}
	return m, nil
}

// renderHelp draws the popup: a tab row and the text of the chosen section.
func (m model) renderHelp() string {
	w := clamp(m.width-6, 50, 74)
	h := min(helpPanelH, m.bodyHeight())

	var tabs []string
	for i, name := range helpTabs {
		if i == m.helpTab {
			tabs = append(tabs, stActive.Reverse(true).Render(" "+name+" "))
		} else {
			tabs = append(tabs, stDim.Render(" "+name+" "))
		}
	}

	content := strings.Join(tabs, " ") + "\n\n" + helpSection(helpTabs[m.helpTab], w-4)
	return panel("Help", content, w, h, true)
}

// helpSection returns the text of one tab. Lines the person can type are
// marked ✓ and things they cannot are marked ✗.
func helpSection(name string, width int) string {
	ok := func(s string) string { return stSuccess.Render("✓ ") + s }
	no := func(s string) string { return stError.Render("✗ ") + s }
	head := func(s string) string { return stActive.Render(s) }
	dim := func(s string) string { return stDim.Render(s) }

	var lines []string
	switch name {
	case "Skills":
		lines = []string{
			head("Skill names, typed in the setup screen (s) or edit skill (e)"),
			"",
			ok(fmt.Sprintf("1 to %d words, up to %d characters", rpg.MaxSkillWords, rpg.MaxSkillLength)),
			ok("Letters, numbers and spaces"),
			ok("Symbols: + # . - / & ' (C++, CI/CD, R&D, Node.js)"),
			ok("Examples: Go, Running, Public speaking"),
			"",
			head("Not accepted"),
			no("Emoji and symbols like ! ? @ % , ;"),
			no("Goals or sentences: \"get fit by June\" (use \"Fitness\")"),
			no("A skill you already have, ignoring case: Go and go are one"),
			no("Empty names or names with no letters"),
			"",
			dim(fmt.Sprintf("Up to %d skills at setup. Pick one in the list to edit it.", rpg.MaxStartSkills)),
		}
	case "Levels":
		lines = []string{
			head("Level is how good you are now, from 1 to 5"),
			"",
			ok("1 Beginner    2 Novice    3 Intermediate"),
			ok("4 Advanced    5 Expert"),
			"",
			dim("Your start level sets your starting XP and tree position."),
			dim("Nodes above your level stay locked until you reach them."),
			"",
			head("Or let the auditor decide"),
			dim("Describe your experience in one sentence in setup, and the AI"),
			dim("picks the level. Nodes below your start level begin done."),
			"",
			head("Editing a skill resets it to level 1"),
			no("Its XP and node progress go back to 0. XP history is kept."),
		}
	case "Categories":
		lines = []string{
			head("A category groups skills on the character sheet"),
			"",
			ok("Mind     learning, study, reading"),
			ok("Body     exercise, health, sleep"),
			ok("Craft    making or building things, technical skills"),
			ok("Social   people, communication"),
			ok("Life     chores, errands, admin, money"),
			"",
			dim("You don't pick it. The AI chooses one when a skill's tree is"),
			dim("built, and when a plan is saved."),
		}
	case "Plans":
		lines = []string{
			head("A plan is one thing you intend to do"),
			"",
			ok("Subject: a title, up to 120 characters, e.g. \"Go generics\""),
			ok("Description: what you want to do, up to 2000 characters"),
			ok("Any text. Both fields are required"),
			"",
			dim("ctrl+s generates a markdown description with the chosen model."),
			dim("Saving a plan also gives it XP on the skill it trains."),
			"",
			no("Empty subject or description"),
			no("\"buy milk\" is a plan, not a skill"),
		}
	case "Keys":
		lines = []string{
			head("Plan list"),
			dim("  ctrl+n new plan · e edit · d delete · q quit"),
			head("Anywhere"),
			dim("  ctrl+o choose the model (esc or ctrl+o closes the picker)"),
			head("Screens"),
			dim("  c character & road · t skill tree · g today's directives"),
			dim("  s character setup · h this help"),
			head("Character"),
			dim("  hjkl or arrows move between skills · enter opens its tree"),
			head("Skill tree"),
			dim("  ←/→ or h/l switch skill · ↑/↓ or j/k move · e edit · ? help"),
			dim("  enter completes today's open directive for the selected node"),
			head("Today's directives (home screen)"),
			dim("  g generates the day's set: ⚡ Recon, 🔧 Core Op, 🔥 Deep Push"),
			dim("  ↑/↓ move · enter complete · r rerolls the highlighted open one"),
			dim("  Once all are done, g gives one ★ bonus directive per day."),
			head("Setup"),
			dim("  tab/↑↓ next field · enter add/save · ctrl+r switch form ⇄ roster"),
			dim("  roster: ↑↓ pick · f/space toggle active/frozen · enter edit · ctrl+d remove"),
			dim("  ctrl+s build trees · esc skip (or cancel edit)"),
		}
	}

	for i, l := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(width).Render(l)
	}
	return strings.Join(lines, "\n")
}
