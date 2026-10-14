package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// ─── Rendering ───────────────────────────────────────────────────────────────

// Input limits shown in the counters. They match the CharLimit of each input.
const (
	subjectLimit     = 120
	descriptionLimit = 2000
)

// renderNewPlan draws the New Plan window: two bordered inputs with counters,
// a status row, and key hints.
func (m model) renderNewPlan() string {
	w, h := m.newPlanSize()
	innerW := w - 4 // panel border and one cell of padding on each side

	subjectFocused := !m.newPlan.descFocused
	descFocused := m.newPlan.descFocused

	var b strings.Builder
	b.WriteString(fieldHeader("Subject", counter(m.newPlan.subject.Value(), subjectLimit), subjectFocused, innerW) + "\n")
	b.WriteString(inputBox(m.newPlan.subject.View(), innerW, subjectFocused) + "\n")
	b.WriteString(fieldHeader("Description", counter(m.newPlan.description.Value(), descriptionLimit), descFocused, innerW) + "\n")
	b.WriteString(inputBox(m.newPlan.description.View(), innerW, descFocused) + "\n")

	if m.isLoading {
		b.WriteString(m.spinner.View() + " " + stActive.Render(m.loadingText))
	} else {
		b.WriteString(stDim.Render("The description is written by the selected model."))
	}
	b.WriteString("\n" + m.newPlanKeys())

	return panel("New Plan", b.String(), w, h, true)
}

// fieldHeader draws a field label on the left and its counter on the right.
func fieldHeader(label, count string, focused bool, width int) string {
	style := stDim
	if focused {
		style = stActive
	}
	gap := max(width-lipgloss.Width(label)-lipgloss.Width(count), 1)
	return style.Render(label) + strings.Repeat(" ", gap) + count
}

// inputBox wraps an input in a rounded border. The border is green while the
// input has focus and dim otherwise.
func inputBox(content string, width int, focused bool) string {
	color := colorInactive
	if focused {
		color = colorActive
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Width(width - 2).
		Render(content)
}

// counter shows how much of a limit is used. It turns red near the limit.
func counter(value string, limit int) string {
	n := utf8.RuneCountInString(value)
	style := stDim
	if n >= limit*9/10 {
		style = stError
	}
	return style.Render(fmt.Sprintf("%d/%d", n, limit))
}

// newPlanKeys draws the key hints as keycaps.
func (m model) newPlanKeys() string {
	if m.isLoading {
		return keyChip("ctrl+c", "quit")
	}
	return strings.Join([]string{
		keyChip("tab", "next"),
		keyChip("ctrl+s", "generate"),
		keyChip("ctrl+o", "model"),
		keyChip("esc", "cancel"),
	}, "  ")
}

// keyChip is one hint: a key name in a keycap, then what it does.
func keyChip(key, action string) string {
	return stChip.Render(" "+key+" ") + stDim.Render(" "+action)
}
