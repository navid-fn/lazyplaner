package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/db"
	"github.com/navid-fn/lazyplaner/llm"
)

// formPanelH is the fixed outer height of the "New Plan" panel:
// 7 content lines + 2 border lines.
const formPanelH = 9

// bottomBarH is the number of lines the bottom bar occupies.
const bottomBarH = 3

var (
	colorActive   = lipgloss.Color("#aaff00")
	colorInactive = lipgloss.Color("238")
	colorDim      = lipgloss.Color("243")
	colorError    = lipgloss.Color("#ff5555")
	colorSuccess  = lipgloss.Color("#50fa7b")
	colorKeyLabel = lipgloss.Color("#f1fa8c")
	colorSep      = lipgloss.Color("237")

	stDim     = lipgloss.NewStyle().Foreground(colorDim)
	stActive  = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
	stError   = lipgloss.NewStyle().Foreground(colorError)
	stSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	stKey     = lipgloss.NewStyle().Foreground(colorKeyLabel).Bold(true)
	stSep     = lipgloss.NewStyle().Foreground(colorSep)
)

// panel draws a rounded box with a title embedded in the top border.
// outerW and outerH are total dimensions including the border itself.
func panel(title, content string, outerW, outerH int, active bool) string {
	bc := colorInactive
	if active {
		bc = colorActive
	}
	bSt := lipgloss.NewStyle().Foreground(bc)
	tSt := lipgloss.NewStyle().Foreground(bc).Bold(true)

	iW := outerW - 2 // inner width (excludes left/right border chars)
	iH := outerH - 2 // inner height (excludes top/bottom border)

	// ╭─ Title ─────────────────────────╮
	ts := " " + title + " "
	dash := iW - 1 - lipgloss.Width(ts)
	if dash < 0 {
		dash = 0
	}
	top := bSt.Render("╭─") + tSt.Render(ts) + bSt.Render(strings.Repeat("─", dash)+"╮")

	// Normalize content to exactly iH lines.
	lines := strings.Split(content, "\n")
	for len(lines) < iH {
		lines = append(lines, "")
	}
	if len(lines) > iH {
		lines = lines[:iH]
	}

	rows := make([]string, iH)
	for i, line := range lines {
		lw := lipgloss.Width(line)
		pad := ""
		if lw < iW {
			pad = strings.Repeat(" ", iW-lw)
		}
		rows[i] = bSt.Render("│") + line + pad + bSt.Render("│")
	}

	bottom := bSt.Render("╰" + strings.Repeat("─", iW) + "╯")

	parts := make([]string, 0, 2+iH)
	parts = append(parts, top)
	parts = append(parts, rows...)
	parts = append(parts, bottom)
	return strings.Join(parts, "\n")
}

// ─── Model ───────────────────────────────────────────────────────────────────

type model struct {
	subjectInput textinput.Model
	detailInput  textinput.Model
	focusIndex   int
	isLoading    bool
	statusText   string
	errorText    string
	plansList    list.Model
	plansByID    map[int64]db.Plan
	plans        []db.Plan
	isEditing    bool
	editTempPath string
	editPlanID   int64
	editOriginal string
	width        int
	height       int
	store        *db.Store
	client       *llm.Client
}

// ─── Message types ────────────────────────────────────────────────────────────

type plansLoadedMsg struct {
	plans []db.Plan
	err   error
}

type planSavedMsg struct {
	plan db.Plan
	err  error
}

type planUpdatedMsg struct {
	plan db.Plan
	err  error
}

type editorFinishedMsg struct {
	err error
}

// ─── List item ───────────────────────────────────────────────────────────────

type planItem struct {
	id      int64
	subject string
}

func (p planItem) Title() string       { return p.subject }
func (p planItem) Description() string { return "" }
func (p planItem) FilterValue() string { return p.subject }

// ─── Constructor ─────────────────────────────────────────────────────────────

