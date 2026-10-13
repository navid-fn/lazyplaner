package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay draws fg centered on top of bg. Cells outside fg keep the content
// of bg, so the screen behind a popup stays visible. Both strings may contain
// ANSI styling; width and height are the size of the area being covered.
func overlay(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	fgLines := strings.Split(fg, "\n")
	fgW := 0
	for _, l := range fgLines {
		fgW = max(fgW, lipgloss.Width(l))
	}
	fgH := len(fgLines)
	x := max((width-fgW)/2, 0)
	y := max((height-fgH)/2, 0)

	for i, fl := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bl := bgLines[row]
		if w := lipgloss.Width(bl); w < x+fgW {
			bl += strings.Repeat(" ", x+fgW-w)
		}
		left := ansi.Cut(bl, 0, x)
		right := ansi.Cut(bl, x+fgW, lipgloss.Width(bl))
		// Reset styling so the popup's colors don't leak into the right side.
		bgLines[row] = left + "\x1b[0m" + fl + "\x1b[0m" + right
	}
	return strings.Join(bgLines[:height], "\n")
}
