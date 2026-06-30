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

var (
	accent      = lipgloss.Color("205")
	subtle      = lipgloss.Color("241")
	errorColor  = lipgloss.Color("196")
	successColor = lipgloss.Color("82")
	textColor   = lipgloss.Color("252")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(subtle).
			PaddingBottom(1).
			MarginBottom(1)

	labelFocused = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)

	labelBlurred = lipgloss.NewStyle().
			Foreground(subtle)

	submitFocused = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(accent).
			Padding(0, 2)

	submitBlurred = lipgloss.NewStyle().
			Foreground(subtle).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtle).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(subtle).
			MarginTop(1)

	loadingStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)

	panelFocused = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 1)

	panelBlurred = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtle).
			Padding(0, 1)
)

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

type planItem struct {
	id      int64
	subject string
}

func (p planItem) Title() string {
	return p.subject
}

func (p planItem) Description() string {
	return ""
}

func (p planItem) FilterValue() string {
	return p.subject
}

func NewModel(store *db.Store, client *llm.Client) model {
	subjectInput := textinput.New()
	subjectInput.Placeholder = "e.g. Learn Go generics"
	subjectInput.Focus()
	subjectInput.CharLimit = 120
	subjectInput.Width = 50
	subjectInput.PromptStyle = labelFocused
	subjectInput.TextStyle = lipgloss.NewStyle().Foreground(textColor)

	detailInput := textinput.New()
	detailInput.Placeholder = "e.g. I know basic Go, want to understand type constraints"
	detailInput.CharLimit = 300
	detailInput.Width = 50

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(accent).
		BorderLeftForeground(accent)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("243")).
		BorderLeftForeground(accent)

	plansList := list.New([]list.Item{}, delegate, 30, 10)
	plansList.Title = "Plans"
	plansList.Styles.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(accent).
		MarginLeft(1)
	plansList.SetShowStatusBar(false)
	plansList.SetFilteringEnabled(false)
	plansList.SetShowHelp(false)
	plansList.SetShowPagination(false)

	return model{
		subjectInput: subjectInput,
		detailInput:  detailInput,
		focusIndex:   0,
		plansList:    plansList,
		plansByID:    map[int64]db.Plan{},
		store:        store,
		client:       client,
	}
}

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
		m.updateListSize()
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

