package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// openModels shows the model picker and refreshes the installed model list.
// ctrl+o opens it from any screen; closing it returns to the screen that
// opened it, popups included.
func (m model) openModels() (tea.Model, tea.Cmd) {
	m.modelsReturn = m.screen
	m.screen = screenModels
	m.errorText = ""
	return m, loadModelsCmd(m.client)
}

func (m model) handleModelsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+o":
		m.screen = m.modelsReturn
		return m, nil
	case "enter":
		return m.handleSelectModel()
	case "r":
		if !m.isLoading {
			return m, loadModelsCmd(m.client)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.modelsList, cmd = m.modelsList.Update(msg)
	return m, cmd
}
