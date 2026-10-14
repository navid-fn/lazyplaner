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

	// Help and the skill editor are drawn over the screen that opened them.
	base := m
	if m.screen == screenHelp || m.screen == screenSkillEdit || m.screen == screenDaily {
		base.screen = m.prevScreen
	}
	if m.screen == screenDailyPick {
		// The picker is a small popup on the plan list of its own.
		base.screen = screenMain
	}
	body := base.baseBody()

	// Popups are drawn over the screen, which stays visible around them.
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
	case m.screen == screenHelp:
		body = overlay(body, m.renderHelp(), m.width, bodyH)
	case m.screen == screenSkillEdit:
		body = overlay(body, m.renderSkillEdit(), m.width, bodyH)
	case m.screen == screenDaily:
		body = overlay(body, m.renderDailyPopup(), m.width, bodyH)
	case m.screen == screenDailyPick:
		body = overlay(body, m.renderDailyPicker(), m.width, bodyH)
	}

	return body + "\n" + m.renderBottomBar()
}

// baseBody draws the screen for m.screen, without popups.
func (m model) baseBody() string {
	switch m.screen {
	case screenRoad:
		return m.renderRoadScreen()
	case screenTree, screenSkillEdit:
		return m.renderTreeScreen()
	case screenOnboard:
		return m.renderOnboardScreen()
	}
	return m.renderHome()
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
	case screenRoad:
		pairs = []kd{{"hjkl ←↑↓→", "move"}, {"enter", "open tree"}, {"?", "help"}, {"c", "back"}, {"q", "quit"}}
	case screenTree:
		pairs = []kd{{"←→ hl", "skill"}, {"↑↓ jk", "node"}, {"e", "edit"}, {"?", "help"}, {"t", "back"}, {"q", "quit"}}
	case screenDailyPick:
		pairs = []kd{{"←→", "count"}, {"enter", "generate"}, {"esc", "back"}}
	case screenDaily:
		if m.isLoading {
			pairs = []kd{{"ctrl+c", "quit"}}
		} else {
			pairs = []kd{{"↑↓", "move"}, {"enter", "tick off"}, {"g", "close"}}
			if canRenew(m.challenges) {
				pairs = append([]kd{{"r", "new set"}}, pairs...)
			}
		}
	case screenOnboard:
		if m.isLoading {
			pairs = []kd{{"ctrl+c", "quit"}}
		} else {
			pairs = []kd{{"tab", "next field"}, {"enter", "add/save"}, {"↑↓", "pick (list)"}, {"ctrl+d", "remove"}, {"ctrl+s", "build"}, {"?", "help"}, {"esc", "skip"}}
		}
	case screenHelp:
		pairs = []kd{{"←→", "section"}, {"esc", "close"}}
	case screenSkillEdit:
		pairs = []kd{{"enter", "save"}, {"esc", "cancel"}}
	default:
		if m.confirmDelete != nil {
			pairs = []kd{{"y", "delete"}, {"any key", "cancel"}}
			break
		}
		if m.mainFocus == focusDirectives {
			pairs = []kd{{"tab", "plans"}, {"↑↓", "move"}, {"enter", "complete"}, {"g", "daily"}, {"ctrl+n", "new plan"}, {"c", "character"}, {"t", "tree"}, {"s", "setup"}, {"h", "help"}, {"q", "quit"}}
		} else {
			pairs = []kd{{"tab", "directives"}, {"↑↓", "navigate"}, {"ctrl+n", "new plan"}, {"e", "edit plan"}, {"d", "delete plan"}, {"c", "character"}, {"t", "tree"}, {"g", "daily"}, {"h", "help"}, {"ctrl+o", "model"}, {"q", "quit"}}
		}
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
