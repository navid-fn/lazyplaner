package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Sizes of the help popup.
const (
	helpMaxW   = 80
	helpMaxH   = 32
	helpChrome = 6  // top padding, tab row, rule, blank, footer, bottom padding
	keyCapW    = 11 // keycaps are padded to this width so descriptions align
)

// Help tabs, in order. Keys comes first: it is what help is opened for.
const (
	helpKeys = iota
	helpRules
	helpAI
)

var helpTabs = []string{"⌨️ Keys", "📜 Game Rules", "🤖 AI & Setup"}

// keyGroup is one section of the Keys tab.
type keyGroup struct {
	title string
	rows  [][2]string // keycap, what it does
}

// Key groups, in the order the Keys tab lists them.
const (
	groupGlobal = iota
	groupHome
	groupTree
	groupNotes
	groupCharacter
)

var keyGroups = []keyGroup{
	{"Global & Screens", [][2]string{
		{"c", "character sheet & road"},
		{"t", "skill tree"},
		{"n", "field notes"},
		{"g", "today's directives: generate, then bonus"},
		{"s", "character setup"},
		{"ctrl+o", "choose the Ollama model"},
		{"h / ?", "this help (? on screens where h moves)"},
		{"q", "quit (ctrl+c anywhere)"},
	}},
	{"Home & Directives", [][2]string{
		{"tab", "switch Directives ⇄ Plans"},
		{"j/k ↑/↓", "move in the focused panel"},
		{"enter", "start the directive; again to finish it with a proof note"},
		{"r", "reroll the highlighted open directive"},
		{"ctrl+n", "new plan (the model writes it)"},
		{"enter", "read the plan, markdown rendered (j/k scroll, esc back)"},
		{"e", "edit the plan in $EDITOR"},
		{"d", "delete the plan (y confirms)"},
	}},
	{"Skill Tree", [][2]string{
		{"tab", "next skill (shift+tab previous)"},
		{"h / l", "parent card / first child card"},
		{"j / k", "next card down / up the column"},
		{"[ / ]", "jump to the previous / next era"},
		{"enter", "start / finish the node's directive"},
		{"g", "focus directive for an open node (again rerolls it)"},
		{"x", "expand into the next era (once the capstone is done)"},
		{"e", "edit the skill (resets it)"},
		{"t / esc", "back"},
	}},
	{"Field Notes", [][2]string{
		{"a", "new note (ctrl+s saves, esc cancels)"},
		{"e", "edit note"},
		{"d", "delete note (y confirms)"},
		{"x", "analyze with the model"},
		{"c / enter", "claim XP for work in the note"},
		{"p", "turn the note into a plan"},
		{"j / k", "move · n back"},
	}},
	{"Character & Setup", [][2]string{
		{"hjkl", "character: move between skills"},
		{"enter", "character: open the skill's tree"},
		{"tab", "setup: next field"},
		{"ctrl+r", "setup: switch form ⇄ roster"},
		{"f / space", "setup: toggle active ⇄ frozen"},
		{"ctrl+d", "setup: remove the skill"},
		{"ctrl+s", "setup: build the trees"},
		{"esc", "setup: skip (or cancel the edit)"},
	}},
}

// helpGroupFor is the key group for the screen help was opened from.
func helpGroupFor(s screen) int {
	switch s {
	case screenMain:
		return groupHome
	case screenTree, screenSkillEdit:
		return groupTree
	case screenNotes, screenNoteEdit:
		return groupNotes
	case screenRoad, screenOnboard:
		return groupCharacter
	}
	return groupGlobal
}

// ─── Keys ────────────────────────────────────────────────────────────────────

// openHelp shows the help popup over the current screen on the Keys tab,
// scrolled to the keys of that screen. Closing it returns to the screen.
func (m *model) openHelp() {
	m.prevScreen = m.screen
	m.screen = screenHelp
	m.setHelpTab(helpKeys)
}

// setHelpTab shows a tab from its top, or the Keys tab at the section of
// the screen help was opened from.
func (m *model) setHelpTab(tab int) {
	m.helpTab = tab
	m.helpScroll = 0
	if tab == helpKeys {
		here := helpGroupFor(m.prevScreen)
		_, starts := keysLines(here)
		m.helpScroll = starts[here]
	}
}

