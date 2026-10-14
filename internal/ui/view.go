package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading…"
	}

	return fitLines(m.renderBody(m.screen), m.bodyHeight()) + "\n" + m.renderBottomBar()
}

// renderBody draws screen s. Popups are drawn over the screen that opened
// them, which stays visible around them: the model picker over whatever
// screen it came from (another popup included), help and the skill editor
// over prevScreen, New Plan and the delete prompt over the home screen.
func (m model) renderBody(s screen) string {
	bodyH := m.bodyHeight()
	under := func(prev screen) string {
		if prev == s {
			prev = screenMain // never draw a popup under itself
		}
		return m.renderBody(prev)
	}
	switch s {
	case screenModels:
		return overlay(under(m.modelsReturn), m.renderModelPicker(), m.width, bodyH)
	case screenHelp:
		return overlay(under(m.prevScreen), m.renderHelp(), m.width, bodyH)
	case screenSkillEdit:
		return overlay(under(m.prevScreen), m.renderSkillEdit(), m.width, bodyH)
	}

	base := m
	base.screen = s
	body := base.baseBody()
	switch {
	case s == screenNewPlan:
		body = overlay(body, m.renderNewPlan(), m.width, bodyH)
	case m.confirmDelete != nil:
		body = overlay(body, m.renderDeletePopup(), m.width, bodyH)
	}
	return body
}

// fitLines pads or cuts s to exactly n lines. The bottom bar sits under the
// body, so a body one line too tall would push the screen's top out of the
// terminal.
func fitLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines[:n], "\n")
}

