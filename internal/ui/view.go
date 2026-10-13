package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading…"
	}

	bodyH := m.bodyHeight()
	leftW, rightW := m.columnWidths()

	listPanel := panel("Plans", m.plansList.View(), leftW, bodyH, m.screen == screenMain && !m.isEditing)
	previewPanel := panel("Preview", m.renderPreview(rightW-2), rightW, bodyH, m.isEditing)
	body := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, previewPanel)

	// Popups are drawn over the plan screen, which stays visible around them.
	switch {
	case m.confirmDelete != nil:
		body = overlay(body, m.renderDeletePopup(), m.width, bodyH)
	case m.screen == screenNewPlan:
		body = overlay(body, m.renderNewPlan(), m.width, bodyH)
	case m.screen == screenModels:
		// The picker opened from New Plan keeps that form visible behind it.
		if m.prevScreen == screenNewPlan {
			body = overlay(body, m.renderNewPlan(), m.width, bodyH)
		}
		body = overlay(body, m.renderModelPicker(), m.width, bodyH)
	}

	return body + "\n" + m.renderBottomBar()
}

func (m model) renderDeletePopup() string {
	w := clamp(m.width-6, 30, 50)

	var b strings.Builder
	b.WriteString(stError.Render("Delete this plan?") + "\n\n")
	b.WriteString(stActive.Render(m.confirmDelete.Subject) + "\n\n")
	b.WriteString(stDim.Render("y delete · any other key cancels"))

	// Lines: title, blank, subject, blank, hint, plus the two borders.
	return panel("Confirm", b.String(), w, 7, true)
}

func (m model) renderNewPlan() string {
	w, h := m.newPlanSize()

	subLabel := stDim.Render("Subject")
	descLabel := stDim.Render("Description")
	if m.newPlan.descFocused {
		descLabel = stActive.Render("Description")
	} else {
		subLabel = stActive.Render("Subject")
	}

	var b strings.Builder
	b.WriteString(subLabel + "\n")
	b.WriteString(m.newPlan.subject.View() + "\n\n")
	b.WriteString(descLabel + "\n")
	b.WriteString(m.newPlan.description.View() + "\n\n")
	if m.isLoading {
		b.WriteString(m.spinner.View() + " " + stActive.Render(m.loadingText))
	} else {
		b.WriteString(stDim.Render("tab switch field · ctrl+s generate · esc cancel"))
	}

	return panel("New Plan", b.String(), w, h, true)
}

func (m model) renderModelPicker() string {
	w, h := m.modelPanelSize()

	content := m.modelsList.View()
	if len(m.models) == 0 {
		content = stDim.Render("No models found. Is Ollama running?\n\nPress r to retry.")
	}
	return panel("Select Model", content, w, h, true)
}

func (m model) renderPreview(innerW int) string {
	if m.isEditing {
		return stActive.Render("Editing in external editor…") + "\n\n" +
			stDim.Render("Save and close the editor to return.")
	}

	plan, ok := m.currentPlan()
	if !ok {
		if len(m.plans) == 0 {
			return stDim.Render("No plans yet.\n\nPress ctrl+n to write your first plan.")
		}
		return stDim.Render("No plan selected.")
	}

	header := stActive.Render(plan.Subject) + "\n" +
		stDim.Render("Created "+formatPlanDate(plan.CreatedAt)) + "\n\n"

	desc := strings.TrimSpace(plan.Description)
	if desc == "" {
		desc = stDim.Render("No description available.")
	}

	// Soft-wrap long lines to fit the panel.
	return lipgloss.NewStyle().Width(innerW).Render(header + desc)
}

func (m model) renderBottomBar() string {
	var statusLine string
	switch {
	case m.errorText != "":
		statusLine = stError.Render("✗ " + m.errorText)
	case m.isLoading:
		statusLine = m.spinner.View() + " " + stActive.Render(m.loadingText)
	case m.statusText != "":
		statusLine = stSuccess.Render("✓ " + m.statusText)
	}

	keysLine := m.renderKeys()
	modelTag := stDim.Render("model ") + stActive.Render(m.modelLabel())
	gap := max(m.width-lipgloss.Width(keysLine)-lipgloss.Width(modelTag)-2, 2)
	line := keysLine + strings.Repeat(" ", gap) + modelTag

	separator := stSep.Render(strings.Repeat("─", m.width))

	return statusLine + "\n" + separator + "\n" +
		lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

// renderKeys lists the shortcuts that apply to the current screen.
func (m model) renderKeys() string {
	type kd struct{ k, d string }
	var pairs []kd
	switch m.screen {
	case screenNewPlan:
		if m.isLoading {
			pairs = []kd{{"ctrl+c", "quit"}}
		} else {
			pairs = []kd{{"tab", "next field"}, {"ctrl+s", "generate"}, {"ctrl+o", "model"}, {"esc", "cancel"}}
		}
	case screenModels:
		pairs = []kd{{"↑↓", "navigate"}, {"enter", "select"}, {"r", "refresh"}, {"esc", "close"}}
	default:
		if m.confirmDelete != nil {
			pairs = []kd{{"y", "delete"}, {"any key", "cancel"}}
			break
		}
		pairs = []kd{{"ctrl+n", "new plan"}, {"↑↓", "navigate"}, {"e", "edit plan"}, {"d", "delete plan"}, {"ctrl+o", "model"}, {"q", "quit"}}
	}

	sep := stSep.Render("  │  ")
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, stKey.Render(p.k)+stDim.Render(" "+p.d))
	}
	return "  " + strings.Join(parts, sep)
}

func (m model) modelLabel() string {
	if m.selectedModel == "" {
		return "none"
	}
	return m.selectedModel
}
