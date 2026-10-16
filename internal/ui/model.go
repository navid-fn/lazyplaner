// Package ui implements the Bubble Tea terminal interface.
package ui

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/game"
	"github.com/navid-fn/lazyplaner/internal/llm"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

// screen is the view that receives keyboard input. The plan list and preview
// are always drawn; the other screens are centered on top of them.
type screen int

const (
	screenMain screen = iota
	screenNewPlan
	screenModels
	screenRoad
	screenOnboard
	screenTree
	screenHelp
	screenSkillEdit
	screenNotes
	screenNoteEdit
	screenPlanView // the selected plan's markdown, full screen
)

// newPlanForm holds the inputs of the New Plan window.
type newPlanForm struct {
	subject     textinput.Model
	description textarea.Model
	descFocused bool
}

// ─── Model ───────────────────────────────────────────────────────────────────

type model struct {
	screen          screen
	prevScreen      screen // screen under help, the skill editor or New Plan
	modelsReturn    screen // screen the model picker was opened from
	newPlan         newPlanForm
	spinner         spinner.Model
	isLoading       bool
	loadingText     string
	statusText      string
	errorText       string
	confirmDelete   *db.Plan
	plansList       list.Model
	plansByID       map[int64]db.Plan
	plans           []db.Plan
	skills          []db.Skill
	nodes           []db.Node
	eras            []db.Era
	proof           *proofForm // the open proof-of-work popup, if any
	planView        planViewer // the plan reader (screenPlanView)
	xpEvents        []db.XPEvent
	challenges      []db.Challenge
	challengeStats  db.ChallengeStats
	challengeCursor int
	treeSkill       int
	treeNode        int
	charCursor      int
	mainFocus       homeFocus // which left panel on the home screen has keys
	treeReturn      screen    // where leaving the tree goes back to
	onboard         onboardForm
	notes           notesState
	skillEdit       skillEditForm
	helpTab         int
	helpScroll      int    // first line of the help tab shown
	dailyBusy       string // set while the model works on today's directives
	onboardChecked  bool
	modelsList      list.Model
	models          []string
	selectedModel   string
	isEditing       bool
	editTempPath    string
	editPlanID      int64
	editOriginal    string
	width           int
	height          int
	store           *db.Store
	client          *llm.Client
}

// ─── Constructor ─────────────────────────────────────────────────────────────

func NewModel(store *db.Store, client *llm.Client) model {
	subjectInput := textinput.New()
	subjectInput.Placeholder = "e.g. Learn Go generics"
	subjectInput.CharLimit = 120

	descInput := textarea.New()
	descInput.Placeholder = "Brief context for the AI"
	descInput.ShowLineNumbers = false
	descInput.Prompt = ""
	descInput.CharLimit = 2000
	descInput.SetHeight(newPlanTextH)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorActive)

	plansList := list.New([]list.Item{}, newListDelegate(), 0, 0)
	plansList.SetShowTitle(false)
	plansList.SetShowStatusBar(false)
	plansList.SetFilteringEnabled(false)
	plansList.SetShowHelp(false)
	plansList.SetShowPagination(false)

	modelsDelegate := newListDelegate()
	modelsDelegate.ShowDescription = false
	modelsList := list.New([]list.Item{}, modelsDelegate, 0, 0)
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
		spinner:       sp,
		newPlan:       newPlanForm{subject: subjectInput, description: descInput},
		notes:         notesState{editor: newNoteEditor()},
		plansList:     plansList,
		plansByID:     map[int64]db.Plan{},
		modelsList:    modelsList,
		selectedModel: selectedModel,
		store:         store,
		client:        client,
	}
}

// newListDelegate returns the shared list styling: active row in green,
// created date shown under each plan subject.
func newListDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(colorActive).
		BorderLeftForeground(colorActive)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(colorDim).
		BorderLeftForeground(colorActive)
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(lipgloss.Color("252"))
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(colorDim)
	d.SetSpacing(0)
	return d
}