// baseBody draws the screen for m.screen, without popups.
func (m model) baseBody() string {
	switch m.screen {
	case screenRoad:
		return m.renderRoadScreen()
	case screenTree:
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
	// The status line is one row, cut to the width. A long error (a model
	// timeout, say) would otherwise wrap in the terminal and scroll the top
	// of the screen out of view.
	oneLine := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var statusLine string
	switch {
	case m.errorText != "":
		statusLine = stError.Render("✗ " + oneLine(m.errorText))
	case m.isLoading:
		statusLine = m.spinner.View() + " " + stActive.Render(oneLine(m.loadingText))
	case m.statusText != "":
		statusLine = stSuccess.Render("✓ " + oneLine(m.statusText))
	}
	statusLine = ansi.Truncate(statusLine, max(m.width-1, 1), "…")

	// The keys come first; the model tag, with the key that changes it,
	// shows where there is room: in full, then without the key, then not at
	// all.
	var keysLine, modelTag string
	for _, tag := range []string{
		stKey.Render("ctrl+o") + " " + stDim.Render("◆ "+m.modelLabel()) + " ",
		stDim.Render("◆ "+m.modelLabel()) + " ",
		"",
	} {
		modelTag = tag
		keysLine = m.renderKeysFit(m.width - lipgloss.Width(tag) - 2)
		if lipgloss.Width(keysLine)+lipgloss.Width(tag)+2 <= m.width {
			break
		}
	}
	gap := max(m.width-lipgloss.Width(keysLine)-lipgloss.Width(modelTag), 0)
	line := keysLine + strings.Repeat(" ", gap) + modelTag

	separator := stSep.Render(strings.Repeat("─", m.width))

	return statusLine + "\n" + separator + "\n" +
		lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

// keyHint is one shortcut in the key bar. A hint marked drop is the first
// to go when the bar does not fit the terminal; the rest always show.
type keyHint struct {
	k, d string
	drop bool
}

// keyHints returns the current screen's shortcuts in two groups: the
// actions on what is selected, and the ways to move around. Keys that are
// obvious (arrows on a list) or rare (setup, model) are left to the help.
func (m model) keyHints() (actions, nav []keyHint) {
	switch m.screen {
	case screenNewPlan:
		if m.isLoading {
			return nil, []keyHint{{k: "ctrl+c", d: "quit"}}
		}
		return []keyHint{{k: "ctrl+s", d: "generate"}},
			[]keyHint{{k: "tab", d: "next field"}, {k: "esc", d: "cancel"}}
	case screenModels:
		return []keyHint{{k: "enter", d: "select"}, {k: "r", d: "refresh"}},
			[]keyHint{{k: "esc/ctrl+o", d: "close"}}
	case screenRoad:
		return []keyHint{{k: "enter", d: "open tree"}},
			[]keyHint{{k: "hjkl", d: "move", drop: true}, {k: "c", d: "back"}, {k: "?", d: "help"}}
	case screenTree:
		actions := []keyHint{{k: "enter", d: "complete directive"}}
		if m.canAskDirective() {
			actions = append(actions, keyHint{k: "g", d: "new directive"})
		}
		return append(actions, keyHint{k: "e", d: "edit skill", drop: true}),
			[]keyHint{{k: "←→", d: "skill"}, {k: "↑↓", d: "node"}, {k: "t", d: "back"}, {k: "?", d: "help"}}
	case screenOnboard:
		f := m.onboard
		switch {
		case m.isLoading || f.building:
			return nil, []keyHint{{k: "ctrl+c", d: "quit"}}
		case f.field == fieldList:
			return []keyHint{{k: "f", d: "toggle active/frozen"}, {k: "enter", d: "edit"}, {k: "ctrl+d", d: "remove", drop: true}},
				[]keyHint{{k: "ctrl+r", d: "focus form"}, {k: "ctrl+s", d: "build trees"}, {k: "?", d: "help", drop: true}, {k: "esc", d: "skip", drop: true}}
		case f.editing >= 0:
			return []keyHint{{k: "enter", d: "save skill"}, {k: "tab", d: "next field", drop: true}},
				[]keyHint{{k: "esc", d: "cancel edit"}, {k: "?", d: "help", drop: true}}
		}
		return []keyHint{{k: "enter", d: "add skill"}, {k: "tab", d: "next field", drop: true}},
			[]keyHint{{k: "ctrl+r", d: "focus roster"}, {k: "ctrl+s", d: "build trees"}, {k: "?", d: "help", drop: true}, {k: "esc", d: "skip", drop: true}}
	case screenHelp:
		return nil, []keyHint{{k: "←→", d: "section"}, {k: "esc", d: "close"}}
	case screenSkillEdit:
		return []keyHint{{k: "enter", d: "save"}}, []keyHint{{k: "esc", d: "cancel"}}
	}

	if m.confirmDelete != nil {
		return []keyHint{{k: "y", d: "delete"}}, []keyHint{{k: "any key", d: "cancel"}}
	}
	// Hints marked drop go from the last one back, so the order below is
	// also the order they give way in: tree, then character, then the rest.
	g := m.dailyKeyLabel()
	if m.mainFocus == focusDirectives {
		actions = []keyHint{{k: "enter", d: "complete"}, {k: "r", d: "reroll"}}
		if g != "" {
			actions = append(actions, keyHint{k: "g", d: g})
		}
		nav = []keyHint{{k: "tab", d: "plans"}, {k: "ctrl+n", d: "new plan", drop: true}}
	} else {
		actions = []keyHint{{k: "ctrl+n", d: "new"}, {k: "e", d: "edit"}, {k: "d", d: "delete"}}
		nav = []keyHint{{k: "tab", d: "directives"}}
		if g != "" {
			nav = append(nav, keyHint{k: "g", d: g, drop: true})
		}
	}
	nav = append(nav,
		keyHint{k: "c", d: "character", drop: true},
		keyHint{k: "t", d: "tree", drop: true},
		keyHint{k: "h", d: "help"},
		keyHint{k: "q", d: "quit"},
	)
	return actions, nav
}

// renderKeys draws the whole key bar, however wide it is.
func (m model) renderKeys() string {
	actions, nav := m.keyHints()
	return drawHints(actions, nav, false)
}

// renderKeysFit draws the key bar in at most width cells: it drops the
// droppable hints from the end, then tightens the spacing.
func (m model) renderKeysFit(width int) string {
	actions, nav := m.keyHints()
	line := drawHints(actions, nav, false)
	for lipgloss.Width(line) > width {
		var ok bool
		if nav, ok = dropLast(nav); !ok {
			if actions, ok = dropLast(actions); !ok {
				break
			}
		}
		line = drawHints(actions, nav, false)
	}
	if lipgloss.Width(line) > width {
		line = drawHints(actions, nav, true)
	}
	return line
}

// dropLast removes the last droppable hint. ok is false when none is left.
func dropLast(hints []keyHint) ([]keyHint, bool) {
	for i := len(hints) - 1; i >= 0; i-- {
		if hints[i].drop {
			return append(hints[:i:i], hints[i+1:]...), true
		}
	}
	return hints, false
}

// drawHints joins the hints with spaces, the two groups with a faint bar.
// compact uses less space between them.
func drawHints(actions, nav []keyHint, compact bool) string {
	gap, bar := "   ", "   │   "
	if compact {
		gap, bar = "  ", " │ "
	}
	group := func(hints []keyHint) string {
		parts := make([]string, 0, len(hints))
		for _, h := range hints {
			parts = append(parts, stKey.Render(h.k)+" "+stDim.Render(h.d))
		}
		return strings.Join(parts, gap)
	}
	var groups []string
	for _, g := range [][]keyHint{actions, nav} {
		if len(g) > 0 {
			groups = append(groups, group(g))
		}
	}
	return " " + strings.Join(groups, stSep.Render(bar))
}

// modelLabel is the selected model's name, without the default ":latest"
// tag.
func (m model) modelLabel() string {
	if m.selectedModel == "" {
		return "no model"
	}
	return strings.TrimSuffix(m.selectedModel, ":latest")
}
