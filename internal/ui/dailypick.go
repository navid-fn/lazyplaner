package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
)

// sizeNames describes each set size, so the number has a feel to it.
var sizeNames = map[int]string{
	1: "Just one",
	2: "Light",
	3: "Balanced",
	4: "Busy",
	5: "Full day",
}

// openDailyPicker asks how many challenges to make. renew is true when a
// new set replaces a finished one, and false for the first set of the day.
// The picker opens over the daily popup and returns to it.
func (m *model) openDailyPicker(renew bool) tea.Cmd {
	m.screen = screenDailyPick
	m.dailyPickRenew = renew
	m.dailyPickSize = m.dailyDefault
	if m.dailyPickSize == 0 {
		m.dailyPickSize = db.DefaultDailyChallenges
	}
	m.errorText = ""
	m.statusText = ""
	return nil
}

func (m model) handleDailyPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.screen = screenDaily
	case "left", "down":
		m.dailyPickSize = clamp(m.dailyPickSize-1, db.MinDailyChallenges, db.MaxDailyChallenges)
	case "right", "up":
		m.dailyPickSize = clamp(m.dailyPickSize+1, db.MinDailyChallenges, db.MaxDailyChallenges)
	case "enter":
		mode := dailyCreate
		if m.dailyPickRenew {
			mode = dailyRenew
		}
		m.screen = screenDaily
		m.isLoading = true
		m.loadingText = fmt.Sprintf("Preparing %d challenge%s…", m.dailyPickSize, plural(m.dailyPickSize))
		m.errorText = ""
		m.statusText = ""
		m.dailyDefault = m.dailyPickSize
		return m, tea.Batch(m.spinner.Tick, loadDailyCmd(m.client, m.store, mode, m.dailyPickSize))
	}
	return m, nil
}

// renderDailyPicker draws the size picker as a small popup.
func (m model) renderDailyPicker() string {
	w := clamp(m.width-10, 40, 56)
	h := min(11, m.bodyHeight())
	innerW := w - 4

	number := lipgloss.NewStyle().
		Background(colorActive).
		Foreground(lipgloss.Color("#0b0f14")).
		Bold(true).
		Padding(0, 2).
		Render(fmt.Sprintf("%d", m.dailyPickSize))
	arrows := stActive.Render("‹") + "    " + number + "    " + stActive.Render("›")

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Width(innerW).Align(lipgloss.Center).Render(
		stActive.Render("How many challenges today?")) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Width(innerW).Align(lipgloss.Center).Render(arrows) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Width(innerW).Align(lipgloss.Center).Render(
		stDim.Render(sizeNames[m.dailyPickSize])) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Width(innerW).Align(lipgloss.Center).Render(
		keyChip("←→", "change") + "  " + keyChip("enter", "generate") + "  " + keyChip("esc", "back")))

	return panel("New set", b.String(), w, h, true)
}
