package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Sizes on the character screen, in terminal cells.
const (
	heroH         = 7  // header panel, borders included
	skillCardH    = 6  // border, four content lines, border
	minSkillCardW = 38 // narrowest skill card; fewer columns below this
	gridGap       = 2  // columns between skill cards
	attrW         = 46 // width of the attributes block in the header
)

// attributes names each life area like an RPG stat.
var attributes = map[string]string{
	"Mind":   "INT",
	"Body":   "STR",
	"Craft":  "DEX",
	"Social": "CHA",
	"Life":   "WIS",
}

// sparkChars draw a bar's height in eighths, for the weekly sparkline.
var sparkChars = []rune("▁▂▃▄▅▆▇█")

// ─── Keys ────────────────────────────────────────────────────────────────────

// openRoad switches to the character screen and reloads skills and events.
func (m *model) openRoad() tea.Cmd {
	m.screen = screenRoad
	m.errorText = ""
	m.statusText = ""
	m.charCursor = clamp(m.charCursor, 0, max(len(m.skills)-1, 0))
	return loadRoadCmd(m.store)
}

func (m model) handleRoadKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.skills)
	cols := skillGridCols(m.skillsPanelW() - 2)

	switch msg.String() {
	case "esc", "c":
		m.screen = screenMain
	case "q":
		return m, tea.Quit
	case "?":
		m.openHelp()
	// Arrows and vim keys move around the skill cards.
	case "left", "h":
		m.charCursor = max(m.charCursor-1, 0)
	case "right", "l":
		m.charCursor = min(m.charCursor+1, max(n-1, 0))
	case "up", "k":
		if m.charCursor-cols >= 0 {
			m.charCursor -= cols
		}
	case "down", "j":
		if m.charCursor+cols < n {
			m.charCursor += cols
		}
	case "enter":
		// Open the selected skill's tree.
		if n > 0 {
			m.treeSkill = m.charCursor
			m.openTree()
		}
	}
	return m, nil
}

func (m *model) setRoad(skills []db.Skill, nodes []db.Node, events []db.XPEvent) {
	m.skills = skills
	m.nodes = nodes
	m.xpEvents = events
	m.charCursor = clamp(m.charCursor, 0, max(len(skills)-1, 0))
}

// ─── Screen ──────────────────────────────────────────────────────────────────

// skillsPanelW is the width of the skills panel; the road takes the rest.
func (m model) skillsPanelW() int {
	return clamp(m.width*3/5, 40, m.width-30)
}

// renderRoadScreen draws the character header across the top, the skill
// cards below on the left, and the road on the right.
func (m model) renderRoadScreen() string {
	bodyH := m.bodyHeight()
	hero := panel("Character", m.renderHero(m.width-2), m.width, heroH, true)

	lowerH := max(bodyH-heroH, 6)
	leftW := m.skillsPanelW()
	rightW := m.width - leftW
	skills := panel("Skills", m.renderSkillGrid(leftW-2, lowerH-2), leftW, lowerH, true)
	road := panel("Road", m.renderRoadLog(rightW-2, lowerH-2), rightW, lowerH, false)

	return hero + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, skills, road)
}

// ─── Header ──────────────────────────────────────────────────────────────────