func (m model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(helpTabs)
	switch msg.String() {
	case "esc", "h", "?":
		m.screen = m.prevScreen
	case "right", "tab", "l":
		m.setHelpTab((m.helpTab + 1) % n)
	case "left", "shift+tab":
		m.setHelpTab((m.helpTab - 1 + n) % n)
	case "down", "j":
		m.helpScroll = min(m.helpScroll+1, m.helpMaxScroll())
	case "up", "k":
		m.helpScroll = max(m.helpScroll-1, 0)
	}
	return m, nil
}

// ─── Screen ──────────────────────────────────────────────────────────────────

// helpSize is the outer size of the help popup.
func (m model) helpSize() (int, int) {
	return clamp(m.width-4, 50, helpMaxW), min(helpMaxH, m.bodyHeight())
}

// helpBodyH is how many lines of the tab fit in the popup.
func (m model) helpBodyH() int {
	_, h := m.helpSize()
	return max(h-2-helpChrome, 1)
}

// helpMaxScroll is the furthest the current tab scrolls.
func (m model) helpMaxScroll() int {
	return max(len(helpSection(m.helpTab, helpGroupFor(m.prevScreen)))-m.helpBodyH(), 0)
}

// renderHelp draws the popup: the tab row, the chosen tab scrolled to
// helpScroll, and the key footer, inside one cell of padding top and
// bottom and two at the sides.
func (m model) renderHelp() string {
	w, h := m.helpSize()
	innerW := w - 2 - 4 // borders, then two cells of padding each side
	bodyH := m.helpBodyH()

	var tabs []string
	for i, name := range helpTabs {
		if i == m.helpTab {
			tabs = append(tabs, chipLabel(name, colorActive))
		} else {
			tabs = append(tabs, stDim.Render(" "+name+" "))
		}
	}

	body := helpSection(m.helpTab, helpGroupFor(m.prevScreen))
	maxScroll := m.helpMaxScroll()
	scroll := clamp(m.helpScroll, 0, maxScroll)
	body = body[scroll:min(scroll+bodyH, len(body))]
	for len(body) < bodyH {
		body = append(body, "")
	}

	footer := stDim.Render("←/→ switch tab · ? / esc close")
	if maxScroll > 0 {
		footer = stDim.Render("←/→ switch tab · ") + stKey.Render("↑/↓") +
			stDim.Render(fmt.Sprintf(" scroll %d/%d · ? / esc close", scroll+1, maxScroll+1))
	}

	lines := []string{"", strings.Join(tabs, " "), stSep.Render(strings.Repeat("─", innerW))}
	lines = append(lines, body...)
	lines = append(lines, "", lipgloss.PlaceHorizontal(innerW, lipgloss.Center, footer))
	for i, l := range lines {
		lines[i] = "  " + ansi.Truncate(l, innerW, "…")
	}
	return panel("Help", strings.Join(lines, "\n"), w, h, true)
}

// helpSection returns the lines of one tab. here is the key group of the
// screen help was opened from, marked on the Keys tab.
func helpSection(tab, here int) []string {
	switch tab {
	case helpRules:
		return rulesLines()
	case helpAI:
		return aiLines()
	}
	lines, _ := keysLines(here)
	return lines
}

// keysLines lays out every key group as a keycap column and a description
// column. starts holds the line each group begins at.
func keysLines(here int) (lines []string, starts []int) {
	for i, g := range keyGroups {
		if i > 0 {
			lines = append(lines, "")
		}
		starts = append(starts, len(lines))
		title := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Render(g.title)
		if i == here {
			title = stActive.Render("▍"+g.title) + " " + stDim.Render("← you are here")
		}
		lines = append(lines, title)
		for _, r := range g.rows {
			lines = append(lines, " "+keyCap(r[0])+"  "+r[1])
		}
	}
	return lines, starts
}

// keyCap draws a key as a highlighted pill, padded so descriptions align.
func keyCap(k string) string {
	pad := max(keyCapW-ansi.StringWidth(k), 0)
	return stChip.Render(" " + k + strings.Repeat(" ", pad) + " ")
}