// ─── Init / Update ───────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(loadPlansCmd(m.store), loadModelsCmd(m.client), loadRoadCmd(m.store),
		loadDailyCmd(m.store), loadNotesCmd(m.store, 0))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleNotesMsg(msg); ok {
		return next, cmd
	}
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
	case spinner.TickMsg:
		// Stop the animation once nothing is running. A skill audit on the
		// setup screen, a directive job and a note analysis run without
		// blocking the screen, so they keep the spinner too.
		if !m.isLoading && !m.onboard.busy() && m.dailyBusy == "" && m.notes.analyzing == 0 {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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
		cmd := m.ensureInstalledModel()
		return m, cmd
	case modelSavedMsg:
		if msg.err != nil {
			m.errorText = fmt.Sprintf("Failed to save the model choice: %v", msg.err)
		}
		return m, nil
	case planSavedMsg:
		m.isLoading = false
		if msg.err != nil {
			// Keep the form open so the user can fix the input and retry.
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.errorText = ""
		m.statusText = planSavedStatus(msg)
		m.newPlan.subject.Reset()
		m.newPlan.description.Reset()
		m.closeNewPlan()
		m.setPlans(append([]db.Plan{msg.plan}, m.plans...))
		// Reload the character so the new plan's badge and the briefing
		// show the XP it just earned.
		return m, loadRoadCmd(m.store)
	case planUpdatedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.errorText = ""
		m.statusText = "Description updated."
		m.updatePlanInList(msg.plan)
		if m.screen == screenPlanView && m.planView.plan.ID == msg.plan.ID {
			m.planView.plan = msg.plan
			m.layoutPlanView()
		}
		return m, nil
	case planDeletedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.errorText = ""
		m.statusText = "Plan deleted."
		m.removePlan(msg.id)
		return m, nil
	case roadLoadedMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.setRoad(msg.skills, msg.nodes, msg.events)
		m.eras = msg.eras
		m.challengeStats = msg.stats
		m.refreshPlanItems()
		// Setup opens on the first load when there is no character yet.
		if !m.onboardChecked {
			m.onboardChecked = true
			if len(msg.skills) == 0 {
				cmd := m.openOnboard()
				return m, cmd
			}
		}
		return m, nil
	case skillUpdatedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.screen = screenTree
		m.errorText = ""
		m.statusText = "Saved " + msg.name + ". It is back at level 1."
		return m, loadRoadCmd(m.store)
	case auditDoneMsg:
		cmd := m.applyAudit(msg)
		return m, cmd
	case categoryDoneMsg:
		m.applyCategory(msg)
		return m, nil
	case treeBuiltMsg:
		return m.handleTreeBuilt(msg)
	case stanceMsg:
		if msg.err != nil {
			m.errorText = msg.err.Error()
			if errors.Is(msg.err, db.ErrActiveCap) {
				m.errorText = fmt.Sprintf("%d skills are active. Press f on one to freeze it, then activate %s.",
					rpg.MaxActiveSkills, msg.name)
			}
			return m, nil
		}
		m.statusText = msg.name + " moved to the garrison: frozen, no directives, no rust."
		if msg.active {
			m.statusText = msg.name + " is now an active front."
		}
		return m, loadRoadCmd(m.store)
	case eraExpandedMsg:
		m.dailyBusy = ""
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.errorText = ""
		m.statusText = fmt.Sprintf("Era %s of %s is open. Its foundations grow from the last capstone.",
			game.RomanEra(msg.era), msg.skill)
		return m, loadRoadCmd(m.store)
	case challengesLoadedMsg:
		m.dailyBusy = ""
		// A failed job still carries today's list as it stands.
		if msg.challenges != nil || msg.err == nil {
			m.challenges = msg.challenges
			m.challengeStats = msg.stats
			m.challengeCursor = clamp(m.challengeCursor, 0, max(len(msg.challenges)-1, 0))
		}
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		if msg.note != "" {
			m.errorText = ""
			m.statusText = msg.note
		}
		return m, nil
	case challengeDoneMsg:
		m.isLoading = false
		if msg.err != nil {
			m.errorText = msg.err.Error()
			return m, nil
		}
		m.statusText = fmt.Sprintf("+%d XP to %s", msg.event.XP, msg.event.SkillName)
		if msg.event.NodeName != "" {
			m.statusText += " › " + msg.event.NodeName
		}
		m.statusText += comebackNote(msg.event)
		return m, tea.Batch(loadDailyCmd(m.store), loadRoadCmd(m.store))
	case editorFinishedMsg:
		return m.handleEditorFinished(msg)
	}
	return m, nil
}
