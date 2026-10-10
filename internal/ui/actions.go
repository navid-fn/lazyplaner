package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
	m.setModels(m.models)
	return m, nil
}
