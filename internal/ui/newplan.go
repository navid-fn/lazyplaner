package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// openNewPlan shows the New Plan window with the subject field focused.
func (m *model) openNewPlan() tea.Cmd {
	m.screen = screenNewPlan
	m.errorText = ""
	m.statusText = ""
	return m.focusNewPlan(false)
}

// closeNewPlan returns to the plan list and releases focus from the inputs.
func (m *model) closeNewPlan() {
	m.screen = screenMain
	m.newPlan.subject.Blur()
	m.newPlan.description.Blur()
}

// focusNewPlan moves focus to the description (desc true) or the subject.
func (m *model) focusNewPlan(desc bool) tea.Cmd {
	m.newPlan.descFocused = desc
	if desc {
		m.newPlan.subject.Blur()
		return m.newPlan.description.Focus()
	}
	m.newPlan.description.Blur()
	return m.newPlan.subject.Focus()
}

func (m model) handleNewPlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if !m.isLoading {
			m.closeNewPlan()
		}
		return m, nil
	case "tab", "shift+tab":
		if m.isLoading {
			return m, nil
		}
		cmd := m.focusNewPlan(!m.newPlan.descFocused)
		return m, cmd
	case "ctrl+s":
		return m.handleSubmit()
	case "ctrl+o":
		// The form keeps its text while the picker is open. A plan that is
		// already generating keeps the model it started with.
		return m.openModels()
	case "enter":
		// Enter moves from subject to description; inside the description
		// it inserts a newline.
		if !m.newPlan.descFocused {
			cmd := m.focusNewPlan(true)
			return m, cmd
		}
	}

	if m.isLoading {
		return m, nil
	}
	var cmd tea.Cmd
	if m.newPlan.descFocused {
		m.newPlan.description, cmd = m.newPlan.description.Update(msg)
	} else {
		m.newPlan.subject, cmd = m.newPlan.subject.Update(msg)
	}
	return m, cmd
}
