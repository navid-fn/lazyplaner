package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/internal/config"
	"github.com/navid-fn/lazyplaner/internal/db"
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

// modelSavedMsg reports whether the chosen model was saved for next time.
type modelSavedMsg struct {
	err error
}

// saveModelCmd saves the chosen model, so the next start uses it too.
func saveModelCmd(store *db.Store, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return modelSavedMsg{err: store.SetSelectedModel(ctx, name)}
	}
}

// installedModel returns the model to use given the installed ones: current
// when it is installed, else config.DefaultOllamaModel when that is, else
// the first installed model. switched is false when current is kept or
// nothing is installed to switch to.
func installedModel(current string, installed []string) (name string, switched bool) {
	if len(installed) == 0 {
		return current, false
	}
	for _, m := range installed {
		if modelNamesMatch(m, current) {
			return current, false
		}
	}
	for _, m := range installed {
		if modelNamesMatch(m, config.DefaultOllamaModel) {
			return m, true
		}
	}
	return installed[0], true
}

// ensureInstalledModel switches away from a model Ollama does not have,
// such as one deleted since it was picked, and saves the new choice.
func (m *model) ensureInstalledModel() tea.Cmd {
	if m.client == nil {
		return nil
	}
	name, switched := installedModel(m.selectedModel, m.models)
	if !switched {
		return nil
	}
	old := m.selectedModel
	m.client.SetModel(name)
	m.selectedModel = name
	m.setModels(m.models)
	m.statusText = "Model " + old + " is not installed; switched to " + name + "."
	return saveModelCmd(m.store, name)
}