func (m model) View() string {
	var builder strings.Builder

	builder.WriteString(titleStyle.Render("  LazyPlaner"))
	builder.WriteString("\n")
	builder.WriteString(m.renderForm())
	builder.WriteString("\n\n")
	builder.WriteString(m.renderWindows())

	if m.statusText != "" {
		builder.WriteString("\n")
		builder.WriteString(statusStyle.Render("✓ " + m.statusText))
	}
	if m.errorText != "" {
		builder.WriteString("\n")
		builder.WriteString(errorStyle.Render("✗ " + m.errorText))
	}

	builder.WriteString("\n")
	builder.WriteString(helpStyle.Render("Tab/Shift+Tab · navigate   Enter · submit   e · edit plan   q · quit"))
	return builder.String()
}

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isEditing {
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "tab", "shift+tab":
		if m.isLoading {
			return m, nil
		}
		m.updateFocus(msg.String())
		return m, nil
	case "enter":
		if m.focusIndex == 2 && !m.isLoading {
			return m.handleSubmit()
		}
	case "e":
		if m.focusIndex != 3 {
			break
		}
		return m.handleEditStart()
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
	if m.isLoading {
		return m, nil
	}
	if m.isEditing {
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
	if key == "up" || key == "shift+tab" {
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
	if m.isLoading {
		return m, nil
	}
	if m.isEditing {
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

	shellCommand := fmt.Sprintf("%s %s", editorCommand, shellQuote(tempFile.Name()))
	command := exec.Command("sh", "-c", shellCommand)

	m.isEditing = true
	m.errorText = ""
	m.statusText = ""
	m.editTempPath = tempFile.Name()
	m.editPlanID = plan.ID
	m.editOriginal = plan.Description

	return m, tea.ExecProcess(command, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m model) renderForm() string {
	var builder strings.Builder

	subjectLabel := labelBlurred.Render("Subject")
	detailLabel := labelBlurred.Render("Detail")
	if m.focusIndex == 0 {
		subjectLabel = labelFocused.Render("Subject")
	}
	if m.focusIndex == 1 {
		detailLabel = labelFocused.Render("Detail")
	}

	var submitBtn string
	switch {
	case m.isLoading:
		submitBtn = loadingStyle.Render("⠋ Generating plan…")
	case m.focusIndex == 2:
		submitBtn = submitFocused.Render("Submit")
	default:
		submitBtn = submitBlurred.Render("Submit")
	}

	builder.WriteString(subjectLabel)
	builder.WriteString("\n")
	builder.WriteString(m.subjectInput.View())
	builder.WriteString("\n\n")
	builder.WriteString(detailLabel)
	builder.WriteString("\n")
	builder.WriteString(m.detailInput.View())
	builder.WriteString("\n\n")
	builder.WriteString(submitBtn)
	return builder.String()
}

func (m model) renderWindows() string {
	planWidth, detailWidth := m.windowSizes()

	plansStyle := panelBlurred.Width(planWidth)
	detailStyle := panelBlurred.Width(detailWidth)

	if m.focusIndex == 3 {
		plansStyle = panelFocused.Width(planWidth)
	}
	if m.isEditing {
		detailStyle = panelFocused.Width(detailWidth)
	}

	plansWindow := plansStyle.Render(m.plansList.View())
	detailWindow := detailStyle.Render(m.renderDetailWindow())
	return lipgloss.JoinHorizontal(lipgloss.Top, plansWindow, detailWindow)
}

func (m model) renderDetailWindow() string {
	if m.isEditing {
		return lipgloss.NewStyle().Foreground(accent).Bold(true).Render("Editing in external editor…") +
			"\n\n" +
			lipgloss.NewStyle().Foreground(subtle).Render("Return here when you save and close the editor.")
	}
	return m.currentPlanDescription()
}

func (m model) currentPlanDescription() string {
	plan, ok := m.currentPlan()
	if !ok {
		if len(m.plansByID) == 0 {
			return lipgloss.NewStyle().Foreground(subtle).Render("No plans yet. Create one above.")
		}
		return lipgloss.NewStyle().Foreground(subtle).Render("No plan selected.")
	}

	if strings.TrimSpace(plan.Description) == "" {
		return lipgloss.NewStyle().Foreground(subtle).Render("No description available.")
	}

	return plan.Description
}

func (m model) currentPlan() (db.Plan, bool) {
	item := m.plansList.SelectedItem()
	if item == nil {
		return db.Plan{}, false
	}
	planItemValue, ok := item.(planItem)
	if !ok {
		return db.Plan{}, false
	}
	plan, ok := m.plansByID[planItemValue.id]
	if !ok {
		return db.Plan{}, false
	}
	return plan, true
}

func (m model) windowSizes() (int, int) {
	defaultPlanWidth := 32
	defaultDetailWidth := 60
	if m.width == 0 {
		return defaultPlanWidth, defaultDetailWidth
	}

	planWidth := m.width / 3
	if planWidth < 28 {
		planWidth = 28
	}
	if planWidth > 40 {
		planWidth = 40
	}
	detailWidth := m.width - planWidth - 6
	if detailWidth < 30 {
		detailWidth = 30
	}
	return planWidth, detailWidth
}

func (m *model) updateListSize() {
	planWidth, _ := m.windowSizes()
	listHeight := 10
	if m.height > 0 {
		listHeight = m.height / 2
		if listHeight < 8 {
			listHeight = 8
		}
	}
	m.plansList.SetSize(planWidth-2, listHeight-2)
}

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
	m.updateListSize()
}

func (m *model) updatePlanInList(plan db.Plan) {
	for index, existingPlan := range m.plans {
		if existingPlan.ID != plan.ID {
			continue
		}
		m.plans[index] = plan
		break
	}
	m.plansByID[plan.ID] = plan
}

func loadPlansCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		plans, err := store.ListPlans(ctx)
		return plansLoadedMsg{plans: plans, err: err}
	}
}

func generatePlanCmd(client *llm.Client, store *db.Store, subject string, detail string) tea.Cmd {
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

	trimmedDescription := strings.TrimSpace(updatedDescription)
	if trimmedDescription == "" {
		m.errorText = "Description is required."
		return m, nil
	}
	if strings.TrimSpace(m.editOriginal) == trimmedDescription {
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
	return m, updateDescriptionCmd(m.store, m.editPlanID, trimmedDescription)
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
