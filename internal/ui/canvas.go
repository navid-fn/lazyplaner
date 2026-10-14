package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Canvas ──────────────────────────────────────────────────────────────────
//
// canvas is a grid of single-width cells, each with its own color. The skill
// tree map is drawn onto one and then cut to the visible window. Connector
// lines are stored as direction bits and turned into box-drawing characters
// when the canvas is rendered, so crossing and branching lines join cleanly.

// Directions a connector leaves a cell in.
const (
	dirUp uint8 = 1 << iota
	dirDown
	dirLeft
	dirRight
)

type cell struct {
	r    rune
	fg   string // color as lipgloss accepts it; empty means the default
	bold bool
	mask uint8 // connector directions; when set, the rune comes from mask
}

type canvas struct {
	w, h  int
	cells [][]cell
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: max(w, 0), h: max(h, 0)}
	c.cells = make([][]cell, c.h)
	for y := range c.cells {
		row := make([]cell, c.w)
		for x := range row {
			row[x].r = ' '
		}
		c.cells[y] = row
	}
	return c
}

func (c *canvas) inside(x, y int) bool {
	return x >= 0 && y >= 0 && x < c.w && y < c.h
}

// set places one character, replacing any connector in that cell.
func (c *canvas) set(x, y int, r rune, fg string, bold bool) {
	if c.inside(x, y) {
		c.cells[y][x] = cell{r: r, fg: fg, bold: bold}
	}
}

// text writes s from x on row y and returns the column after it. Every rune
// is assumed to be one cell wide.
func (c *canvas) text(x, y int, s, fg string, bold bool) int {
	for _, r := range s {
		c.set(x, y, r, fg, bold)
		x++
	}
	return x
}

// link adds connector directions to a cell. It only draws on empty cells or
// cells that already hold a connector, so lines never cut through cards.
// The color is replaced each time, so the last line drawn sets it.
func (c *canvas) link(x, y int, dirs uint8, fg string) {
	if !c.inside(x, y) {
		return
	}
	cl := &c.cells[y][x]
	if cl.mask == 0 && cl.r != ' ' {
		return
	}
	cl.mask |= dirs
	cl.fg = fg
}

// bar draws a progress bar of width cells starting at x.
func (c *canvas) bar(x, y int, frac float64, width int, from, to string) {
	filled := barFill(frac, width)
	for i := 0; i < width; i++ {
		if i < filled {
			c.set(x+i, y, '━', gradientAt(from, to, i, width), false)
		} else {
			c.set(x+i, y, '┈', colorTrack, false)
		}
	}
}

// joinGlyph turns connector directions into a box-drawing character. Single
// corners are rounded to match the cards.
func joinGlyph(m uint8) rune {
	switch m {
	case dirLeft, dirRight, dirLeft | dirRight:
		return '─'
	case dirUp, dirDown, dirUp | dirDown:
		return '│'
	case dirDown | dirRight:
		return '╭'
	case dirUp | dirRight:
		return '╰'
	case dirDown | dirLeft:
		return '╮'
	case dirUp | dirLeft:
		return '╯'
	case dirUp | dirDown | dirRight:
		return '├'
	case dirUp | dirDown | dirLeft:
		return '┤'
	case dirLeft | dirRight | dirDown:
		return '┬'
	case dirLeft | dirRight | dirUp:
		return '┴'
	default:
		return '┼'
	}
}

// window renders the part of the canvas starting at (x0, y0), w cells wide
// and h rows high. Offsets may be negative or run past the canvas, which
// pads with spaces; that is how a small map is centered in a large window.
func (c *canvas) window(x0, y0, w, h int) []string {
	styles := map[string]lipgloss.Style{}
	styleFor := func(fg string, bold bool) lipgloss.Style {
		key := fmt.Sprintf("%s|%t", fg, bold)
		st, ok := styles[key]
		if !ok {
			st = lipgloss.NewStyle().Bold(bold)
			if fg != "" {
				st = st.Foreground(lipgloss.Color(fg))
			}
			styles[key] = st
		}
		return st
	}

	lines := make([]string, h)
	for row := 0; row < h; row++ {
		var b strings.Builder
		var run strings.Builder
		runFg, runBold := "", false

		flush := func() {
			if run.Len() == 0 {
				return
			}
			if runFg == "" && !runBold {
				b.WriteString(run.String())
			} else {
				b.WriteString(styleFor(runFg, runBold).Render(run.String()))
			}
			run.Reset()
		}

		for col := 0; col < w; col++ {
			x, y := x0+col, y0+row
			r, fg, bold := ' ', "", false
			if c.inside(x, y) {
				cl := c.cells[y][x]
				r, fg, bold = cl.r, cl.fg, cl.bold
				if cl.mask != 0 {
					r, bold = joinGlyph(cl.mask), false
				}
			}
			if r == ' ' {
				fg, bold = "", false
			}
			if fg != runFg || bold != runBold {
				flush()
				runFg, runBold = fg, bold
			}
			run.WriteRune(r)
		}
		flush()
		lines[row] = b.String()
	}
	return lines
}

// ─── Color helpers ───────────────────────────────────────────────────────────

// colorTrack is the empty part of a progress bar.
const colorTrack = "239"

// parseHex reads a #rrggbb color. ANSI color numbers are not hex and fail.
func parseHex(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 0, 0, 0, false
	}
	return r, g, b, true
}

// mixColor blends a toward b by t, from 0 to 1. If either is not a hex
// color, a is returned unchanged.
func mixColor(a, b string, t float64) string {
	ar, ag, ab, ok1 := parseHex(a)
	br, bg, bb, ok2 := parseHex(b)
	if !ok1 || !ok2 {
		return a
	}
	t = max(0, min(1, t))
	mix := func(x, y int) int { return x + int(float64(y-x)*t+0.5) }
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
}

// lighten blends a color toward white.
func lighten(c string, t float64) string {
	return mixColor(c, "#ffffff", t)
}

// gradientAt is the color of cell i out of width along a gradient.
func gradientAt(from, to string, i, width int) string {
	if width <= 1 {
		return from
	}
	return mixColor(from, to, float64(i)/float64(width-1))
}

// barFill is how many of width cells a fraction fills.
func barFill(frac float64, width int) int {
	frac = max(0, min(1, frac))
	return int(frac*float64(width) + 0.5)
}

// gradientBar draws a thin progress bar whose filled part fades from one
// color to another. The empty part is a dotted track, so an empty bar still
// reads as a bar and not as a separator line.
func gradientBar(frac float64, width int, from, to string) string {
	filled := barFill(frac, width)
	var b strings.Builder
	for i := 0; i < filled; i++ {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(gradientAt(from, to, i, width))).Render("━"))
	}
	if filled < width {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorTrack)).Render(strings.Repeat("┈", width-filled)))
	}
	return b.String()
}

// gradientText colors each character of s along a gradient, in bold.
func gradientText(s, from, to string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		b.WriteString(lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(gradientAt(from, to, i, len(runes)))).Render(string(r)))
	}
	return b.String()
}
