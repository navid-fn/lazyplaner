package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
)

// openDaily shows the daily challenges popup over the current screen and
// loads today's list. If today has no set yet, the size picker opens next.
func (m *model) openDaily() tea.Cmd {
	m.prevScreen = m.screen
	m.screen = screenDaily
	m.errorText = ""
	m.statusText = ""
	m.isLoading = true
	m.loadingText = "Loading today's challenges…"
	m.dailyOpening = true
	return tea.Batch(m.spinner.Tick, loadDailyCmd(m.client, m.store, dailyLoad, 0))
}

func (m model) handleDailyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isLoading {
		if msg.String() == "esc" {
			m.screen = m.prevScreen
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "g":
		m.screen = m.prevScreen
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.challengeCursor > 0 {
			m.challengeCursor--
		}
	case "down", "j":
		if m.challengeCursor < len(m.challenges)-1 {
			m.challengeCursor++
		}
	case "enter", " ":
		return m.completeSelectedChallenge()
	case "r":
		// A new set is made when today has none (or generating failed), or
		// when every challenge in today's set is done.
		if !canRenew(m.challenges) {
			m.statusText = "Finish today's challenges first, then you can get a new set."
			return m, nil
		}
		cmd := m.openDailyPicker(len(m.challenges) > 0)
		return m, cmd
	}
	return m, nil
}

// completeSelectedChallenge ticks off the challenge under the cursor. The
// daily popup and the home screen's directives both use it.
func (m model) completeSelectedChallenge() (tea.Model, tea.Cmd) {
	if m.challengeCursor >= len(m.challenges) {
		return m, nil
	}
	c := m.challenges[m.challengeCursor]
	if c.Done {
		m.statusText = "Already done."
		return m, nil
	}
	m.isLoading = true
	m.loadingText = "Logging " + c.Title + "…"
	m.errorText = ""
	m.statusText = ""
	return m, tea.Batch(m.spinner.Tick, completeChallengeCmd(m.store, c.ID))
}

// renderDailyPopup draws today's challenges as a quest board: a header with
// the date, streak and score, a segmented bar with one segment per
// challenge, a card per challenge, and the keys. The popup is as tall as its
// contents, up to the screen height.
func (m model) renderDailyPopup() string {
	w := clamp(m.width-10, 60, 96)
	innerW := w - 4

	lines := []string{m.questHeader(innerW), m.questProgress(innerW), ""}

	switch {
	case m.isLoading:
		lines = append(lines, " "+m.spinner.View()+" "+stActive.Render(m.loadingText), "")
	case len(m.challenges) == 0:
		lines = append(lines,
			lipgloss.NewStyle().Width(innerW).Align(lipgloss.Center).Render(
				stDim.Render("No quests for today yet.\nPress r to choose how many to take on.")),
			"")
	default:
		// Cards span the panel's full inner width.
		for i, c := range m.challenges {
			lines = append(lines, questCard(c, i == m.challengeCursor, w-2))
		}
		if canRenew(m.challenges) {
			lines = append(lines, "", m.questBanner(innerW))
		}
		lines = append(lines, "")
	}
	lines = append(lines, " "+m.dailyKeys())

	content := strings.Join(lines, "\n")
	h := min(lipgloss.Height(content)+2, m.bodyHeight())
	return panel("Daily quests", content, w, h, true)
}

// questHeader is the title line: today's date on the left, streak and score
// on the right.
func (m model) questHeader(width int) string {
	st := m.challengeStats
	title := " " + stActive.Render("◆ TODAY'S QUESTS") + stDim.Render(" · "+time.Now().Format("Mon Jan 2"))
	chips := chipLabel(fmt.Sprintf("streak %d day%s", st.Streak, plural(st.Streak)), colorKeyLabel) + " " +
		chipLabel(fmt.Sprintf("score %d", st.Score), colorSuccess) + " "
	return spread(title, chips, width)
}

