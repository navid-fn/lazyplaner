package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// onboardField is the input that has focus on the setup screen.
type onboardField int

const (
	fieldName       onboardField = iota
	fieldExperience              // optional sentence for the skill auditor
	fieldGoal                    // optional goal that shapes the tree
	fieldLevel                   // manual level; skipped while the auditor sets it
	fieldList                    // the pending skills, for editing or removing
	fieldCount
)

// maxActiveFronts is how many start skills begin active. The rest start
// garrisoned, so a new player is not handed eight skills to keep from
// rusting at once.
const maxActiveFronts = 3

// levelNames labels the self-rated levels 1 to 5. The rating is also the
// character's starting level in that skill.
var levelNames = []string{"", "Beginner", "Novice", "Intermediate", "Advanced", "Expert"}

// setupIdeas are example skills, shown as pills under the form for
// inspiration. They are not added unless typed.
var setupIdeas = []string{
	"Go", "Python", "Java", "Drawing", "Cooking", "Running", "Yoga", "Chess",
	"Spanish", "Guitar", "Public speaking", "Budgeting", "Woodworking", "Gardening",
}

// pendingSkill is a start skill entered on the setup screen, not yet saved.
//
// The level is picked by hand, or, when Experience is given, set by the
// skill auditor along with Rusty and Category. Goal shapes the tree.
type pendingSkill struct {
	Name       string
	Level      int
	Experience string // the person's own account of their standing, if any
	Goal       string // what they want to master or build, if any
	Evidence   string // the auditor's summary of Experience
	Category   string // from the auditor; empty until it answers or without one
	Rusty      bool
	Auditing   bool // the auditor is still working on it
}

// garrisoned reports whether the pending skill at index i starts parked.
func garrisoned(i int) bool { return i >= maxActiveFronts }

// onboardForm holds the state of the character setup screen.
type onboardForm struct {
	name       textinput.Model
	experience textinput.Model
	goal       textinput.Model
	field      onboardField
	level      int // manual level, 1 to 5
	pending    []pendingSkill
	cursor     int // selected row in pending, used when the list has focus
	editing    int // index in pending being edited, or -1

	// note is the message under the name field. It is an error after a
	// rejected entry and a hint otherwise.
	note      string
	noteError bool
}

const (
	onboardHint     = "1 to 3 words, e.g. Golang or Java. ? for help."
	onboardEditHint = "Change it and press enter to save, esc to cancel."
)

func newOnboardForm() onboardForm {
	name := textinput.New()
	name.Placeholder = "e.g. Golang or Java"
	name.CharLimit = rpg.MaxSkillLength

	exp := textinput.New()
	exp.Placeholder = "e.g. Wrote Go at work for 4 years, stopped in 2024"
	exp.CharLimit = rpg.MaxExperienceLength

	goal := textinput.New()
	goal.Placeholder = "e.g. Build a REST API, pass the AWS exam"
	goal.CharLimit = rpg.MaxGoalLength

	return onboardForm{name: name, experience: exp, goal: goal, level: 3, editing: -1, note: onboardHint}
}

// levelLocked is true while the experience field has text: the auditor sets
// the level, so the manual selector is hidden and skipped.
func (f onboardForm) levelLocked() bool {
	return strings.TrimSpace(f.experience.Value()) != ""
}

// auditing reports whether any pending skill is waiting on the auditor.
func (f onboardForm) auditing() bool {
	for _, p := range f.pending {
		if p.Auditing {
			return true
		}
	}
	return false
}

// openOnboard shows the character setup screen with the name field focused.
func (m *model) openOnboard() tea.Cmd {
	m.screen = screenOnboard
	m.onboard = newOnboardForm()
	m.sizeOnboardInputs()
	m.errorText = ""
	m.statusText = ""
	return m.onboard.name.Focus()
}

// onboardPanels returns the widths of the left (skills) and right (form)
// panels on the setup screen.
func (m model) onboardPanels() (int, int) {
	leftW := clamp(m.width*2/5, 36, 60)
	return leftW, m.width - leftW
}

// sizeOnboardInputs fits the text inputs to the form's boxes. Each box has a
// border and one cell of padding; the input adds a 2-cell prompt and a
// cursor cell on top of its Width.
func (m *model) sizeOnboardInputs() {
	_, rightW := m.onboardPanels()
	w := max(rightW-4-4-3, 10)
	m.onboard.name.Width = w
	m.onboard.experience.Width = w
	m.onboard.goal.Width = w
}

