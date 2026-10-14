package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// panel draws a rounded box with a title embedded in the top border.
// outerW and outerH are total dimensions including the border itself.
func panel(title, content string, outerW, outerH int, active bool) string {
	bc := colorInactive
	if active {
		bc = colorActive
	}
	bSt := lipgloss.NewStyle().Foreground(bc)
	tSt := lipgloss.NewStyle().Foreground(bc).Bold(true)

	iW := outerW - 2 // inner width (excludes left/right border chars)
	iH := outerH - 2 // inner height (excludes top/bottom border)

	// ╭─ Title ─────────────────────────╮
	// A title too long for the border is clipped, so it can't widen the box.
	ts := ansi.Truncate(" "+title+" ", max(iW-1, 0), "… ")
	dash := iW - 1 - lipgloss.Width(ts)
	if dash < 0 {
		dash = 0
	}
	top := bSt.Render("╭─") + tSt.Render(ts) + bSt.Render(strings.Repeat("─", dash)+"╮")

	// Normalize content to exactly iH lines.
	lines := strings.Split(content, "\n")
	for len(lines) < iH {
		lines = append(lines, "")
	}
	if len(lines) > iH {
		lines = lines[:iH]
	}

	rows := make([]string, iH)
	for i, line := range lines {
		// Clip anything wider than the inner area so it can't push the
		// right border out of line.
		line = lipgloss.NewStyle().MaxWidth(iW).Render(line)
		lw := lipgloss.Width(line)
		pad := ""
		if lw < iW {
			pad = strings.Repeat(" ", iW-lw)
		}
		rows[i] = bSt.Render("│") + line + pad + bSt.Render("│")
	}

	bottom := bSt.Render("╰" + strings.Repeat("─", iW) + "╯")

	parts := make([]string, 0, 2+iH)
	parts = append(parts, top)
	parts = append(parts, rows...)
	parts = append(parts, bottom)
	return strings.Join(parts, "\n")
}
