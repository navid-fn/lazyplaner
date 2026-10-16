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

// maxActiveFronts is how many skills can be active at once (see
// rpg.MaxActiveSkills). The rest start frozen.
const maxActiveFronts = rpg.MaxActiveSkills

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
	Category   string // from the auditor or the classifier; empty until one answers
	Rusty      bool
	Auditing   bool // the auditor is still working on it
	// Classifying is set while the model picks a category for a skill
	// added without experience (the auditor picks it otherwise).
	Classifying bool
	// Frozen skills start garrisoned. The list keeps active skills first,
	// so its order is the order the two roster groups show.
	Frozen bool
}

// activeCount is how many pending skills start active.
func activeCount(pending []pendingSkill) int {
	n := 0
	for _, p := range pending {
		if !p.Frozen {
			n++
		}
	}
	return n
}

// onboardForm holds the state of the character setup screen.
type onboardForm struct {
	name       textinput.Model
	experience textinput.Model
	goal       textinput.Model
	field      onboardField
	level      int // manual level, 1 to 5
	pending    []pendingSkill
	cursor     int          // selected row in pending, used when the list has focus
	editing    int          // index in pending being edited, or -1
	lastField  onboardField // form field tab returns to from the list

	// building is set while the trees are designed, one skill at a time;
	// built counts the skills saved so far.
	building bool
	built    int

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

// busy reports whether anything on the setup screen is waiting on the
// model, so the spinner keeps turning.
func (f onboardForm) busy() bool {
	if f.building || f.auditing() {
		return true
	}
	for _, p := range f.pending {
		if p.Classifying {
			return true
		}
	}
	return false
}

// resetNote puts back the hint for the current mode.
func (f *onboardForm) resetNote() {
	f.note, f.noteError = onboardHint, false
	if f.editing >= 0 {
		f.note = onboardEditHint
	}
}

// readingNote is the message shown while the auditor reads a skill.
func readingNote(verb, name string) string {
	return fmt.Sprintf("%s %s. The auditor is reading your experience…", verb, name)
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
	f := &m.onboard
	if m.isLoading || f.building {
		return m, nil
	}

	// The roster has its own keys: f or space moves a skill between the
	// active fronts and the garrison, and ←/→ (like ctrl+r) go back to the
	// form.
	if f.field == fieldList {
		switch msg.String() {
		case "f", " ":
			f.toggleFrozen()
			return m, nil
		case "left", "right":
			cmd := f.setField(f.lastField)
			return m, cmd
		}
	}

	switch msg.String() {
	case "?":
		m.openHelp()
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

	case "ctrl+r":
		// ctrl+r switches between the form and the roster.
		next := fieldList
		if f.field == fieldList {
			next = f.lastField
		}
		cmd := f.setField(next)
		return m, cmd

	case "tab", "shift+tab", "up", "down":
		// Tab and the arrows step through the form's fields; in the roster
		// the arrows pick a card and tab does nothing.
		step := 1
		if msg.String() == "up" || msg.String() == "shift+tab" {
			step = -1
		}
		if f.field == fieldList && (msg.String() == "tab" || msg.String() == "shift+tab") {
			return m, nil
		}
		if f.field == fieldList {
			if len(f.pending) > 0 {
				f.cursor = clamp(f.cursor+step, 0, len(f.pending)-1)
			}
			return m, nil
		}
		cmd := f.setField(f.nextField(step))
		return m, cmd

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
		// The form is locked while the trees are built: no cursor, no
		// edits, and the progress shows once, in the form.
		f.building, f.built = true, 0
		f.blurAll()
		f.note, f.noteError = "", false
		m.errorText = ""
		m.statusText = ""
		return m, tea.Batch(m.spinner.Tick, buildSkillTreeCmd(m.client, m.store, f.pending, 0))
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
	before := f.name.Value() + "\x00" + f.experience.Value() + "\x00" + f.goal.Value()
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
	// Typing starts a new entry, so the message about the last one (an
	// error, "Added …", the auditor's progress) gives way to the hint.
	if f.name.Value()+"\x00"+f.experience.Value()+"\x00"+f.goal.Value() != before {
		f.resetNote()
	}
	return m, cmd
}

// formFieldCount is the number of fields in the form; the list comes after
// them.
const formFieldCount = int(fieldList)

// nextField is the form field step positions away from the current one,
// wrapping around and skipping the level selector while the auditor owns
// the level.
func (f onboardForm) nextField(step int) onboardField {
	next := f.field
	for range formFieldCount {
		next = onboardField((int(next) + step + formFieldCount) % formFieldCount)
		if next != fieldLevel || !f.levelLocked() {
			return next
		}
	}
	return next
}

// toggleFrozen moves the selected skill between the active fronts and the
// garrison. Promoting a skill when all fronts are taken swaps it with the
// last active one. The list stays active-first, and the cursor follows the
// skill.
func (f *onboardForm) toggleFrozen() {
	if len(f.pending) == 0 {
		return
	}
	if f.editing >= 0 {
		f.note, f.noteError = "Finish or cancel the edit first (enter or esc).", true
		return
	}
	i := f.cursor
	moved := f.pending[i]
	var swapped string
	if moved.Frozen && activeCount(f.pending) >= maxActiveFronts {
		last := activeCount(f.pending) - 1
		f.pending[last].Frozen = true
		swapped = f.pending[last].Name
	}
	moved.Frozen = !moved.Frozen
	f.pending[i] = moved

	// Rebuild the order: actives, the promoted skill last among them; then
	// the frozen, a demoted skill first among them.
	var active, frozen []pendingSkill
	for j, p := range f.pending {
		if j == i {
			continue
		}
		if p.Frozen {
			frozen = append(frozen, p)
		} else {
			active = append(active, p)
		}
	}
	if moved.Frozen {
		frozen = append([]pendingSkill{moved}, frozen...)
	} else {
		active = append(active, moved)
	}
	if swapped != "" {
		// The demoted skill leads the garrison, just after the old order.
		for j, p := range frozen {
			if p.Name == swapped {
				frozen = append(append([]pendingSkill{p}, frozen[:j]...), frozen[j+1:]...)
				break
			}
		}
	}
	f.pending = append(active, frozen...)
	for j, p := range f.pending {
		if p.Name == moved.Name {
			f.cursor = j
		}
	}

	switch {
	case swapped != "":
		f.note = fmt.Sprintf("%s is now active; %s moved to the garrison.", moved.Name, swapped)
	case moved.Frozen:
		f.note = moved.Name + " moved to the garrison (frozen, it will not rust)."
	default:
		f.note = moved.Name + " is now an active front."
	}
	f.noteError = false
}

// setField moves focus to next, blurring the text inputs it leaves and
// focusing the one it enters.
func (f *onboardForm) setField(next onboardField) tea.Cmd {
	f.field = next
	if next != fieldList {
		f.lastField = next
	}
	f.blurAll()
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

// blurAll hides the cursor in every text input.
func (f *onboardForm) blurAll() {
	f.name.Blur()
	f.experience.Blur()
	f.goal.Blur()
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
	f.resetNote()
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
	name := rpg.NormalizeSkillName(f.name.Value())

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
	// Without experience the auditor does not run, so a small classifier
	// picks the category. An edit that keeps the name keeps its category.
	if !skill.Auditing {
		if f.editing >= 0 && strings.EqualFold(f.pending[f.editing].Name, name) {
			skill.Category = f.pending[f.editing].Category
		}
		skill.Classifying = skill.Category == ""
	}
	var work tea.Cmd
	switch {
	case skill.Auditing:
		work = tea.Batch(m.spinner.Tick, auditSkillCmd(m.client, name, experience))
	case skill.Classifying:
		work = tea.Batch(m.spinner.Tick, categorizeSkillCmd(m.client, name))
	}

	verb := "Added"
	if f.editing >= 0 {
		skill.Frozen = f.pending[f.editing].Frozen
		f.pending[f.editing] = skill
		f.cursor = f.editing
		f.editing = -1
		verb = "Saved"
	} else {
		// A new skill takes a free front, or else waits in the garrison.
		// Active skills stay ahead of frozen ones in the list.
		at := activeCount(f.pending)
		skill.Frozen = at >= maxActiveFronts
		if skill.Frozen {
			at = len(f.pending)
		}
		f.pending = append(f.pending[:at], append([]pendingSkill{skill}, f.pending[at:]...)...)
		f.cursor = at
	}
	f.resetInputs()
	if skill.Auditing {
		f.note, f.noteError = readingNote(verb, name), false
	} else {
		f.note, f.noteError = fmt.Sprintf("%s %s. Add another, or ctrl+s to build.", verb, name), false
	}
	cmd := f.setField(fieldName)
	return m, tea.Batch(cmd, work)
}

// applyAudit puts the auditor's answer on the pending skill it was for. The
// skill is matched by name; if it was removed or renamed meanwhile, the
// answer is dropped. The card shows the result, so the form's message only
// changes on an error. When the audit gives no category, the classifier is
// asked instead.
func (m *model) applyAudit(msg auditDoneMsg) tea.Cmd {
	f := &m.onboard
	for i := range f.pending {
		p := &f.pending[i]
		if !strings.EqualFold(p.Name, msg.name) || !p.Auditing {
			continue
		}
		p.Auditing = false
		if f.note == readingNote("Added", p.Name) || f.note == readingNote("Saved", p.Name) {
			f.resetNote()
		}
		if msg.err != nil {
			f.note = fmt.Sprintf("The auditor could not read %s, so it stays at Lv %d. (%v)", p.Name, p.Level, msg.err)
			f.noteError = true
		} else {
			p.Level = msg.audit.StartLevel
			p.Rusty = msg.audit.Rusty
			p.Evidence = msg.audit.Evidence
			p.Category = msg.audit.Category
		}
		if p.Category == "" {
			p.Classifying = true
			return categorizeSkillCmd(m.client, p.Name)
		}
		return nil
	}
	return nil
}

// applyCategory puts the classifier's answer on the pending skill it was
// for. A failed classification leaves the category to the tree builder.
func (m *model) applyCategory(msg categoryDoneMsg) {
	for i := range m.onboard.pending {
		p := &m.onboard.pending[i]
		if strings.EqualFold(p.Name, msg.name) && p.Classifying {
			p.Classifying = false
			if msg.err == nil {
				p.Category = msg.category
			}
			return
		}
	}
}

// handleTreeBuilt records one saved tree and starts the next. When the last
// one is saved, setup closes. On an error, the skills already saved leave
// the list, so building again continues with the rest.
func (m model) handleTreeBuilt(msg treeBuiltMsg) (tea.Model, tea.Cmd) {
	f := &m.onboard
	if !f.building {
		return m, nil
	}
	if msg.err != nil {
		f.building = false
		f.pending = f.pending[msg.index:]
		f.cursor = 0
		m.errorText = msg.err.Error()
		if msg.index > 0 {
			f.note, f.noteError = fmt.Sprintf("Saved %d tree%s. Press ctrl+s to retry the rest.",
				msg.index, plural(msg.index)), true
		}
		cmd := f.setField(fieldName)
		return m, cmd
	}

	f.built = msg.index + 1
	if f.built < len(f.pending) {
		return m, buildSkillTreeCmd(m.client, m.store, f.pending, f.built)
	}
	count := f.built
	f.building = false
	m.errorText = ""
	m.screen = screenMain
	m.statusText = fmt.Sprintf("Character created with %d skill tree%s.", count, plural(count))
	return m, tea.Batch(loadRoadCmd(m.store), loadDailyCmd(m.store))
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

	// Active skills come first in the list, so each group is a run of it.
	active := activeCount(f.pending)
	var lines []string
	lines = append(lines, sectionHeader("⚔️  Active Fronts",
		fmt.Sprintf("%d/%d", active, maxActiveFronts), width))
	for i := 0; i < active; i++ {
		lines = append(lines, m.pendingCard(i, width))
	}

	lines = append(lines, "", sectionHeader("🛡️  Garrisoned / Backlog",
		fmt.Sprint(len(f.pending)-active), width))
	if active == len(f.pending) {
		lines = append(lines,
			" "+stDim.Render("Skills past the first three wait here, frozen,"),
			" "+stDim.Render("so you can focus on three at a time."))
	}
	for i := active; i < len(f.pending); i++ {
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
	frozen := p.Frozen
	selected := f.field == fieldList && i == f.cursor

	label, labelInk := "auto", "242"
	if p.Classifying {
		label = "…"
	}
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

	// The build's progress shows here and only here; the keys are in the
	// footer.
	if f.building {
		current := ""
		if f.built < len(f.pending) {
			current = ": " + f.pending[f.built].Name
		}
		lines = append(lines, ansi.Truncate(m.spinner.View()+" "+stActive.Render(fmt.Sprintf(
			"Designing skill trees (%d/%d%s)…", f.built+1, len(f.pending), current)), width, "…"))
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