// ─── Keys ────────────────────────────────────────────────────────────────────

func (m model) handleOnboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isLoading {
		return m, nil
	}
	f := &m.onboard

	switch msg.String() {
	case "?":
		m.prevScreen = screenOnboard
		m.screen = screenHelp
		m.helpTab = 0
		return m, nil

	case "esc":
		// While editing, esc cancels the edit. Otherwise it skips setup.
		if f.editing >= 0 {
			f.cancelEdit()
			cmd := f.setField(fieldName)
			return m, cmd
		}
		m.screen = screenMain
		f.setField(fieldList)
		m.statusText = "Setup skipped. Add skills later, or it opens again on the next start."
		return m, nil

	case "tab", "shift+tab":
		step := 1
		if msg.String() == "shift+tab" {
			step = -1
		}
		cmd := f.setField(f.nextField(step))
		return m, cmd

	case "up", "down":
		if f.field == fieldList && len(f.pending) > 0 {
			step := 1
			if msg.String() == "up" {
				step = -1
			}
			f.cursor = clamp(f.cursor+step, 0, len(f.pending)-1)
		}
		return m, nil

	case "left", "right":
		if f.field == fieldLevel && !f.levelLocked() {
			dir := 1
			if msg.String() == "left" {
				dir = -1
			}
			f.level = clamp(f.level+dir, 1, 5)
			return m, nil
		}

	case "enter":
		if f.field == fieldList {
			return m.loadPendingForEdit()
		}
		// Enter adds the skill, or saves the edit, from any field.
		return m.commitSkill()

	case "ctrl+d":
		return m.removePending()

	case "ctrl+s":
		if len(f.pending) == 0 {
			f.note, f.noteError = "Add at least one skill before building.", true
			return m, nil
		}
		for _, p := range f.pending {
			if p.Auditing {
				f.note, f.noteError = "The auditor is still reading "+p.Name+". Build once it is done.", true
				return m, nil
			}
		}
		m.isLoading = true
		m.loadingText = "Designing your skill trees…"
		m.errorText = ""
		m.statusText = ""
		return m, tea.Batch(m.spinner.Tick, buildSkillTreesCmd(m.client, m.store, f.pending))
	}

	// Typing goes to the name field when the level or list has focus, so
	// changing the level and then typing a name just works.
	if msg.Type == tea.KeyRunes && (f.field == fieldLevel || f.field == fieldList) {
		cmd := f.setField(fieldName)
		var typed tea.Cmd
		f.name, typed = f.name.Update(msg)
		return m, tea.Batch(cmd, typed)
	}

	var cmd tea.Cmd
	switch f.field {
	case fieldName:
		f.name, cmd = f.name.Update(msg)
	case fieldExperience:
		f.experience, cmd = f.experience.Update(msg)
	case fieldGoal:
		f.goal, cmd = f.goal.Update(msg)
	default:
		return m, nil
	}
	// A new edit clears the old message; the hint returns.
	if f.noteError {
		f.note, f.noteError = onboardHint, false
	}
	return m, cmd
}

// nextField is the field step positions away from the current one, skipping
// the level selector while the auditor owns the level.
func (f onboardForm) nextField(step int) onboardField {
	next := f.field
	for range fieldCount {
		next = onboardField((int(next) + step + int(fieldCount)) % int(fieldCount))
		if next != fieldLevel || !f.levelLocked() {
			return next
		}
	}
	return next
}

// setField moves focus to next, blurring the text inputs it leaves and
// focusing the one it enters.
func (f *onboardForm) setField(next onboardField) tea.Cmd {
	f.field = next
	f.name.Blur()
	f.experience.Blur()
	f.goal.Blur()
	switch next {
	case fieldName:
		return f.name.Focus()
	case fieldExperience:
		return f.experience.Focus()
	case fieldGoal:
		return f.goal.Focus()
	}
	return nil
}

// resetInputs clears the three text inputs.
func (f *onboardForm) resetInputs() {
	f.name.Reset()
	f.experience.Reset()
	f.goal.Reset()
}

// cancelEdit leaves edit mode without changing the pending skill.
func (f *onboardForm) cancelEdit() {
	f.editing = -1
	f.resetInputs()
	f.note, f.noteError = onboardHint, false
}

