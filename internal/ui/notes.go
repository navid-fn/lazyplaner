package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/llm"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// The Notes screen is the Field Log: daily notes the model reads for the
// day's status (a sick day shields the streak), work to claim as XP, and a
// follow-up task that can become a plan.

// Sizes of the note editor popup.
const (
	noteLimit       = 5000 // most characters in one note
	noteEditorMaxW  = 80
	noteEditorMaxH  = 22
	noteEditorExtra = 6 // panel borders, input box borders, counter and key rows
)

// Colors of the note signifiers.
const (
	inkBlocker = "#ffb86c" // ! blocked
	inkIdea    = "#8be9fd" // • a reflection or idea
)

// notesState is the Notes screen: the list of notes, the cursor in it, the
// editor popup used both to write a new note and to edit one, and the log
// of each day for the snapshot.
type notesState struct {
	list          []db.Note
	dayLogs       map[string]db.DayLog
	cursor        int
	editor        textarea.Model
	editID        int64 // the note being edited; 0 while writing a new one
	confirmDelete bool
	analyzing     int64  // the note the model is reading, 0 when none
	ret           screen // where leaving the notes goes back to
}

func newNoteEditor() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "What happened today?"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = noteLimit
	return ta
}

// ─── Keys ────────────────────────────────────────────────────────────────────

// openNotes shows the Notes screen and refreshes it, so the day snapshots
// include directives done since it was last open. Leaving it returns to the
// screen it was opened from.
func (m *model) openNotes() tea.Cmd {
	m.notes.ret = m.screen
	m.notes.confirmDelete = false
	m.screen = screenNotes
	m.errorText = ""
	m.statusText = ""
	return loadNotesCmd(m.store, 0)
}

func (m model) handleNotesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := &m.notes
	if n.confirmDelete {
		n.confirmDelete = false
		if msg.String() != "y" {
			m.statusText = "Delete cancelled."
			return m, nil
		}
		note := n.list[n.cursor]
		return m, deleteNoteCmd(m.store, note.ID)
	}

	note, has := m.selectedNote()
	switch msg.String() {
	case "esc", "n":
		m.screen = n.ret
		return m, nil
	case "q":
		return m, tea.Quit
	case "up", "k":
		n.cursor = max(n.cursor-1, 0)
		return m, nil
	case "down", "j":
		n.cursor = clamp(n.cursor+1, 0, max(len(n.list)-1, 0))
		return m, nil
	case "a", "ctrl+n":
		cmd := m.openNoteEditor(0)
		return m, cmd
	case "?":
		m.openHelp()
		return m, nil
	}

	// The rest act on the selected note.
	switch msg.String() {
	case "e", "x", "enter", "c", "p", "d":
		if !has {
			m.statusText = "No notes yet. Press a to write one."
			return m, nil
		}
	default:
		return m, nil
	}
	m.errorText, m.statusText = "", ""
	switch msg.String() {
	case "e":
		cmd := m.openNoteEditor(note.ID)
		return m, cmd
	case "x":
		cmd := m.analyzeNote(note)
		return m, cmd
	case "enter", "c":
		switch {
		case note.Claimed():
			m.statusText = fmt.Sprintf("Already claimed: +%d XP on %s.", note.ClaimedXP, note.Analysis.Skill)
		case note.Analysis.Status == "":
			m.statusText = "Analyze the note first (x) to find work to claim."
		case !note.Analysis.Claimable():
			m.statusText = "No finished work on a skill in this note."
		default:
			return m, claimNoteCmd(m.store, note.ID)
		}
		return m, nil
	case "p":
		return m, noteToPlanCmd(m.store, note)
	case "d":
		n.confirmDelete = true
	}
	return m, nil
}

// selectedNote is the note under the cursor. ok is false with no notes.
func (m model) selectedNote() (db.Note, bool) {
	if len(m.notes.list) == 0 {
		return db.Note{}, false
	}
	return m.notes.list[clamp(m.notes.cursor, 0, len(m.notes.list)-1)], true
}

