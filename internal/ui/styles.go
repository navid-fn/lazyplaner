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