func NewModel(store *db.Store, client *llm.Client) model {
	subjectInput := textinput.New()
	subjectInput.Placeholder = "e.g. Learn Go generics"
	subjectInput.Focus()
	subjectInput.CharLimit = 120
	subjectInput.Width = 34

	detailInput := textinput.New()
	detailInput.Placeholder = "Brief context for the AI"
	detailInput.CharLimit = 300
	detailInput.Width = 34

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(colorActive).
		BorderLeftForeground(colorActive)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(colorDim).
		BorderLeftForeground(colorActive)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(lipgloss.Color("252"))

	plansList := list.New([]list.Item{}, delegate, 34, 10)
	plansList.SetShowTitle(false)
	plansList.SetShowStatusBar(false)
	plansList.SetFilteringEnabled(false)
	plansList.SetShowHelp(false)
	plansList.SetShowPagination(false)

	return model{
		subjectInput: subjectInput,
		detailInput:  detailInput,
		plansList:    plansList,
		plansByID:    map[int64]db.Plan{},
		store:        store,
		client:       client,
	}
}

// ─── Init / Update ───────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, loadPlansCmd(m.store))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateSizes()
		return m, nil
	case plansLoadedMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.setPlans(msg.plans)
		return m, nil
	case planSavedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.errorText = ""
		m.statusText = "Plan saved."
		m.setPlans(append([]db.Plan{msg.plan}, m.plans...))
		m.subjectInput.SetValue("")
		m.detailInput.SetValue("")
		m.focusIndex = 0
		m.subjectInput.Focus()
		m.detailInput.Blur()
		return m, nil
	case planUpdatedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.errorText = ""
		m.statusText = "Description updated."
		m.updatePlanInList(msg.plan)
		return m, nil
	case editorFinishedMsg:
		return m.handleEditorFinished(msg)
	}
	return m, nil
}

// ─── Key / mouse handling ────────────────────────────────────────────────────

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isEditing {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab", "shift+tab":
		if !m.isLoading {
			m.updateFocus(msg.String())
		}
		return m, nil
	case "enter":
		if m.focusIndex == 2 && !m.isLoading {
			return m.handleSubmit()
		}
	case "e":
		if m.focusIndex == 3 {
			return m.handleEditStart()
		}
	}

	var cmd tea.Cmd
	switch m.focusIndex {
	case 0:
		m.subjectInput, cmd = m.subjectInput.Update(msg)
	case 1:
		m.detailInput, cmd = m.detailInput.Update(msg)
	case 3:
		m.plansList, cmd = m.plansList.Update(msg)
	}
	return m, cmd
}

func (m model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.isLoading || m.isEditing {
		return m, nil
	}
	m.focusIndex = 3
	m.subjectInput.Blur()
	m.detailInput.Blur()
	var cmd tea.Cmd
	m.plansList, cmd = m.plansList.Update(msg)
	return m, cmd
}

func (m *model) updateFocus(key string) {
	if key == "shift+tab" {
		m.focusIndex--
	} else {
		m.focusIndex++
	}
	if m.focusIndex > 3 {
		m.focusIndex = 0
	}
	if m.focusIndex < 0 {
		m.focusIndex = 3
	}
	m.subjectInput.Blur()
	m.detailInput.Blur()
	switch m.focusIndex {
	case 0:
		m.subjectInput.Focus()
	case 1:
		m.detailInput.Focus()
	}
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading…"
	}

	leftW, rightW := m.columnWidths()
	totalH := m.height - bottomBarH

	listPanelH := totalH - formPanelH
	if listPanelH < 4 {
		listPanelH = 4
	}
	descPanelH := totalH

	formActive := m.focusIndex <= 2
	listActive := m.focusIndex == 3

	formPanel := panel("New Plan", m.renderFormContent(), leftW, formPanelH, formActive)
	listPanel := panel("Plans", m.plansList.View(), leftW, listPanelH, listActive)
	descPanel := panel("Description", m.renderDescContent(rightW-2), rightW, descPanelH, m.isEditing)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, formPanel, listPanel)
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, descPanel)

	return mainArea + "\n" + m.renderBottomBar()
}