// analyzeNote asks the model to read a note. One note is read at a time.
func (m *model) analyzeNote(note db.Note) tea.Cmd {
	if m.notes.analyzing != 0 {
		m.statusText = "Already analyzing a note."
		return nil
	}
	m.notes.analyzing = note.ID
	return tea.Batch(m.spinner.Tick, analyzeNoteCmd(m.client, m.store, note))
}

// openNoteEditor opens the editor popup on the note with id, or on an empty
// new note when id is 0.
func (m *model) openNoteEditor(id int64) tea.Cmd {
	n := &m.notes
	n.editID = id
	n.editor.Reset()
	if note, ok := m.noteByID(id); ok {
		n.editor.SetValue(note.Body)
	}
	m.sizeNoteEditor()
	m.screen = screenNoteEdit
	m.errorText, m.statusText = "", ""
	return n.editor.Focus()
}

func (m model) handleNoteEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := &m.notes
	switch msg.String() {
	case "esc":
		n.editor.Blur()
		m.screen = screenNotes
		return m, nil
	case "ctrl+s":
		return m.saveNote()
	}
	var cmd tea.Cmd
	n.editor, cmd = n.editor.Update(msg)
	return m, cmd
}

// saveNote checks the editor's text and stores it as a new note or over the
// one being edited.
func (m model) saveNote() (tea.Model, tea.Cmd) {
	n := &m.notes
	body := strings.TrimSpace(n.editor.Value())
	if body == "" {
		m.errorText = "The note is empty."
		return m, nil
	}
	if note, ok := m.noteByID(n.editID); ok && note.Body == body {
		n.editor.Blur()
		m.screen = screenNotes
		m.statusText = "No changes to save."
		return m, nil
	}
	m.errorText = ""
	return m, saveNoteCmd(m.store, n.editID, body)
}

func (m model) noteByID(id int64) (db.Note, bool) {
	for _, note := range m.notes.list {
		if note.ID == id && id != 0 {
			return note, true
		}
	}
	return db.Note{}, false
}

// ─── Commands and messages ───────────────────────────────────────────────────

type notesLoadedMsg struct {
	notes    []db.Note
	dayLogs  map[string]db.DayLog
	selectID int64 // note to put the cursor on, if it is in the list
	err      error
}

type noteSavedMsg struct {
	note    db.Note
	created bool
	err     error
}

type noteDeletedMsg struct {
	id  int64
	err error
}

type noteAnalyzedMsg struct {
	id   int64
	note db.Note
	err  error
}

type noteClaimedMsg struct {
	id    int64
	event db.XPEvent
	err   error
}

type planFromNoteMsg struct {
	plan db.Plan
	err  error
}

func loadNotesCmd(store *db.Store, selectID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		notes, err := store.ListNotes(ctx)
		if err != nil {
			return notesLoadedMsg{err: err}
		}
		logs, err := store.DayLogs(ctx)
		return notesLoadedMsg{notes: notes, dayLogs: logs, selectID: selectID, err: err}
	}
}

// saveNoteCmd creates a note when id is 0 and updates note id otherwise.
func saveNoteCmd(store *db.Store, id int64, body string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if id == 0 {
			note, err := store.CreateNote(ctx, body)
			return noteSavedMsg{note: note, created: true, err: err}
		}
		note, err := store.UpdateNote(ctx, id, body)
		return noteSavedMsg{note: note, err: err}
	}
}

func deleteNoteCmd(store *db.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return noteDeletedMsg{id: id, err: store.DeleteNote(ctx, id)}
	}
}

// analyzeNoteCmd asks the model to read a note against the existing skills
// and stores what it found.
func analyzeNoteCmd(client *llm.Client, store *db.Store, note db.Note) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return noteAnalyzedMsg{id: note.ID, err: errors.New("planner is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		skills, err := store.ListSkills(ctx)
		if err != nil {
			return noteAnalyzedMsg{id: note.ID, err: err}
		}
		names := make([]string, 0, len(skills))
		for _, s := range skills {
			names = append(names, s.Name)
		}
		a, err := rpg.AnalyzeNote(ctx, client, note.Body, names)
		if err != nil {
			return noteAnalyzedMsg{id: note.ID, err: err}
		}
		saved, err := store.SetNoteAnalysis(ctx, note.ID, a)
		return noteAnalyzedMsg{id: note.ID, note: saved, err: err}
	}
}

