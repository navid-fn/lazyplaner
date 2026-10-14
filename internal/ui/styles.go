package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	colorActive   = lipgloss.Color("#aaff00")
	colorInactive = lipgloss.Color("238")
	colorDim      = lipgloss.Color("243")
	colorError    = lipgloss.Color("#ff5555")
	colorSuccess  = lipgloss.Color("#50fa7b")
	colorKeyLabel = lipgloss.Color("#f1fa8c")
	colorSep      = lipgloss.Color("237")

	stDim     = lipgloss.NewStyle().Foreground(colorDim)
	stActive  = lipgloss.NewStyle().Foreground(colorActive).Bold(true)
	stError   = lipgloss.NewStyle().Foreground(colorError)
	stSuccess = lipgloss.NewStyle().Foreground(colorSuccess)
	stKey     = lipgloss.NewStyle().Foreground(colorKeyLabel).Bold(true)
	stSep     = lipgloss.NewStyle().Foreground(colorSep)
)

// stChip styles a key name in the key hints, like a keycap.
var stChip = lipgloss.NewStyle().
	Background(lipgloss.Color("237")).
	Foreground(colorKeyLabel).
	Bold(true)

// categoryColors gives each life area its own color on the character screen.
var categoryColors = map[string]lipgloss.Color{
	"Mind":   lipgloss.Color("#8be9fd"),
	"Body":   lipgloss.Color("#ff79c6"),
	"Craft":  lipgloss.Color("#ffb86c"),
	"Social": lipgloss.Color("#bd93f9"),
	"Life":   lipgloss.Color("#50fa7b"),
}

func categoryColor(category string) lipgloss.Color {
	if c, ok := categoryColors[category]; ok {
		return c
	}
	return colorDim
}

func categoryStyle(category string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(categoryColor(category)).Bold(true)
}

// chipLabel draws text as a filled label in color, like a badge.
func chipLabel(text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().
		Background(color).
		Foreground(lipgloss.Color(inkDark)).
		Bold(true).
		Padding(0, 1).
		Render(text)
}