// renderHero is the character header: level, XP, next rank and stats on the
// left, and the attribute bars on the right when there is room.
func (m model) renderHero(width int) string {
	total := 0
	for _, sk := range m.skills {
		total += sk.TotalXP
	}
	p := rpg.ProgressFor(total)
	rank := rpg.RankTitle(p.Level)

	showAttrs := width >= 110
	leftW := width
	if showAttrs {
		leftW = width - attrW - 3
	}

	barW := clamp(leftW-34, 10, 120)
	nextRank := stSuccess.Render("★ Highest rank reached.")
	if title, from, ok := rpg.NextRank(p.Level); ok {
		nextRank = stDim.Render("Next rank ") + stActive.Render("▸ "+title) +
			stDim.Render(fmt.Sprintf("  at Lv %d · %d xp to go", from, rpg.TotalXPForLevel(from)-total))
	}

	done, all := m.nodesDone()
	st := m.challengeStats
	left := []string{
		" " + chipLabel(fmt.Sprintf("LV %d", p.Level), colorActive) + "  " +
			stActive.Render(strings.ToUpper(rank)) + stDim.Render(fmt.Sprintf("   %d xp total", total)),
		" " + gradientBar(p.Fraction, barW, string(colorActive), "#00e5ff") +
			"  " + stActive.Render(fmt.Sprint(p.Into)) + stDim.Render(fmt.Sprintf(" / %d xp to Lv %d", p.Span, p.Level+1)),
		" " + nextRank,
		"",
		" " + chipLabel(fmt.Sprintf("streak %d day%s", st.Streak, plural(st.Streak)), colorKeyLabel) + " " +
			chipLabel(fmt.Sprintf("score %d", st.Score), colorSuccess) + " " +
			chipLabel(fmt.Sprintf("nodes %d/%d", done, all), lipgloss.Color("#8be9fd")) + " " +
			chipLabel(fmt.Sprintf("skills %d", len(m.skills)), lipgloss.Color("#bd93f9")),
	}

	block := lipgloss.NewStyle().Width(leftW).Render(strings.Join(left, "\n"))
	if !showAttrs {
		return block
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).
		Render(strings.TrimSuffix(strings.Repeat("│\n", len(left)), "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, block, " ", divider, " ", m.renderAttributes(attrW))
}

// renderAttributes shows XP per life area as stat bars. The strongest area
// fills its bar and the others are drawn relative to it.
func (m model) renderAttributes(width int) string {
	xp := map[string]int{}
	best := 1
	for _, sk := range m.skills {
		xp[sk.Category] += sk.TotalXP
		best = max(best, xp[sk.Category])
	}

	barW := width - 20
	lines := make([]string, 0, len(rpg.Categories))
	for _, cat := range rpg.Categories {
		c := string(categoryColor(cat))
		frac := float64(xp[cat]) / float64(best)
		lines = append(lines,
			lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Bold(true).Render(attributes[cat])+" "+
				stDim.Render(fmt.Sprintf("%-6s", cat))+" "+
				gradientBar(frac, barW, c, lighten(c, 0.5))+" "+
				stDim.Render(fmt.Sprintf("%6d", xp[cat])))
	}
	return strings.Join(lines, "\n")
}

// nodesDone counts finished nodes across every skill tree.
func (m model) nodesDone() (done, all int) {
	for _, sk := range m.skills {
		nodes := m.nodesFor(sk.ID)
		statuses := rpg.NodeStatuses(toTreeNodes(nodes), rpg.ProgressFor(sk.TotalXP).Level)
		for _, n := range nodes {
			all++
			if statuses[n.ID] == rpg.Done {
				done++
			}
		}
	}
	return done, all
}

// ─── Skill cards ─────────────────────────────────────────────────────────────

// skillGridCols is how many skill cards fit side by side.
func skillGridCols(width int) int {
	return clamp((width+gridGap)/(minSkillCardW+gridGap), 1, 3)
}

// renderSkillGrid lays the skills out as cards. Rows spread out to fill the
// panel when there is room, and the grid scrolls to keep the selected card
// in view when there is not.
func (m model) renderSkillGrid(width, height int) string {
	if len(m.skills) == 0 {
		return " " + stDim.Render("No skills yet. Press s to create your character.")
	}

	cols := skillGridCols(width)
	cardW := (width - gridGap*(cols-1)) / cols
	rows := (len(m.skills) + cols - 1) / cols

	gap := 1
	if rows > 1 {
		gap = clamp((height-rows*skillCardH)/(rows-1), 1, 3)
	}
	visible := max((height+gap)/(skillCardH+gap), 1)
	selRow := m.charCursor / cols
	first := 0
	if selRow >= visible {
		first = selRow - visible + 1
	}

	var out []string
	for r := first; r < min(rows, first+visible); r++ {
		var cards []string
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(m.skills) {
				break
			}
			if c > 0 {
				cards = append(cards, strings.Repeat(" ", gridGap))
			}
			cards = append(cards, m.skillCard(m.skills[i], cardW, i == m.charCursor))
		}
		if r > first {
			out = append(out, strings.Repeat("\n", gap-1))
		}
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	return strings.Join(out, "\n")
}

// skillCard draws one skill: level and XP, a bar to the next level, its node
// counts, and what it unlocks next.
func (m model) skillCard(sk db.Skill, width int, selected bool) string {
	sp := rpg.ProgressFor(sk.TotalXP)
	cat := string(categoryColor(sk.Category))
	inner := width - 4

	nodes := m.nodesFor(sk.ID)
	rows := flattenTree(nodes, rpg.NodeStatuses(toTreeNodes(nodes), sp.Level))
	done, open, locked := countStatuses(rows)

	xp := stDim.Render(fmt.Sprintf("%d xp", sk.TotalXP))
	line1 := spread(chipLabel(fmt.Sprintf("LV %d", sp.Level), lipgloss.Color(cat))+" "+
		stDim.Render(rpg.RankTitle(sp.Level)), xp, inner)

	nums := fmt.Sprintf(" %d/%d", sp.Into, sp.Span)
	line2 := gradientBar(sp.Fraction, max(inner-len(nums), 4), cat, lighten(cat, 0.5)) + stDim.Render(nums)

	line3 := stSuccess.Render(fmt.Sprintf("✓ %d", done)) + "  " +
		stActive.Render(fmt.Sprintf("◆ %d", open)) + "  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(inkLocked)).Render(fmt.Sprintf("◇ %d", locked)) +
		stDim.Render("  nodes")

	line4 := ansi.Truncate(skillNext(sk, rows), inner, "…")

	border, title := cat, cat
	if selected {
		border = inkSelect
	}
	return cardFrame(sk.Name, sk.Category, cat, []string{line1, line2, line3, line4}, width, border, title, selected)
}

// skillNext is a one-line hint at what the skill opens up next.
func skillNext(sk db.Skill, rows []treeRow) string {
	if len(rows) == 0 {
		return stDim.Render("No tree yet")
	}
	if allDone(rows) {
		return stSuccess.Render("★ Mastered")
	}
	for _, r := range rows {
		if r.status == rpg.Rusty {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Bold(true).
				Render("⚠ rusty ▸ "+r.node.Name) + stDim.Render(fmt.Sprintf(" · ×%.2f comeback", rpg.ComebackMultiplier))
		}
	}
	status := statusByID(rows)
	best := -1
	for i, r := range rows {
		parentDone := r.node.ParentID == 0 || status[r.node.ParentID] == rpg.Done
		if r.status == rpg.Locked && parentDone &&
			(best < 0 || r.node.RequiredLevel < rows[best].node.RequiredLevel) {
			best = i
		}
	}
	if best < 0 {
		return stDim.Render("Finish open nodes to go further")
	}
	n := rows[best].node
	name := nodeName(n, rpg.Locked, rpg.ProgressFor(sk.TotalXP).Level)
	return stDim.Render("next ") + stActive.Render("▸ "+name) + stDim.Render(fmt.Sprintf(" at Lv %d", n.RequiredLevel))
}

// cardFrame draws a bordered card with a title on the left of the top border
// and a label in labelInk on the right. A thick border marks the selected
// card.
func cardFrame(title, label, labelInk string, lines []string, width int, borderInk, titleInk string, thick bool) string {
	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	if thick {
		tl, tr, bl, br, hz, vt = "┏", "┓", "┗", "┛", "━", "┃"
	}
	bst := lipgloss.NewStyle().Foreground(lipgloss.Color(borderInk))
	inner := width - 4

	head := lipgloss.NewStyle().Foreground(lipgloss.Color(titleInk)).Bold(true).
		Render(" " + truncRunes(title, max(width-lipgloss.Width(label)-10, 4)) + " ")
	tag := lipgloss.NewStyle().Foreground(lipgloss.Color(labelInk)).Bold(true).Render(" " + label + " ")
	fill := max(width-4-lipgloss.Width(head)-lipgloss.Width(tag), 1)

	out := []string{bst.Render(tl+hz) + head + bst.Render(strings.Repeat(hz, fill)) + tag + bst.Render(hz+tr)}
	for _, l := range lines {
		pad := max(inner-lipgloss.Width(l), 0)
		out = append(out, bst.Render(vt)+" "+l+strings.Repeat(" ", pad)+" "+bst.Render(vt))
	}
	out = append(out, bst.Render(bl+strings.Repeat(hz, width-2)+br))
	return strings.Join(out, "\n")
}

// ─── Road ────────────────────────────────────────────────────────────────────

// renderRoadLog shows the last seven days as a sparkline, then every award
// newest first, grouped under a header per day.
func (m model) renderRoadLog(width, height int) string {
	if len(m.xpEvents) == 0 {
		return " " + stDim.Render("Your road is empty.\n\n Save a plan with ctrl+n or finish a\n daily challenge with g, and the XP\n it earns shows up here.")
	}

	now := time.Now()
	perDay := map[string]int{}
	for _, ev := range m.xpEvents {
		perDay[ev.CreatedAt.Local().Format("2006-01-02")] += ev.XP
	}

	lines := m.weekSpark(now, perDay, width)
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", width)))

	lastDay := ""
	for _, ev := range m.xpEvents {
		if len(lines) >= height {
			break
		}
		when := ev.CreatedAt.Local()
		day := when.Format("2006-01-02")
		if day != lastDay {
			if lastDay != "" && len(lines) < height-1 {
				lines = append(lines, "")
			}
			lines = append(lines, dayHeader(dayLabel(when, now), perDay[day], width))
			lastDay = day
			if len(lines) >= height {
				break
			}
		}
		lines = append(lines, roadEntry(ev, width))
	}
	return strings.Join(lines, "\n")
}

// weekSpark draws a bar per day for the last seven days, with the weekday
// letter under each bar and the week's total beside it.
func (m model) weekSpark(now time.Time, perDay map[string]int, width int) []string {
	best, week := 0, 0
	var days []time.Time
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		days = append(days, d)
		xp := perDay[d.Format("2006-01-02")]
		best = max(best, xp)
		week += xp
	}

	var bars, letters strings.Builder
	for i, d := range days {
		xp := perDay[d.Format("2006-01-02")]
		level := 0
		if best > 0 && xp > 0 {
			level = clamp(xp*len(sparkChars)/best, 1, len(sparkChars)) - 1
		}
		ink := lighten(string(colorActive), 0.6*float64(6-i)/6) // today is brightest
		if xp == 0 {
			bars.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorTrack)).Render("▁") + "  ")
		} else {
			bars.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ink)).Render(string(sparkChars[level])) + "  ")
		}
		letters.WriteString(stDim.Render(d.Format("Mon")[:1]) + "  ")
	}

	summary := stSuccess.Render(fmt.Sprintf("+%d xp", week)) + stDim.Render(" this week")
	return []string{
		" " + stDim.Render("7 days ") + " " + bars.String() + " " + summary,
		" " + strings.Repeat(" ", 8) + letters.String(),
	}
}