func claimNoteCmd(store *db.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ev, err := store.ClaimNoteXP(ctx, id)
		return noteClaimedMsg{id: id, event: ev, err: err}
	}
}

// noteToPlanCmd saves a note as a plan: the follow-up task as its subject
// when the analyzer found one, else the note's first line. The plan is a
// to-do, so it earns no XP until its work is logged.
func noteToPlanCmd(store *db.Store, note db.Note) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		subject := note.Analysis.FollowUp
		if subject == "" {
			subject = noteTitle(note.Body)
		}
		subject = truncRunes(subject, subjectLimit)
		desc := "From the field log, " + note.CreatedAt.Local().Format("Mon 2 Jan 2006") + ":\n\n> " +
			strings.ReplaceAll(note.Body, "\n", "\n> ")
		plan, err := store.CreatePlan(ctx, subject, note.Body, desc)
		return planFromNoteMsg{plan: plan, err: err}
	}
}

// handleNotesMsg applies the result of a notes command. ok is false for
// messages that are not about notes.
func (m model) handleNotesMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	n := &m.notes
	switch msg := msg.(type) {
	case notesLoadedMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil, true
		}
		n.list = msg.notes
		n.dayLogs = msg.dayLogs
		n.cursor = clamp(n.cursor, 0, max(len(n.list)-1, 0))
		for i, note := range n.list {
			if note.ID == msg.selectID {
				n.cursor = i
			}
		}
		return m, nil, true

	case noteSavedMsg:
		if msg.err != nil {
			// Keep the editor open so the text is not lost.
			m.errorText = msg.err.Error()
			return m, nil, true
		}
		n.editor.Blur()
		m.screen = screenNotes
		m.errorText = ""
		m.statusText = "Note updated."
		if msg.created {
			m.statusText = "Note saved."
		}
		cmds := []tea.Cmd{loadNotesCmd(m.store, msg.note.ID)}
		// Each saved note is read by the model when one is set up; without
		// one, x reports why.
		if m.client != nil && n.analyzing == 0 {
			m.statusText += " Analyzing…"
			cmds = append(cmds, m.analyzeNote(msg.note))
		}
		return m, tea.Batch(cmds...), true

	case noteDeletedMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil, true
		}
		kept := n.list[:0:0]
		for _, note := range n.list {
			if note.ID != msg.id {
				kept = append(kept, note)
			}
		}
		n.list = kept
		n.cursor = clamp(n.cursor, 0, max(len(kept)-1, 0))
		m.errorText = ""
		m.statusText = "Note deleted."
		// A deleted sick note no longer shields its day.
		return m, loadRoadCmd(m.store), true

	case noteAnalyzedMsg:
		n.analyzing = 0
		if msg.err != nil {
			m.errorText = "Note analysis failed: " + msg.err.Error()
			return m, nil, true
		}
		m.errorText = ""
		m.statusText = analysisStatus(msg.note.Analysis)
		// A shield changes the streak, so the stats reload too.
		return m, tea.Batch(loadNotesCmd(m.store, 0), loadRoadCmd(m.store), loadDailyCmd(m.store)), true

	case noteClaimedMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil, true
		}
		m.errorText = ""
		m.statusText = fmt.Sprintf("+%d XP to %s from the field log", msg.event.XP, msg.event.SkillName)
		return m, tea.Batch(loadNotesCmd(m.store, 0), loadRoadCmd(m.store)), true

	case planFromNoteMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil, true
		}
		m.errorText = ""
		m.statusText = "Plan created: " + msg.plan.Subject
		m.setPlans(append([]db.Plan{msg.plan}, m.plans...))
		return m, nil, true
	}
	return m, nil, false
}

