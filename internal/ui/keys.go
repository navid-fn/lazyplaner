package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.isEditing {
		return m, nil
	}
	if m.proof != nil {
		return m.handleProofKey(msg)
	}
	// ctrl+o picks the model from anywhere; the picker handles it itself
	// (it closes the picker).
	if msg.String() == "ctrl+o" && m.screen != screenModels {
		return m.openModels()
	}
	switch m.screen {
	case screenNewPlan:
		return m.handleNewPlanKey(msg)
	case screenModels:
		return m.handleModelsKey(msg)
	case screenRoad:
		return m.handleRoadKey(msg)
	case screenOnboard:
		return m.handleOnboardKey(msg)
	case screenTree:
		return m.handleTreeKey(msg)
	case screenHelp:
		return m.handleHelpKey(msg)
	case screenSkillEdit:
		return m.handleSkillEditKey(msg)
	case screenNotes:
		return m.handleNotesKey(msg)
	case screenPlanView:
		return m.handlePlanViewKey(msg)
	case screenNoteEdit:
		return m.handleNoteEditKey(msg)
	}
	return m.handleMainKey(msg)
}

func (m model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmDelete != nil {
		return m.handleDeleteConfirmKey(msg)
	}
	if next, cmd, handled := m.handleHomeKey(msg); handled {
		return next, cmd
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "d":
		if m.isLoading {
			return m, nil
		}
		plan, ok := m.currentPlan()
		if !ok {
			m.errorText = "Select a plan to delete."
			return m, nil
		}
		m.confirmDelete = &plan
		m.errorText = ""
		m.statusText = ""
		return m, nil
	case "ctrl+n":
		cmd := m.openNewPlan()
		return m, cmd
	case "c":
		cmd := m.openRoad()
		return m, cmd
	case "t":
		m.openTree()
		return m, nil
	case "n":
		cmd := m.openNotes()
		return m, cmd
	case "g":
		cmd := m.startDailyJob()
		return m, cmd
	case "s":
		if m.isLoading {
			return m, nil
		}
		cmd := m.openOnboard()
		return m, cmd
	case "h":
		m.openHelp()
		return m, nil
	case "e":
		if !m.isLoading {
			return m.handleEditStart()
		}
	case "enter":
		// enter on a plan opens the reader; while the list is being
		// filtered, enter belongs to the filter.
		if m.mainFocus == focusPlans && m.plansList.FilterState() != list.Filtering {
			m.openPlanView()
			return m, nil
		}
	}

	// Only the plan list moves with the remaining keys, and only when it
	// has focus.
	if m.mainFocus != focusPlans {
		return m, nil
	}
	var cmd tea.Cmd
	m.plansList, cmd = m.plansList.Update(msg)
	return m, cmd
}

// handleDeleteConfirmKey answers the delete prompt: y deletes, anything else
// cancels.
func (m model) handleDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	plan := *m.confirmDelete
	m.confirmDelete = nil
	if msg.String() != "y" {
		m.statusText = "Delete cancelled."
		return m, nil
	}
	m.isLoading = true
	m.loadingText = fmt.Sprintf("Deleting %q…", plan.Subject)
	m.statusText = ""
	return m, tea.Batch(m.spinner.Tick, deletePlanCmd(m.store, plan.ID))
}

func (m model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.screen != screenMain || m.isEditing || m.mainFocus != focusPlans {
		return m, nil
	}
	var cmd tea.Cmd
	m.plansList, cmd = m.plansList.Update(msg)
	return m, cmd
}
