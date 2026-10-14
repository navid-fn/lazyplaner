package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) handleSubmit() (tea.Model, tea.Cmd) {
	subject := strings.TrimSpace(m.newPlan.subject.Value())
	detail := strings.TrimSpace(m.newPlan.description.Value())
	if subject == "" || detail == "" {
		m.errorText = "Subject and description are required."
		return m, nil
	}
	if m.client == nil || m.store == nil {
		m.errorText = "Planner is not configured."
		return m, nil
	}
	m.isLoading = true
	m.loadingText = fmt.Sprintf("Generating plan with %s…", m.selectedModel)
	m.statusText = ""
	m.errorText = ""
	return m, tea.Batch(m.spinner.Tick, generatePlanCmd(m.client, m.store, subject, detail))
}

func (m model) handleSelectModel() (tea.Model, tea.Cmd) {
	item := m.modelsList.SelectedItem()
	if item == nil {
		m.errorText = "No model selected."
		return m, nil
	}
	mi, ok := item.(modelItem)
	if !ok {
		return m, nil
	}
	if m.client == nil {
		m.errorText = "Planner is not configured."
		return m, nil
	}
	m.client.SetModel(mi.name)
	m.selectedModel = mi.name
	m.errorText = ""
	m.statusText = fmt.Sprintf("Model set to %s.", mi.name)
	m.screen = m.modelsReturn
	m.setModels(m.models)
	return m, nil
}
