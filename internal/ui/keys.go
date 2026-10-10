package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

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
		switch {
		case (m.focusIndex == focusSubject || m.focusIndex == focusDetail) && !m.isLoading:
			return m.handleSubmit()
		case m.focusIndex == focusModels && !m.isLoading:
			return m.handleSelectModel()
		}
	case "e":
		if m.focusIndex == focusPlans {
			return m.handleEditStart()
		}
	case "r":
		if m.focusIndex == focusModels && !m.isLoading {
			return m, loadModelsCmd(m.client)
		}
	}

	var cmd tea.Cmd
	switch m.focusIndex {
	case focusSubject:
		m.subjectInput, cmd = m.subjectInput.Update(msg)
	case focusDetail:
		m.detailInput, cmd = m.detailInput.Update(msg)
	case focusModels:
		m.modelsList, cmd = m.modelsList.Update(msg)
	case focusPlans:
		m.plansList, cmd = m.plansList.Update(msg)
	}
	return m, cmd
}

func (m model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.isLoading || m.isEditing {
		return m, nil
	}

	leftW, _ := m.columnWidths()
	totalH := m.height - bottomBarH
	listPanelH := totalH - formPanelH - modelPanelH
	if listPanelH < 4 {
		listPanelH = 4
	}

	// Only clicks inside the left column change panel focus; the
	// description panel on the right is not interactive.
	if msg.X < leftW {
		switch {
		case msg.Y < formPanelH:
			m.setFocus(focusSubject)
			return m, nil
		case msg.Y < formPanelH+modelPanelH:
			m.setFocus(focusModels)
		default:
			m.setFocus(focusPlans)
		}
	}

	var cmd tea.Cmd
	switch m.focusIndex {
	case focusModels:
		m.modelsList, cmd = m.modelsList.Update(msg)
	case focusPlans:
		m.plansList, cmd = m.plansList.Update(msg)
	}
	return m, cmd
}