func (m model) renderFormContent() string {
	subLabel := stDim.Render("Subject")
	if m.focusIndex == 0 {
		subLabel = stActive.Render("Subject")
	}

	detLabel := stDim.Render("Detail")
	if m.focusIndex == 1 {
		detLabel = stActive.Render("Detail")
	}

	var submit string
	switch {
	case m.isLoading:
		submit = stActive.Render("⠋ Generating…")
	case m.focusIndex == 2:
		submit = lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(colorActive).
			Padding(0, 1).
			Bold(true).
			Render("[ Submit ]")
	default:
		submit = stDim.Render("[ Submit ]")
	}

	var b strings.Builder
	b.WriteString(subLabel + "\n")
	b.WriteString(m.subjectInput.View() + "\n")
	b.WriteString("\n")
	b.WriteString(detLabel + "\n")
	b.WriteString(m.detailInput.View() + "\n")
	b.WriteString("\n")
	b.WriteString(submit)
	return b.String()
}

func (m model) renderDescContent(innerW int) string {
	if m.isEditing {
		return stActive.Render("Editing in external editor…") + "\n\n" +
			stDim.Render("Save and close the editor to return.")
	}

	plan, ok := m.currentPlan()
	if !ok {
		if len(m.plansByID) == 0 {
			return stDim.Render("No plans yet.\n\nFill in the form and hit Submit to generate your first plan.")
		}
		return stDim.Render("No plan selected.")
	}

	desc := strings.TrimSpace(plan.Description)
	if desc == "" {
		return stDim.Render("No description available.")
	}

	// Soft-wrap long lines to fit the panel.
	return lipgloss.NewStyle().Width(innerW).Render(desc)
}

func (m model) renderBottomBar() string {
	var statusLine string
	switch {
	case m.errorText != "":
		statusLine = stError.Render("✗ " + m.errorText)
	case m.statusText != "":
		statusLine = stSuccess.Render("✓ " + m.statusText)
	}

	type kd struct{ k, d string }
	pairs := []kd{
		{"tab", "next panel"},
		{"↑↓", "navigate"},
		{"enter", "submit"},
		{"e", "edit plan"},
		{"q", "quit"},
	}
	sep := stSep.Render("  │  ")
	var keyParts []string
	for _, p := range pairs {
		keyParts = append(keyParts, stKey.Render(p.k)+stDim.Render(" "+p.d))
	}
	keysLine := "  " + strings.Join(keyParts, sep)

	separator := stSep.Render(strings.Repeat("─", m.width))

	return statusLine + "\n" + separator + "\n" + keysLine
}

// ─── Submit / edit ───────────────────────────────────────────────────────────

func (m model) handleSubmit() (tea.Model, tea.Cmd) {
	subject := strings.TrimSpace(m.subjectInput.Value())
	detail := strings.TrimSpace(m.detailInput.Value())
	if subject == "" || detail == "" {
		m.errorText = "Subject and detail are required."
		return m, nil
	}
	if m.client == nil || m.store == nil {
		m.errorText = "Planner is not configured."
		return m, nil
	}
	m.isLoading = true
	m.statusText = ""
	m.errorText = ""
	return m, generatePlanCmd(m.client, m.store, subject, detail)
}

func (m model) handleEditStart() (tea.Model, tea.Cmd) {
	if m.isLoading || m.isEditing {
		return m, nil
	}
	plan, ok := m.currentPlan()
	if !ok {
		m.errorText = "Select a plan to edit."
		return m, nil
	}

	editorCommand := strings.TrimSpace(os.Getenv("VISUAL"))
	if editorCommand == "" {
		editorCommand = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editorCommand == "" {
		editorCommand = "nano"
	}

	tempFile, err := os.CreateTemp("", "lazyplaner-edit-*.md")
	if err != nil {
		m.errorText = fmt.Sprintf("Failed to open editor: %v", err)
		return m, nil
	}
	if _, err := tempFile.WriteString(plan.Description); err != nil {
		m.errorText = fmt.Sprintf("Failed to prepare editor: %v", err)
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return m, nil
	}
	if err := tempFile.Close(); err != nil {
		m.errorText = fmt.Sprintf("Failed to prepare editor: %v", err)
		_ = os.Remove(tempFile.Name())
		return m, nil
	}

	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %s", editorCommand, shellQuote(tempFile.Name())))
	m.isEditing = true
	m.errorText = ""
	m.statusText = ""
	m.editTempPath = tempFile.Name()
	m.editPlanID = plan.ID
	m.editOriginal = plan.Description

	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m model) handleEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	m.isEditing = false
	defer m.resetEditorState()

	if msg.err != nil {
		m.errorText = fmt.Sprintf("Editor error: %v", msg.err)
		return m, nil
	}
	updatedDescription, err := m.readEditedDescription()
	if err != nil {
		m.errorText = err.Error()
		return m, nil
	}
	trimmed := strings.TrimSpace(updatedDescription)
	if trimmed == "" {
		m.errorText = "Description is required."
		return m, nil
	}
	if strings.TrimSpace(m.editOriginal) == trimmed {
		m.statusText = "No changes to save."
		return m, nil
	}
	if m.store == nil {
		m.errorText = "Planner is not configured."
		return m, nil
	}
	m.isLoading = true
	m.errorText = ""
	m.statusText = ""
	return m, updateDescriptionCmd(m.store, m.editPlanID, trimmed)
}

