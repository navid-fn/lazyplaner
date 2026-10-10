// Package ui implements the Bubble Tea terminal interface.
package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/llm"
)

// Focus indices for the panels that can receive keyboard focus.
// Enter submits the form directly from Subject/Detail, so there is no
// separate focus stop for the submit button.
const (
	focusSubject = iota
	focusDetail
	focusModels
	focusPlans
)

// ─── Model ───────────────────────────────────────────────────────────────────

type model struct {
	subjectInput  textinput.Model
	detailInput   textinput.Model
	focusIndex    int
	isLoading     bool
	statusText    string
	errorText     string
	plansList     list.Model
	plansByID     map[int64]db.Plan
	plans         []db.Plan
	modelsList    list.Model
	models        []string
	selectedModel string
	isEditing     bool
	editTempPath  string
	editPlanID    int64
	editOriginal  string
	width         int
	height        int
	store         *db.Store
	client        *llm.Client
}

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
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	plansList := list.New([]list.Item{}, delegate, 34, 10)
	plansList.SetShowTitle(false)
	plansList.SetShowStatusBar(false)
	plansList.SetFilteringEnabled(false)
	plansList.SetShowHelp(false)
	plansList.SetShowPagination(false)

	modelsDelegate := list.NewDefaultDelegate()
	modelsDelegate.Styles.SelectedTitle = modelsDelegate.Styles.SelectedTitle.
		Foreground(colorActive).
		BorderLeftForeground(colorActive)
	modelsDelegate.Styles.SelectedDesc = modelsDelegate.Styles.SelectedDesc.
		Foreground(colorDim).
		BorderLeftForeground(colorActive)
	modelsDelegate.Styles.NormalTitle = modelsDelegate.Styles.NormalTitle.Foreground(lipgloss.Color("252"))
	modelsDelegate.ShowDescription = false
	modelsDelegate.SetSpacing(0)

	modelsList := list.New([]list.Item{}, modelsDelegate, 34, 5)
	modelsList.SetShowTitle(false)
	modelsList.SetShowStatusBar(false)
	modelsList.SetFilteringEnabled(false)
	modelsList.SetShowHelp(false)
	modelsList.SetShowPagination(false)

	var selectedModel string
	if client != nil {
		selectedModel = client.Model()
	}

	return model{
		subjectInput:  subjectInput,
		detailInput:   detailInput,
		plansList:     plansList,
		plansByID:     map[int64]db.Plan{},
		modelsList:    modelsList,
		selectedModel: selectedModel,
		store:         store,
		client:        client,
	}
}

// ─── Init / Update ───────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, loadPlansCmd(m.store), loadModelsCmd(m.client))
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
	case modelsLoadedMsg:
		if msg.err != nil {
			m.errorText = fmt.Sprintf("Failed to load models: %v", msg.err)
			return m, nil
		}
		m.setModels(msg.models)
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
		m.setFocus(focusSubject)
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
