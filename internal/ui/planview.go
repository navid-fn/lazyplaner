package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
)

// The plan reader: enter on a plan opens it full screen, its markdown
// rendered with headings, lists, tables and code blocks, and scrolled with
// the usual keys. e edits it in $EDITOR from here too.

// planViewChrome is the rows the reader's panel spends around the text:
// the two borders, the title, the meta line, a rule, and the scroll line.
const planViewChrome = 6

// planViewer is the open reader.
type planViewer struct {
	plan db.Plan
	vp   viewport.Model
}

// openPlanView shows the selected plan in the reader.
func (m *model) openPlanView() {
	plan, ok := m.currentPlan()
	if !ok {
		m.errorText = "Select a plan to read."
		return
	}
	m.errorText, m.statusText = "", ""
	m.planView = planViewer{plan: plan}
	m.screen = screenPlanView
	m.layoutPlanView()
}

// layoutPlanView fits the reader to the window and renders the plan's
// markdown at that width. It keeps the scroll position when it can.
func (m *model) layoutPlanView() {
	w := max(m.width-4, 20) // panel border and one cell of padding each side
	h := max(m.bodyHeight()-planViewChrome, 3)
	offset := m.planView.vp.YOffset
	m.planView.vp = viewport.New(w, h)
	m.planView.vp.SetContent(renderMarkdown(m.planView.plan.Description, w))
	m.planView.vp.SetYOffset(offset)
}

// renderMarkdown renders md for the terminal at width. If the renderer
// fails, the plain text is shown wrapped instead, so a plan is never lost.
func renderMarkdown(md string, width int) string {
	md = strings.TrimSpace(md)
	if md == "" {
		return stDim.Render("This plan has no description yet. Press e to write one.")
	}
	r, err := glamour.NewTermRenderer(
		// A fixed style: asking the terminal for its background would
		// block, and the rest of the UI is drawn for a dark one.
		glamour.WithStandardStyle(styles.DarkStyle),
		glamour.WithWordWrap(width-2),
		glamour.WithEmoji(),
	)
	if err == nil {
		if out, err := r.Render(md); err == nil {
			return strings.Trim(out, "\n")
		}
	}
	return lipgloss.NewStyle().Width(width).Render(md)
}

// handlePlanViewKey scrolls the reader. e edits the plan; esc, q and
// enter go back to the plan list.
func (m model) handlePlanViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vp := &m.planView.vp
	switch msg.String() {
	case "esc", "q", "enter":
		m.screen = screenMain
		return m, nil
	case "e":
		return m.handleEditStart()
	case "g", "home":
		vp.GotoTop()
	case "G", "end":
		vp.GotoBottom()
	case "?":
		m.openHelp()
	default:
		// j/k, arrows, pgup/pgdn, space, ctrl+u/d: the viewport's own keys.
		var cmd tea.Cmd
		*vp, cmd = vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// renderPlanView is the reader: the plan's title and meta on top, the
// rendered markdown, and where the scroll is at the bottom.
func (m model) renderPlanView() string {
	pv := m.planView
	inner := m.width - 2

	title := " " + gradientText(truncRunes(pv.plan.Subject, inner-4), string(colorActive), "#00e5ff")
	meta := []string{stDim.Render("📅 " + formatPlanDate(pv.plan.CreatedAt))}
	if badge, ok := m.planBadges()[pv.plan.ID]; ok {
		meta = append(meta, stSuccess.Render("⚡ "+strings.Trim(badge, "[]")))
	}
	words := len(strings.Fields(pv.plan.Description))
	meta = append(meta, stDim.Render(fmt.Sprintf("📝 %d words · ~%d min read", words, max(1, (words+199)/200))))
	metaLine := " " + strings.Join(meta, stSep.Render("  │  "))
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", inner))

	body := pv.vp.View()
	scroll := stDim.Render(fmt.Sprintf("%3.0f%%", pv.vp.ScrollPercent()*100))
	if pv.vp.TotalLineCount() <= pv.vp.Height {
		scroll = stDim.Render("all")
	}
	hint := stDim.Render(" j/k scroll · space/b page · g/G top/end · e edit · esc back")
	footer := spread(hint, scroll+" ", inner)

	content := strings.Join([]string{
		ansi.Truncate(title, inner, "…"),
		ansi.Truncate(metaLine, inner, "…"),
		rule,
		indentLines(body, " "),
		footer,
	}, "\n")
	return panel("📜 Plan", content, m.width, m.bodyHeight(), true)
}

// indentLines puts prefix before every line of s.
func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