func (m model) readEditedDescription() (string, error) {
	if m.editTempPath == "" {
		return "", fmt.Errorf("missing editor file")
	}
	content, err := os.ReadFile(m.editTempPath)
	if err != nil {
		return "", fmt.Errorf("failed to read editor content: %w", err)
	}
	return string(content), nil
}

func (m *model) resetEditorState() {
	if m.editTempPath != "" {
		_ = os.Remove(m.editTempPath)
	}
	m.editTempPath = ""
	m.editPlanID = 0
	m.editOriginal = ""
}

// ─── Layout helpers ───────────────────────────────────────────────────────────

func (m model) columnWidths() (int, int) {
	if m.width == 0 {
		return 38, 62
	}
	left := m.width / 3
	if left < 30 {
		left = 30
	}
	if left > 45 {
		left = 45
	}
	return left, m.width - left
}

func (m *model) updateSizes() {
	leftW, _ := m.columnWidths()

	// Resize text inputs to fit the form panel.
	inputW := leftW - 4
	if inputW < 15 {
		inputW = 15
	}
	m.subjectInput.Width = inputW
	m.detailInput.Width = inputW

	// Resize the plans list to fit the list panel.
	listPanelH := m.height - bottomBarH - formPanelH
	if listPanelH < 4 {
		listPanelH = 4
	}
	listInnerW := leftW - 2
	listInnerH := listPanelH - 2
	if listInnerH < 2 {
		listInnerH = 2
	}
	m.plansList.SetSize(listInnerW, listInnerH)
}

// ─── Data helpers ─────────────────────────────────────────────────────────────

func (m *model) setPlans(plans []db.Plan) {
	items := make([]list.Item, 0, len(plans))
	m.plansByID = map[int64]db.Plan{}
	m.plans = plans
	for _, plan := range plans {
		m.plansByID[plan.ID] = plan
		items = append(items, planItem{id: plan.ID, subject: plan.Subject})
	}
	m.plansList.SetItems(items)
	if len(items) > 0 {
		m.plansList.Select(0)
	}
}

func (m *model) updatePlanInList(plan db.Plan) {
	for i, p := range m.plans {
		if p.ID == plan.ID {
			m.plans[i] = plan
			break
		}
	}
	m.plansByID[plan.ID] = plan
}

func (m model) currentPlan() (db.Plan, bool) {
	item := m.plansList.SelectedItem()
	if item == nil {
		return db.Plan{}, false
	}
	pi, ok := item.(planItem)
	if !ok {
		return db.Plan{}, false
	}
	plan, ok := m.plansByID[pi.id]
	return plan, ok
}

// ─── Commands ─────────────────────────────────────────────────────────────────

func loadPlansCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		plans, err := store.ListPlans(ctx)
		return plansLoadedMsg{plans: plans, err: err}
	}
}

func generatePlanCmd(client *llm.Client, store *db.Store, subject, detail string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		description, err := client.GenerateDescription(ctx, subject, detail)
		if err != nil {
			return planSavedMsg{err: err}
		}
		plan, err := store.CreatePlan(ctx, subject, detail, description)
		return planSavedMsg{plan: plan, err: err}
	}
}

func updateDescriptionCmd(store *db.Store, id int64, description string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		plan, err := store.UpdatePlanDescription(ctx, id, description)
		return planUpdatedMsg{plan: plan, err: err}
	}
}

// ─── Misc ─────────────────────────────────────────────────────────────────────

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