// existingNames lists the names a skill being typed must not clash with. The
// skill being edited is left out, so saving it under its own name is fine.
func (m model) existingNames() []string {
	f := m.onboard
	names := make([]string, 0, len(f.pending)+len(m.skills))
	for i, p := range f.pending {
		if i != f.editing {
			names = append(names, p.Name)
		}
	}
	for _, s := range m.skills {
		names = append(names, s.Name)
	}
	return names
}

// commitSkill adds the typed skill, or saves it over the one being edited.
// With an experience sentence, the auditor is asked at once, so the card on
// the left fills in with the level, category and rust while the next skill
// is typed. A rejected name stays in the field so it can be fixed.
func (m model) commitSkill() (tea.Model, tea.Cmd) {
	f := &m.onboard
	name := strings.TrimSpace(f.name.Value())

	if f.editing < 0 && len(f.pending) >= rpg.MaxStartSkills {
		f.note = fmt.Sprintf("Up to %d skills per setup. Press ctrl+s to build, or ctrl+d to remove one.", rpg.MaxStartSkills)
		f.noteError = true
		return m, nil
	}
	if err := rpg.ValidateSkillName(name, m.existingNames()); err != nil {
		f.note = err.Error()
		f.noteError = true
		return m, nil
	}

	experience := strings.TrimSpace(f.experience.Value())
	skill := pendingSkill{
		Name:       name,
		Level:      f.level,
		Experience: experience,
		Goal:       strings.TrimSpace(f.goal.Value()),
		Auditing:   experience != "",
	}
	var audit tea.Cmd
	if skill.Auditing {
		audit = tea.Batch(m.spinner.Tick, auditSkillCmd(m.client, name, experience))
	}

	verb := "Added"
	if f.editing >= 0 {
		f.pending[f.editing] = skill
		f.cursor = f.editing
		f.editing = -1
		verb = "Saved"
	} else {
		f.pending = append(f.pending, skill)
		f.cursor = len(f.pending) - 1
	}
	f.resetInputs()
	if skill.Auditing {
		f.note, f.noteError = fmt.Sprintf("%s %s. The auditor is reading your experience…", verb, name), false
	} else {
		f.note, f.noteError = fmt.Sprintf("%s %s. Add another, or ctrl+s to build.", verb, name), false
	}
	cmd := f.setField(fieldName)
	return m, tea.Batch(cmd, audit)
}

// applyAudit puts the auditor's answer on the pending skill it was for. The
// skill is matched by name; if it was removed or renamed meanwhile, the
// answer is dropped.
func (m *model) applyAudit(msg auditDoneMsg) {
	f := &m.onboard
	for i := range f.pending {
		p := &f.pending[i]
		if !strings.EqualFold(p.Name, msg.name) || !p.Auditing {
			continue
		}
		p.Auditing = false
		if msg.err != nil {
			f.note = fmt.Sprintf("The auditor could not read %s, so it stays at Lv %d. (%v)", p.Name, p.Level, msg.err)
			f.noteError = true
			return
		}
		p.Level = msg.audit.StartLevel
		p.Rusty = msg.audit.Rusty
		p.Evidence = msg.audit.Evidence
		p.Category = msg.audit.Category
		rust := ""
		if p.Rusty {
			rust = ", rusty"
		}
		f.note = fmt.Sprintf("Auditor: %s starts at Lv %d (%s%s).", p.Name, p.Level, levelNames[p.Level], rust)
		f.noteError = false
		return
	}
}

// loadPendingForEdit copies the selected skill into the form and moves focus
// to the name, so it can be changed and saved with enter.
func (m model) loadPendingForEdit() (tea.Model, tea.Cmd) {
	f := &m.onboard
	if len(f.pending) == 0 {
		return m, nil
	}
	p := f.pending[f.cursor]
	f.editing = f.cursor
	f.name.SetValue(p.Name)
	f.name.CursorEnd()
	f.experience.SetValue(p.Experience)
	f.goal.SetValue(p.Goal)
	f.level = p.Level
	f.note, f.noteError = onboardEditHint, false
	cmd := f.setField(fieldName)
	return m, cmd
}