// rulesLines is the Game Rules tab: fronts, the daily set, node states,
// and levels and stats.
func rulesLines() []string {
	head := func(s string) string { return stActive.Render(s) }
	dim := func(s string) string { return stDim.Render(s) }
	tier := func(t rpg.Tier, what string) string {
		label := t.Label()
		pad := strings.Repeat(" ", max(22-lipgloss.Width(label), 1))
		return " " + lipgloss.NewStyle().Foreground(colorKeyLabel).Render(label) + pad + dim(what)
	}
	state := func(chip, what string) string {
		return " " + chip + strings.Repeat(" ", max(14-lipgloss.Width(chip), 1)) + dim(what)
	}
	node := func(st rpg.Status) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(statusInk(st))).Render(statusIcon(st)) + " " + statusChip(st)
	}
	rustDays := int(rpg.RustAfter / (24 * time.Hour))

	var ranks []string
	for lv := 1; ; {
		ranks = append(ranks, fmt.Sprintf("%s %d", rpg.RankTitle(lv), lv))
		_, next, ok := rpg.NextRank(lv)
		if !ok {
			break
		}
		lv = next
	}
	var stats []string
	for _, c := range rpg.Categories {
		stats = append(stats, categoryStyle(c).Render(attributes[c])+" "+dim(c))
	}

	return []string{
		head(fmt.Sprintf("⚔️ Active fronts (max %d) vs ❄ FROZEN", maxActiveFronts)),
		dim(" Only active skills get daily directives, and only they can rust."),
		dim(" Frozen skills are garrisoned: no directives, no rust, XP kept."),
		"",
		head("🎯 Daily tactical set"),
		tier(rpg.TierRecon, "a quick warm-up or habit"),
		tier(rpg.TierCoreOp, "hands-on practice on an open node"),
		tier(rpg.TierDeepPush, "deep work that ends with something done"),
		dim(" r rerolls one open directive. Finish the set for one ★ bonus a day."),
		dim(" 🌿 After a sick or burnt-out day the set is two light Recovery tasks."),
		"",
		head("🌳 Skill tree states"),
		state(node(rpg.Done), "finished; its XP is in"),
		state(node(rpg.Unlocked), "open: your level is high enough"),
		state(node(rpg.Rusty), fmt.Sprintf("no XP for %dd: ×%.2f comeback XP", rustDays, rpg.ComebackMultiplier)),
		state(node(rpg.Locked), "needs a higher skill level"),
		state(stDim.Render(fogLabel), fmt.Sprintf("fog of war: more than %d levels above you", fogRange)),
		"",
		head("📈 Levels, ranks & stats"),
		dim(" " + strings.Join(ranks[:len(ranks)/2], " → ") + " →"),
		dim(" " + strings.Join(ranks[len(ranks)/2:], " → ")),
		" " + strings.Join(stats, "  "),
		dim(" XP is 1 per 5 minutes of effort, ×1.0 to ×2.5 for difficulty."),
	}
}

// aiLines is the AI & Setup tab: what the local model does, and what it
// never decides.
func aiLines() []string {
	head := func(s string) string { return stActive.Render(s) }
	dim := func(s string) string { return stDim.Render(s) }
	return []string{
		head("🧠 The model names things; Go does the math"),
		dim(" lazyplaner runs a local Ollama model (ctrl+o picks it). It reads and"),
		dim(" writes text only. XP, levels, rust, streaks and unlocks are fixed"),
		dim(" formulas in code, so the same work always earns the same XP."),
		"",
		head("🔍 Skill auditor & goal-driven trees (setup, s)"),
		dim(" Experience: one sentence about your past. The auditor sets your start"),
		dim(" level (1-5) and whether the skill is rusty; nodes below it start done."),
		dim(" Target goal: what you are working toward. It shapes the names of the"),
		dim(" 8-node branching tree; the shape and node levels come from code."),
		"",
		head("📋 Plans (ctrl+n)"),
		dim(" The model writes the plan, then picks its skill, node, difficulty and"),
		dim(" minutes. Saving it awards XP by the formula."),
		"",
		head("📓 Field notes (n)"),
		dim(" Each saved note is read for your status. Sick or exhausted shields the"),
		dim(" day's streak and lightens the next set. Work on a skill can be claimed"),
		dim(" as XP (c); a follow-up becomes a plan (p)."),
		"",
		stSep.Render(strings.Repeat("─", 30)),
		dim(fmt.Sprintf(" Skill names: 1-%d words, up to %d characters: letters, numbers,", rpg.MaxSkillWords, rpg.MaxSkillLength)),
		dim(" spaces and + # . - / & ' (C++, CI/CD). A skill, not a goal: Fitness."),
	}
}