// analysisStatus is the status line after a note is analyzed.
func analysisStatus(a rpg.NoteAnalysis) string {
	switch {
	case a.Shield:
		return "🛡️ " + a.Status + " day logged: the streak is protected and the next set will be light."
	case a.Claimable():
		return fmt.Sprintf("★ Work found: c claims +%d XP on %s.", a.ClaimXP(), a.Skill)
	case a.FollowUp != "":
		return "Follow-up found: p turns it into a plan."
	}
	return "Note analyzed: " + a.Status + "."
}

// ─── Screen ──────────────────────────────────────────────────────────────────

// noteDay names the local day of t: Today, Yesterday, or its date.
func noteDay(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	y, mo, d := t.Date()
	day := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	y, mo, d = now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	switch {
	case day.Equal(today):
		return "Today · " + t.Format("Mon 2 Jan")
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday · " + t.Format("Mon 2 Jan")
	}
	return t.Format("Mon 2 Jan 2006")
}

// noteTitle is the first non-empty line of a note.
func noteTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// signifier is a note's bullet-journal mark: ! for a blocker, ★ for a win,
// • for a reflection, and a dim dot until the note is analyzed.
func signifier(a rpg.NoteAnalysis) string {
	switch rpg.SignifierFor(a) {
	case rpg.SignBlocker:
		ink := colorError
		if a.Status == rpg.StatusBlocked {
			ink = lipgloss.Color(inkBlocker)
		}
		return lipgloss.NewStyle().Foreground(ink).Bold(true).Render("!")
	case rpg.SignWin:
		return stSuccess.Bold(true).Render("★")
	case rpg.SignNote:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(inkIdea)).Render("•")
	}
	return stDim.Render("·")
}

// dayShield returns the status that shielded a day, or "" when no note of
// that day shields it.
func (m model) dayShield(day string) string {
	for _, note := range m.notes.list {
		if note.Day() == day && note.Analysis.Shield {
			return note.Analysis.Status
		}
	}
	return ""
}

// dayBadge is the compact tag next to a day header: the shield, or the XP
// earned that day.
func (m model) dayBadge(day string) string {
	if status := m.dayShield(day); status != "" {
		return stError.Render("[🛡️ " + status + "]")
	}
	if xp := m.notes.dayLogs[day].XP; xp > 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Render(fmt.Sprintf("[🔥 +%d XP]", xp))
	}
	return ""
}

// renderNotesScreen draws the notes grouped by day on the left and the
// selected note with its analysis and day snapshot on the right.
func (m model) renderNotesScreen() string {
	h := m.bodyHeight()
	leftW, rightW := m.columnWidths()
	return lipgloss.JoinHorizontal(lipgloss.Top,
		panel("📓 Field Log", m.renderNoteList(leftW-2, h-2), leftW, h, true),
		panel("📝 Note", m.renderNoteView(rightW-2, h-2), rightW, h, false))
}

// renderNoteList lists the notes under a header for each day, scrolled so
// the selected one is in view.
func (m model) renderNoteList(width, height int) string {
	n := m.notes
	if len(n.list) == 0 {
		return "\n " + stDim.Render("No notes yet.") + "\n " + stChip.Render(" a ") +
			stDim.Render(" writes today's note.")
	}

	now := time.Now()
	var lines []string
	selLine, lastDay := 0, ""
	for i, note := range n.list {
		if day := noteDay(note.CreatedAt, now); day != lastDay {
			if lastDay != "" {
				lines = append(lines, "")
			}
			header := " " + stKey.Render(day)
			if badge := m.dayBadge(note.Day()); badge != "" {
				header += " " + badge
			}
			lines = append(lines, ansi.Truncate(header, width, "…"))
			lastDay = day
		}
		pointer, title := "  ", lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(noteTitle(note.Body))
		if i == n.cursor {
			pointer, title = stActive.Render(" ▸"), stActive.Render(noteTitle(note.Body))
			selLine = len(lines)
		}
		sign := signifier(note.Analysis)
		if note.ID == n.analyzing {
			sign = m.spinner.View()
		}
		row := pointer + " " + sign + " " + stDim.Render(note.CreatedAt.Local().Format("15:04")) + "  " + title
		lines = append(lines, ansi.Truncate(row, width, "…"))
	}

	if len(lines) > height {
		start := clamp(selLine-height/2, 0, len(lines)-height)
		lines = lines[start : start+height]
	}
	return strings.Join(lines, "\n")
}