// removePending deletes the selected skill from the list, or the last one
// when the list does not have focus.
func (m model) removePending() (tea.Model, tea.Cmd) {
	f := &m.onboard
	if len(f.pending) == 0 {
		return m, nil
	}
	idx := len(f.pending) - 1
	if f.field == fieldList {
		idx = f.cursor
	}
	removed := f.pending[idx].Name
	f.pending = append(f.pending[:idx], f.pending[idx+1:]...)
	f.cursor = clamp(f.cursor, 0, max(len(f.pending)-1, 0))
	if f.editing == idx {
		f.cancelEdit()
	} else if f.editing > idx {
		f.editing--
	}
	f.note, f.noteError = fmt.Sprintf("Removed %s.", removed), false
	return m, nil
}

// ─── Screen ──────────────────────────────────────────────────────────────────

// renderOnboardScreen draws the setup screen as two panels filling the body
// area: the skills added so far on the left, and the form on the right.
func (m model) renderOnboardScreen() string {
	f := m.onboard
	bodyH := m.bodyHeight()
	leftW, rightW := m.onboardPanels()

	title := "Add a skill"
	if f.editing >= 0 {
		title = "Edit " + f.pending[f.editing].Name
	}
	left := panel(fmt.Sprintf("Your skills %d/%d", len(f.pending), rpg.MaxStartSkills),
		m.renderPendingList(leftW-2), leftW, bodyH, f.field == fieldList)
	right := panel(title, m.renderOnboardForm(rightW-4), rightW, bodyH, f.field != fieldList)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderPendingList shows the start skills as cards in two groups: the first
// three are active fronts, the rest wait in the garrison.
func (m model) renderPendingList(width int) string {
	f := m.onboard
	if len(f.pending) == 0 {
		return " " + stDim.Render("No skills yet.") + "\n\n " +
			stDim.Render("Type one on the right and press enter.")
	}

	var lines []string
	lines = append(lines, sectionHeader("⚔️  Active Fronts",
		fmt.Sprintf("%d/%d", min(len(f.pending), maxActiveFronts), maxActiveFronts), width))
	for i := 0; i < min(len(f.pending), maxActiveFronts); i++ {
		lines = append(lines, m.pendingCard(i, width))
	}

	lines = append(lines, "", sectionHeader("🛡️  Garrisoned / Backlog",
		fmt.Sprint(max(len(f.pending)-maxActiveFronts, 0)), width))
	if len(f.pending) <= maxActiveFronts {
		lines = append(lines,
			" "+stDim.Render(fmt.Sprintf("Skills %d–%d wait here, frozen, so you", maxActiveFronts+1, rpg.MaxStartSkills)),
			" "+stDim.Render("can focus on three at a time."))
	}
	for i := maxActiveFronts; i < len(f.pending); i++ {
		lines = append(lines, m.pendingCard(i, width))
	}

	lines = append(lines, "", " "+startingCharacter(f.pending))
	return strings.Join(lines, "\n")
}

// sectionHeader is a title with a count on the right and a rule between.
func sectionHeader(title, count string, width int) string {
	left := " " + stActive.Render(title) + " "
	right := " " + stDim.Render(count) + " "
	rule := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", rule)) + right
}

// pendingCard draws one start skill: its level and badges, then its goal or
// the auditor's reading.
func (m model) pendingCard(i, width int) string {
	f := m.onboard
	p := f.pending[i]
	inner := width - 4
	frozen := garrisoned(i)
	selected := f.field == fieldList && i == f.cursor

	label, labelInk := "auto", "242"
	if p.Category != "" {
		label, labelInk = p.Category, string(categoryColor(p.Category))
	}

	border, titleInk := string(colorInactive), "252"
	if frozen {
		border, titleInk = "238", "245"
	}
	title := p.Name
	switch {
	case i == f.editing:
		title = "✎ " + p.Name
		border = string(colorActive)
	case selected:
		border = inkSelect
	}

	// Line 1: level and where it came from, with badges on the right.
	var levelPart string
	if p.Auditing {
		levelPart = m.spinner.View() + " " + stActive.Render("auditing…")
	} else {
		source := "set by hand"
		if p.Evidence != "" {
			source = "by AI audit"
		}
		levelPart = chipLabel(fmt.Sprintf("LV %d", p.Level), colorActive) + " " +
			stDim.Render(levelNames[p.Level]+" · "+source)
	}
	var badges []string
	if p.Rusty {
		badges = append(badges, chipLabel("⚠ RUSTY", lipgloss.Color(inkRusty)))
	}
	if frozen {
		badges = append(badges, chipLabel("FROZEN", lipgloss.Color("#8be9fd")))
	}
	// The badges always show; the level text gives way when space is short.
	right := strings.Join(badges, " ")
	room := inner
	if right != "" {
		room = inner - lipgloss.Width(right) - 2 // spread needs a 2-cell gap
	}
	line1 := spread(ansi.Truncate(levelPart, max(room, 1), "…"), right, inner)

	// Line 2: the goal, or else what the auditor read.
	var line2 string
	switch {
	case p.Goal != "":
		line2 = "🎯 " + lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(p.Goal)
	case p.Evidence != "":
		line2 = stDim.Render("“" + p.Evidence + "”")
	default:
		line2 = stDim.Render("no goal set")
	}
	line2 = ansi.Truncate(line2, inner, "…")

	return cardFrame(title, label, labelInk, []string{line1, line2}, width, border, titleInk, selected)
}

// startingCharacter summarises the character the pending skills would give:
// the starting XP is the sum of each skill's starting level.
func startingCharacter(pending []pendingSkill) string {
	total := 0
	for _, p := range pending {
		total += rpg.TotalXPForLevel(p.Level)
	}
	level := rpg.ProgressFor(total).Level
	return stDim.Render("Starting character  ") +
		chipLabel(fmt.Sprintf("LV %d", level), colorActive) +
		stDim.Render(fmt.Sprintf("  %s · %d xp", rpg.RankTitle(level), total))
}

// renderOnboardForm draws the three inputs, the level, the keys and ideas.
func (m model) renderOnboardForm(width int) string {
	f := m.onboard
	noteStyle := stDim
	if f.noteError {
		noteStyle = stError
	}
	optional := stDim.Render("optional")

	lines := []string{
		fieldHeader("Skill", counter(f.name.Value(), rpg.MaxSkillLength), f.field == fieldName, width),
		inputBox(f.name.View(), width, f.field == fieldName),
		ansi.Truncate(noteStyle.Render(f.note), width, "…"),
		"",
		fieldHeader("Current Standing / Experience", optional, f.field == fieldExperience, width),
		inputBox(f.experience.View(), width, f.field == fieldExperience),
		ansi.Truncate(stDim.Render("Leave blank if starting from zero; otherwise 1 sentence for AI level audit"), width, "…"),
		"",
		fieldHeader("Target Focus / Goal", optional, f.field == fieldGoal, width),
		inputBox(f.goal.View(), width, f.field == fieldGoal),
		ansi.Truncate(stDim.Render("What do you want to master or build? Used to tailor the skill tree"), width, "…"),
		"",
		m.levelRow(),
		"",
	}

	if m.isLoading {
		lines = append(lines, m.spinner.View()+" "+stActive.Render(m.loadingText))
	} else {
		lines = append(lines, wrapPills([]string{
			keyChip("enter", "add"), keyChip("tab", "next"), keyChip("ctrl+s", "build"),
			keyChip("?", "help"), keyChip("esc", "skip"),
		}, width, "  "))
	}

	ideaStyle := lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("252")).Padding(0, 1)
	var ideas []string
	for _, idea := range setupIdeas {
		ideas = append(ideas, ideaStyle.Render(idea))
	}
	lines = append(lines, "", stActive.Render("Ideas"), wrapPills(ideas, width, " "))
	return strings.Join(lines, "\n")
}

// levelRow is the manual level selector, or a locked badge while the
// experience field has text and the auditor sets the level.
func (m model) levelRow() string {
	f := m.onboard
	if f.levelLocked() {
		badge := lipgloss.NewStyle().Foreground(lipgloss.Color("#8be9fd")).Bold(true).
			Render("[ 🤖 AI Auto-Calibration Active ]")
		return badge + "  " + stDim.Render("level comes from your experience")
	}
	focused := f.field == fieldLevel
	arrow, label := stDim, stDim
	if focused {
		arrow, label = stActive, stActive
	}
	return label.Render("Starting level  ") + arrow.Render("‹ ") +
		stActive.Render(fmt.Sprintf("%d · %s", f.level, levelNames[f.level])) + arrow.Render(" ›")
}

// wrapPills lays items out left to right, starting a new line whenever the
// next one would not fit in width.
func wrapPills(items []string, width int, sep string) string {
	var lines []string
	line := ""
	for _, it := range items {
		switch {
		case line == "":
			line = it
		case lipgloss.Width(line)+lipgloss.Width(sep)+lipgloss.Width(it) <= width:
			line += sep + it
		default:
			lines = append(lines, line)
			line = it
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