// dayHeader is the line above one day's awards: its name, a rule, and the
// XP earned that day.
func dayHeader(label string, xp int, width int) string {
	left := " " + stActive.Render(label) + " "
	right := " " + stSuccess.Render(fmt.Sprintf("+%d xp", xp)) + " "
	rule := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", rule)) + right
}

// dayLabel names a day relative to now.
func dayLabel(t, now time.Time) string {
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location())
	day := time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location())
	switch today.Sub(day) {
	case 0:
		return "Today"
	case 24 * time.Hour:
		return "Yesterday"
	}
	return t.Format("Mon Jan 2")
}

// roadEntry is one award on one line: the skill's color, the XP, whether it
// came from a challenge (★) or a plan (✎), the skill, what was done, and the
// time.
func roadEntry(ev db.XPEvent, width int) string {
	icon, activity := stDim.Render("✎"), ev.Activity
	if rest, ok := strings.CutPrefix(ev.Activity, "Challenge: "); ok {
		icon, activity = stKey.Render("★"), rest
	}
	cat := categoryStyle(ev.Category)
	left := "  " + cat.Render("●") + " " +
		stSuccess.Render(fmt.Sprintf("%-4s", fmt.Sprintf("+%d", ev.XP))) + " " + icon + " " +
		cat.Render(fmt.Sprintf("%-9s", truncRunes(ev.SkillName, 9))) + " "
	clock := " " + stDim.Render(ev.CreatedAt.Local().Format("15:04")) + " "

	room := max(width-lipgloss.Width(left)-lipgloss.Width(clock), 4)
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(truncRunes(activity, room))
	gap := max(room-lipgloss.Width(text), 0)
	return left + text + strings.Repeat(" ", gap) + clock
}

// ─── Shared ──────────────────────────────────────────────────────────────────

// renderBar draws a thin progress bar in color, fading lighter toward the
// filled end. frac is clamped to 0..1.
func renderBar(frac float64, width int, color lipgloss.Color) string {
	c := string(color)
	return gradientBar(frac, width, c, lighten(c, 0.45))
}

// comebackNote is added to a status line when an award cleared rust.
func comebackNote(ev db.XPEvent) string {
	if !ev.Comeback {
		return ""
	}
	return fmt.Sprintf(" · comeback ×%.2f, rust cleared", rpg.ComebackMultiplier)
}

// planSavedStatus is the status line after a plan is saved. It reports the
// XP award, or why no XP was given.
func planSavedStatus(msg planSavedMsg) string {
	if msg.xp != nil {
		return fmt.Sprintf("Plan saved · +%d XP in %s (%s)%s", msg.xp.XP, msg.xp.SkillName, msg.xp.Category, comebackNote(*msg.xp))
	}
	if msg.xpErr != nil {
		return fmt.Sprintf("Plan saved · no XP: %v", msg.xpErr)
	}
	return "Plan saved."
}
