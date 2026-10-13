package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.isEditing {
		return m, nil
	}
	switch m.screen {
	case screenNewPlan:
		return m.handleNewPlanKey(msg)
	case screenModels:
		return m.handleModelsKey(msg)
	}
	return m.handleMainKey(msg)
}

func (m model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmDelete != nil {
		return m.handleDeleteConfirmKey(msg)
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
	case "ctrl+o":
		return m.openModels()
	case "e":
		if !m.isLoading {
			return m.handleEditStart()
		}
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
	if m.screen != screenMain || m.isEditing {
		return m, nil
	}
	var cmd tea.Cmd
	m.plansList, cmd = m.plansList.Update(msg)
	return m, cmd
}