// statusBadge is a note's status as a filled label, or "" before analysis.
func statusBadge(a rpg.NoteAnalysis) string {
	switch a.Status {
	case "":
		return ""
	case rpg.StatusSick, rpg.StatusExhausted:
		return chipLabel("🛡️ "+a.Status, colorError)
	case rpg.StatusBlocked:
		return chipLabel("! Blocked", lipgloss.Color(inkBlocker))
	case rpg.StatusEnergized:
		return chipLabel("⚡ Energized", colorSuccess)
	}
	return chipLabel("• "+a.Status, lipgloss.Color(inkIdea))
}

// renderNoteView shows the selected note: its date and status, its text,
// the strategist's analysis, and a snapshot of the day it was written. The
// text gives way first when the panel is short.
func (m model) renderNoteView(width, height int) string {
	note, ok := m.selectedNote()
	if !ok {
		return stDim.Render("Notes are dated when you write them. The model reads each\none for your status, work to claim as XP, and follow-ups.")
	}

	header := spread(stActive.Render(note.CreatedAt.Local().Format("Monday, 2 January 2006 · 15:04")),
		statusBadge(note.Analysis), width)
	if note.UpdatedAt.Sub(note.CreatedAt) >= time.Minute {
		header += "\n" + stDim.Render("edited "+formatPlanDate(note.UpdatedAt))
	}

	rule := func(title string) string {
		t := " " + title + " "
		return stSep.Render("──") + stKey.Render(t) + stSep.Render(strings.Repeat("─", max(width-2-lipgloss.Width(t), 0)))
	}
	tail := append([]string{"", rule("🤖 Strategist Analysis")}, m.strategistLines(note, width)...)
	tail = append(tail, "", rule("📊 Day Snapshot · "+note.CreatedAt.Local().Format("2006-01-02")))
	tail = append(tail, m.snapshotLines(note.Day(), width)...)

	headLines := strings.Split(header, "\n")
	room := max(height-len(headLines)-1-len(tail), 1)
	body := strings.Split(lipgloss.NewStyle().Width(width).Render(note.Body), "\n")
	if len(body) > room {
		body = append(body[:room-1], stDim.Render("… e opens the full note"))
	}

	lines := append(append(append(headLines, ""), body...), tail...)
	return strings.Join(lines, "\n")
}

// strategistLines is the analysis of a note: its status and what it means
// for the streak, any XP to claim, and the follow-up task.
func (m model) strategistLines(note db.Note, width int) []string {
	a := note.Analysis
	if note.ID == m.notes.analyzing {
		return []string{" " + m.spinner.View() + " " + stActive.Render("Reading the note…")}
	}
	if a.Status == "" {
		return []string{" " + stDim.Render("Not analyzed yet. ") + stChip.Render(" x ") + stDim.Render(" analyze")}
	}

	var lines []string
	switch a.Status {
	case rpg.StatusSick:
		lines = append(lines, stError.Render(" 🛡️ Illness detected — this day's streak is protected."),
			stDim.Render("    The next day's directives are light recovery tasks."))
	case rpg.StatusExhausted:
		lines = append(lines, stError.Render(" 🛡️ Burnout detected — this day's streak is protected."),
			stDim.Render("    The next day's directives are light recovery tasks."))
	case rpg.StatusBlocked:
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(inkBlocker)).Render(" ! Blocker logged."))
	case rpg.StatusEnergized:
		lines = append(lines, stSuccess.Render(" ⚡ Energized — a good day for a Deep Push."))
	default:
		lines = append(lines, stDim.Render(" • A normal day."))
	}

	switch {
	case note.Claimed():
		lines = append(lines, stSuccess.Render(fmt.Sprintf(" ✓ Claimed +%d XP on %s.", note.ClaimedXP, a.Skill)))
	case a.Claimable():
		lines = append(lines, " "+stSuccess.Render(fmt.Sprintf("★ +%d XP on %s", a.ClaimXP(), a.Skill))+
			stDim.Render(fmt.Sprintf(" (%d min) ", a.Minutes))+stChip.Render(" c ")+stDim.Render(" claim"))
	}
	if a.FollowUp != "" {
		lines = append(lines, " "+stActive.Render("→ "+a.FollowUp))
		lines = append(lines, "   "+stChip.Render(" p ")+stDim.Render(" turns it into a plan"))
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return lines
}