// questProgress is a bar split into one segment per challenge. Finished
// challenges fill their segment, so progress reads at a glance.
func (m model) questProgress(width int) string {
	n := len(m.challenges)
	done, earned, total := 0, 0, 0
	for _, c := range m.challenges {
		total += c.XP
		if c.Done {
			done++
			earned += c.XP
		}
	}
	summary := stActive.Render(fmt.Sprintf(" %d/%d done", done, n)) +
		stDim.Render(" · ") + stSuccess.Render(fmt.Sprintf("%d/%d xp", earned, total))

	barW := width - lipgloss.Width(summary) - 2
	if n == 0 {
		return " " + gradientBar(0, barW, string(colorSuccess), string(colorSuccess)) + summary
	}

	segW := max((barW-(n-1))/n, 1)
	var segs []string
	for i, c := range m.challenges {
		if c.Done {
			from := mixColor(string(colorActive), string(colorSuccess), float64(i)/float64(max(n-1, 1)))
			segs = append(segs, gradientBar(1, segW, from, lighten(from, 0.35)))
		} else {
			segs = append(segs, gradientBar(0, segW, "", ""))
		}
	}
	return " " + strings.Join(segs, " ") + summary
}

// questBanner celebrates a finished day and points at the next set.
func (m model) questBanner(width int) string {
	earned := 0
	for _, c := range m.challenges {
		earned += c.XP
	}
	text := fmt.Sprintf("★  All quests complete · +%d xp today  ★", earned)
	line := gradientText(text, string(colorActive), "#00e5ff")
	hint := stDim.Render("Press r for a new set.")
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(line + "\n" + hint)
}

// questCard draws one challenge. The top border holds its title and the
// skill it trains; inside are its difficulty, time, node and XP, then what to
// do. The selected card has a thick white border, and a finished one turns
// green with a ✓ before its title.
func questCard(c db.Challenge, selected bool, width int) string {
	cat := string(categoryColor(c.Category))
	inner := width - 4

	border, titleInk := string(colorInactive), "252"
	if c.Done {
		border, titleInk = string(colorSuccess), string(colorSuccess)
	}
	if selected {
		border = inkSelect
	}

	dots := lipgloss.NewStyle().Foreground(colorKeyLabel).Render(strings.Repeat("●", c.Difficulty)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorTrack)).Render(strings.Repeat("○", max(5-c.Difficulty, 0)))
	meta := dots + stDim.Render(fmt.Sprintf("  ◷ %d min", c.Minutes))
	if c.NodeName != "" {
		meta += stDim.Render("  › ") + lipgloss.NewStyle().Foreground(lipgloss.Color(cat)).Render(c.NodeName)
	}

	xp := chipLabel(fmt.Sprintf("+%d XP", c.XP), colorSuccess)
	if c.Done {
		xp = stSuccess.Render(fmt.Sprintf("✓ earned %d xp", c.XP))
	}
	line1 := spread(ansi.Truncate(meta, inner-lipgloss.Width(xp)-2, "…"), xp, inner)

	desc := c.Description
	if desc == "" {
		desc = "No description."
	}
	descInk := "245"
	if selected && !c.Done {
		descInk = "252"
	}
	wrapped := strings.Split(lipgloss.NewStyle().Width(inner).Render(truncRunes(desc, inner*2)), "\n")
	for len(wrapped) < 2 {
		wrapped = append(wrapped, "")
	}
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(descInk))

	title := c.Title
	if c.Done {
		title = "✓ " + title
	}
	return cardFrame(title, c.SkillName, cat,
		[]string{line1, descStyle.Render(wrapped[0]), descStyle.Render(wrapped[1])},
		width, border, titleInk, selected)
}

// dailyKeys draws the key hints for the popup, with a message when every
// challenge is done.
func (m model) dailyKeys() string {
	if m.isLoading {
		return keyChip("esc", "close")
	}
	keys := []string{keyChip("↑↓", "move"), keyChip("enter", "tick off")}
	if canRenew(m.challenges) {
		keys = append(keys, keyChip("r", "new set"))
	}
	keys = append(keys, keyChip("esc", "close"))
	return strings.Join(keys, "  ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
