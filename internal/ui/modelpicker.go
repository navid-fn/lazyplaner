package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// openModels shows the model picker and refreshes the installed model list.
// The picker can be opened from the plan list or from the New Plan window;
// closing it returns to whichever screen opened it.
func (m model) openModels() (tea.Model, tea.Cmd) {
	m.prevScreen = m.screen
	m.screen = screenModels
	m.errorText = ""
	return m, loadModelsCmd(m.client)
}

func (m model) handleModelsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = m.prevScreen
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
