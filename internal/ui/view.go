package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading…"
	}

	leftW, rightW := m.columnWidths()
	totalH := m.height - bottomBarH

	listPanelH := totalH - formPanelH - modelPanelH
	if listPanelH < 4 {
		listPanelH = 4
	}
	descPanelH := totalH

	formActive := m.focusIndex == focusSubject || m.focusIndex == focusDetail
	modelsActive := m.focusIndex == focusModels
	listActive := m.focusIndex == focusPlans

	formPanel := panel("New Plan", m.renderFormContent(), leftW, formPanelH, formActive)
	modelTitle := "Model"
	if m.selectedModel != "" {
		modelTitle = fmt.Sprintf("Model: %s", m.selectedModel)
	}
	modelPanel := panel(modelTitle, m.modelsList.View(), leftW, modelPanelH, modelsActive)
	listPanel := panel("Plans", m.plansList.View(), leftW, listPanelH, listActive)
	descPanel := panel("Description", m.renderDescContent(rightW-2), rightW, descPanelH, m.isEditing)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, formPanel, modelPanel, listPanel)
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, descPanel)

	return mainArea + "\n" + m.renderBottomBar()
}

func (m model) renderFormContent() string {
	subLabel := stDim.Render("Subject")
	if m.focusIndex == focusSubject {
		subLabel = stActive.Render("Subject")
	}

	detLabel := stDim.Render("Detail")
	if m.focusIndex == focusDetail {
		detLabel = stActive.Render("Detail")
	}

	formFocused := m.focusIndex == focusSubject || m.focusIndex == focusDetail

	var submit string
	switch {
	case m.isLoading:
		submit = stActive.Render("⠋ Generating…")
	case formFocused:
		submit = lipgloss.NewStyle().
			Foreground(lipgloss.Color("232")).
			Background(colorActive).
			Padding(0, 1).
			Bold(true).
			Render("[ Submit ]") + stDim.Render(" (enter)")
	default:
		submit = stDim.Render("[ Submit ] (enter)")
	}

	var b strings.Builder
	b.WriteString(subLabel + "\n")
	b.WriteString(m.subjectInput.View() + "\n")
	b.WriteString("\n")
	b.WriteString(detLabel + "\n")
	b.WriteString(m.detailInput.View() + "\n")
	b.WriteString("\n")
	b.WriteString(submit)
	return b.String()
}

func (m model) renderDescContent(innerW int) string {
	if m.isEditing {
		return stActive.Render("Editing in external editor…") + "\n\n" +
			stDim.Render("Save and close the editor to return.")
	}

	plan, ok := m.currentPlan()
	if !ok {
		if len(m.plansByID) == 0 {
			return stDim.Render("No plans yet.\n\nFill in the form and hit Submit to generate your first plan.")
		}
		return stDim.Render("No plan selected.")
	}

	desc := strings.TrimSpace(plan.Description)
	if desc == "" {
		return stDim.Render("No description available.")
	}

	// Soft-wrap long lines to fit the panel.
	return lipgloss.NewStyle().Width(innerW).Render(desc)
}

func (m model) renderBottomBar() string {
	var statusLine string
	switch {
	case m.errorText != "":
		statusLine = stError.Render("✗ " + m.errorText)
	case m.statusText != "":
		statusLine = stSuccess.Render("✓ " + m.statusText)
	}

	type kd struct{ k, d string }
	pairs := []kd{
		{"tab", "next panel"},
		{"↑↓", "navigate"},
		{"enter", "submit/select"},
		{"e", "edit plan"},
		{"r", "refresh models"},
		{"q", "quit"},
	}
	sep := stSep.Render("  │  ")
	var keyParts []string
	for _, p := range pairs {
		keyParts = append(keyParts, stKey.Render(p.k)+stDim.Render(" "+p.d))
	}
	keysLine := "  " + strings.Join(keyParts, sep)

	separator := stSep.Render(strings.Repeat("─", m.width))

	return statusLine + "\n" + separator + "\n" + keysLine
}