// snapshotLines is what happened on a day: directives, XP and skills.
func (m model) snapshotLines(day string, width int) []string {
	l := m.notes.dayLogs[day]
	row := func(label, value string) string {
		return ansi.Truncate(" "+stDim.Render(fmt.Sprintf("%-11s", label))+value, width, "…")
	}

	directives := stDim.Render("none set")
	if l.DirectivesTotal > 0 {
		directives = stActive.Render(fmt.Sprintf("%d/%d done", l.DirectivesDone, l.DirectivesTotal))
	}
	xp := stDim.Render("0")
	if l.XP > 0 {
		xp = stSuccess.Render(fmt.Sprintf("+%d", l.XP))
	}
	skills := stDim.Render("none practised")
	if len(l.Skills) > 0 {
		skills = strings.Join(l.Skills, ", ")
	}
	lines := []string{row("Directives", directives), row("XP earned", xp), row("Skills", skills)}
	if status := m.dayShield(day); status != "" {
		lines = append(lines, row("Streak", stError.Render("🛡️ shielded ("+status+")")))
	}
	return lines
}

// noteEditorSize returns the outer size of the editor popup.
func (m model) noteEditorSize() (int, int) {
	return clamp(m.width-6, 40, noteEditorMaxW), min(noteEditorMaxH, m.bodyHeight())
}

// sizeNoteEditor fits the text area to the editor popup.
func (m *model) sizeNoteEditor() {
	w, h := m.noteEditorSize()
	// The text sits in a bordered box with one cell of padding, inside the
	// panel's own border.
	m.notes.editor.SetWidth(max(w-6, 10))
	m.notes.editor.SetHeight(max(h-noteEditorExtra, 2))
}

// renderNoteEditor draws the popup for writing or editing a note.
func (m model) renderNoteEditor() string {
	w, h := m.noteEditorSize()
	n := m.notes

	title := "New note · " + time.Now().Format("Mon 2 Jan 2006")
	if note, ok := m.noteByID(n.editID); ok {
		title = "Edit note · " + note.CreatedAt.Local().Format("Mon 2 Jan 2006")
	}
	var b strings.Builder
	b.WriteString(inputBox(n.editor.View(), w-2, true) + "\n")
	b.WriteString(fieldHeader(" enter adds a line", counter(n.editor.Value(), noteLimit)+" ", false, w-2) + "\n")
	b.WriteString(" " + keyChip("ctrl+s", "save") + "  " + keyChip("esc", "cancel"))
	return panel(title, b.String(), w, h, true)
}

// renderNoteDeletePopup asks before a note is deleted.
func (m model) renderNoteDeletePopup() string {
	w := clamp(m.width-6, 30, 50)
	note := m.notes.list[m.notes.cursor]

	var b strings.Builder
	b.WriteString(stError.Render("Delete this note?") + "\n\n")
	b.WriteString(stActive.Render(ansi.Truncate(noteTitle(note.Body), w-4, "…")) + "\n")
	b.WriteString(stDim.Render(formatPlanDate(note.CreatedAt)) + "\n\n")
	b.WriteString(stDim.Render("y delete · any other key cancels"))

	// Lines: title, blank, note, date, blank, hint, plus the two borders.
	return panel("Confirm", b.String(), w, 8, true)
}
