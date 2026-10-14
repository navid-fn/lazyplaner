package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// homeFocus is the left-column panel that receives keys on the home screen.
type homeFocus int

const (
	focusDirectives homeFocus = iota // today's challenges
	focusPlans                       // the plan list
)

// Sizes on the home screen, in terminal cells.
const (
	hudH          = 1 // the character HUD above the panels
	directivesMax = 9 // borders, progress line, four directives, the bonus and a footer
	recentXPCount = 3 // awards shown in the briefing's feed
)

// homeHeights splits the body: the HUD line, then the panels. The left
// column stacks the directives (fixed, so the plan list keeps a steady size)
// over the plans.
func (m model) homeHeights() (panelsH, directivesH, plansH int) {
	panelsH = max(m.bodyHeight()-hudH, 6)
	directivesH = clamp(panelsH/2, 4, directivesMax)
	return panelsH, directivesH, panelsH - directivesH
}

// ─── Keys ────────────────────────────────────────────────────────────────────

// handleHomeKey handles the keys that depend on which left panel has focus.
// It returns handled false for keys the plan list or global shortcuts should
// see instead.
func (m model) handleHomeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if msg.String() == "tab" {
		if m.mainFocus == focusDirectives {
			m.mainFocus = focusPlans
		} else {
			m.mainFocus = focusDirectives
		}
		return m, nil, true
	}
	if m.mainFocus != focusDirectives {
		return m, nil, false
	}

	switch msg.String() {
	case "up", "k":
		m.challengeCursor = max(m.challengeCursor-1, 0)
		return m, nil, true
	case "down", "j":
		m.challengeCursor = clamp(m.challengeCursor+1, 0, max(len(m.challenges)-1, 0))
		return m, nil, true
	case "enter", " ":
		if m.isLoading {
			return m, nil, true
		}
		next, cmd := m.completeSelectedChallenge()
		return next.(model), cmd, true
	case "r":
		cmd := m.rerollSelected()
		return m, cmd, true
	case "d", "e":
		// Plan actions only apply where the plans are.
		m.statusText = "Press tab to move to the plans first."
		return m, nil, true
	}
	return m, nil, false
}

// ─── Screen ──────────────────────────────────────────────────────────────────

// renderHome is the command center: the character HUD on top, today's
// directives over the plan list on the left, and the briefing or the
// selected plan on the right.
func (m model) renderHome() string {
	panelsH, dirH, plansH := m.homeHeights()
	leftW, rightW := m.columnWidths()

	left := lipgloss.JoinVertical(lipgloss.Left,
		m.renderDirectives(leftW, dirH),
		m.renderPlansPanel(leftW, plansH))

	var right string
	if m.isEditing || (m.mainFocus == focusPlans && len(m.plans) > 0) {
		right = panel("📜 Plan", m.renderPreview(rightW-2), rightW, panelsH, m.isEditing)
	} else {
		right = panel("📊 Strategic Briefing", m.renderBriefing(rightW-2, panelsH-2), rightW, panelsH, false)
	}

	return m.renderHUD() + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderHUD is the one-line character header: level and rank with an XP bar
// on the left, and the day's numbers on the right.
func (m model) renderHUD() string {
	total, active := 0, 0
	for _, sk := range m.skills {
		total += sk.TotalXP
		if sk.IsActive {
			active++
		}
	}
	rusty := 0
	for _, n := range m.nodes {
		if n.IsRusty {
			rusty++
		}
	}
	p := rpg.ProgressFor(total)
	st := m.challengeStats

	left := " " + chipLabel(fmt.Sprintf("LV %d", p.Level), colorActive) + " " +
		stActive.Render(strings.ToUpper(rpg.RankTitle(p.Level))) + "  " +
		gradientBar(p.Fraction, 16, string(colorActive), "#00e5ff") +
		stDim.Render(fmt.Sprintf(" %d/%d xp", p.Into, p.Span))

	sep := stSep.Render("  │  ")
	stats := []string{
		stActive.Render(fmt.Sprintf("⚔️ Active: %d", active)),
		lipgloss.NewStyle().Foreground(colorKeyLabel).Render(fmt.Sprintf("🎯 Today: %d/%d", st.Done, st.Total)),
		lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Render(fmt.Sprintf("🔥 Streak: %dd", st.Streak)),
		stSuccess.Render(fmt.Sprintf("★ Score: %d", st.Score)),
	}
	if rusty > 0 {
		stats = append(stats, lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Bold(true).
			Render(fmt.Sprintf("⚠ Rusty: %d", rusty)))
	}

	line := spread(left, strings.Join(stats, sep)+" ", m.width)
	return ansi.Truncate(line, m.width, "…")
}

// renderDirectives is today's directives as a compact checklist, with a
// progress bar on top. With none yet, it says how to get them. While the
// model works on them, the header carries the spinner and what it is doing.
func (m model) renderDirectives(width, height int) string {
	focused := m.mainFocus == focusDirectives && m.screen == screenMain
	inner := width - 2

	// While a job runs, the title gives up words so the spinner and its
	// text fit even in the narrowest column.
	title := "🎯 Today's Directives"
	if m.dailyBusy != "" {
		title = "🎯 Directives " + m.spinner.View() + " " + stActive.Render(m.dailyBusy)
	}

	if len(m.challenges) == 0 {
		prompt := "Press " + stChip.Render(" g ") + stDim.Render(" to generate today's 3 directives")
		if m.dailyBusy != "" {
			prompt = stDim.Render("Drafting ⚡ Recon · 🔧 Core Op · 🔥 Deep Push…")
		}
		content := "\n" + lipgloss.NewStyle().Width(inner).Align(lipgloss.Center).Render(prompt)
		return panel(title, content, width, height, focused)
	}

	done, earned, total := 0, 0, 0
	for _, c := range m.challenges {
		total += c.XP
		if c.Done {
			done++
			earned += c.XP
		}
	}
	summary := stDim.Render(fmt.Sprintf(" %d/%d · %d/%d xp", done, len(m.challenges), earned, total))
	barW := max(inner-lipgloss.Width(summary)-1, 4)
	lines := []string{" " + gradientBar(float64(done)/float64(len(m.challenges)), barW,
		string(colorActive), string(colorSuccess)) + summary}

	for i, c := range m.challenges {
		pointer := "  "
		if focused && i == m.challengeCursor {
			pointer = stActive.Render("▸ ")
		}
		box, name := stDim.Render("[ ]"), stActive.Render(c.Title)
		if c.Done {
			box, name = stSuccess.Render("[✓]"), stDim.Strikethrough(true).Render(c.Title)
		}
		xp := " " + categoryStyle(c.Category).Render(truncRunes(c.SkillName, 10)) +
			stSuccess.Render(fmt.Sprintf(" +%d", c.XP)) + " "
		left := pointer + box + " "
		if chip := tierChip(c.Tier); chip != "" {
			left += chip + " "
		}
		room := max(inner-lipgloss.Width(left)-lipgloss.Width(xp), 4)
		name = ansi.Truncate(name, room, "…")
		gap := max(room-lipgloss.Width(name), 0)
		lines = append(lines, left+name+strings.Repeat(" ", gap)+xp)
	}

	// Without a set (only focus directives from the tree), point at g;
	// once the set is done, say whether the day's one bonus is left.
	if !hasSet(m.challenges) && m.dailyBusy == "" {
		lines = append(lines, ansi.Truncate(" "+stChip.Render(" g ")+stDim.Render(" generate today's set"), inner, "…"))
	} else if done == len(m.challenges) && m.dailyBusy == "" {
		footer := stSuccess.Render(" ★ Set complete ") + stChip.Render(" g ") + stDim.Render(" 1 bonus")
		if bonusUsed(m.challenges) {
			footer = stSuccess.Render(" ★ Daily cap reached") + stDim.Render(" · new directives tomorrow")
		}
		lines = append(lines, ansi.Truncate(footer, inner, "…"))
	}
	return panel(title, strings.Join(lines, "\n"), width, height, focused)
}

// renderPlansPanel is the plan list, or a hint when there are no plans.
func (m model) renderPlansPanel(width, height int) string {
	focused := m.mainFocus == focusPlans && m.screen == screenMain && !m.isEditing
	content := m.plansList.View()
	if len(m.plans) == 0 {
		content = "\n " + stDim.Render("No plans yet. ") + stChip.Render(" ctrl+n ") +
			stDim.Render(" writes one;") + "\n " + stDim.Render("saving it earns XP on its skill.")
	}
	return panel("📋 Plans", content, width, height, focused)
}

// ─── Briefing ────────────────────────────────────────────────────────────────

// renderBriefing fills the right panel: active skills on top, the selected
// directive or plan, each skill's next move, and the latest XP at the bottom.
func (m model) renderBriefing(width, height int) string {
	feed := m.briefingFeed(width)
	top := append(m.briefingSkills(width, max(height/3, 3)), "")
	top = append(top, m.briefingSelected(width)...)
	top = append(top, "")
	top = append(top, m.briefingNextMoves(width)...)

	// The feed sits at the bottom; the details give way if space is short.
	room := max(height-len(feed)-1, 0)
	if len(top) > room {
		top = top[:room]
	}
	gap := max(height-len(top)-len(feed), 1)
	return strings.Join(top, "\n") + strings.Repeat("\n", gap) + strings.Join(feed, "\n")
}

// briefingSkills lists active skills, strongest first, with a bar to the
// next level and a warning on any that have rust.
func (m model) briefingSkills(width, maxRows int) []string {
	var active []db.Skill
	for _, sk := range m.skills {
		if sk.IsActive {
			active = append(active, sk)
		}
	}
	lines := []string{sectionHeader("⚔️  Active Skills", fmt.Sprint(len(active)), width)}
	if len(active) == 0 {
		return append(lines, " "+stDim.Render("No active skills yet. Press ")+stChip.Render(" s ")+
			stDim.Render(" to set up your character."))
	}

	rusty := map[int64]bool{}
	for _, n := range m.nodes {
		if n.IsRusty {
			rusty[n.SkillID] = true
		}
	}

	const nameW = 14
	for i, sk := range active {
		if i >= maxRows-1 {
			lines = append(lines, " "+stDim.Render(fmt.Sprintf("+%d more · c for the full sheet", len(active)-i)))
			break
		}
		p := rpg.ProgressFor(sk.TotalXP)
		cat := string(categoryColor(sk.Category))
		name := lipgloss.NewStyle().Foreground(lipgloss.Color(cat)).Bold(true).Width(nameW).
			Render(truncRunes(sk.Name, nameW-1))
		level := stActive.Render(fmt.Sprintf("Lv %-2d", p.Level))
		tail := stDim.Render(fmt.Sprintf(" %d/%d", p.Into, p.Span))
		warn := ""
		if rusty[sk.ID] {
			warn = lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Bold(true).Render("  ⚠ Rusty")
		}
		barW := max(width-nameW-6-lipgloss.Width(tail)-lipgloss.Width(warn)-3, 6)
		lines = append(lines, " "+name+level+" "+gradientBar(p.Fraction, barW, cat, lighten(cat, 0.5))+tail+warn)
	}
	return lines
}

// briefingSelected describes the highlighted directive, or says what to do
// when nothing is selected.
func (m model) briefingSelected(width int) []string {
	lines := []string{sectionHeader("🔎  Selected", "", width)}

	if m.mainFocus == focusPlans {
		return append(lines, " "+stDim.Render("No plans yet. Press ")+stChip.Render(" ctrl+n ")+
			stDim.Render(" to write your first; saving it earns XP."))
	}
	if m.challengeCursor >= len(m.challenges) {
		return append(lines, " "+stDim.Render("No directive selected. Press ")+stChip.Render(" g ")+
			stDim.Render(" for today's set."))
	}

	c := m.challenges[m.challengeCursor]
	if c.Tier == rpg.TierOvertime {
		lines = append(lines, " "+stSuccess.Bold(true).Render("★ Bonus Overtime Directive"))
	} else if c.Tier != rpg.TierNone {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(colorKeyLabel).Render("["+c.Tier.Label()+"]"))
	}
	title := stActive.Render(c.Title)
	if c.Done {
		title = stSuccess.Render("✓ " + c.Title)
	}
	lines = append(lines, " "+title)

	target := categoryStyle(c.Category).Render(c.SkillName)
	if c.NodeName != "" {
		target += stDim.Render(" › ") + lipgloss.NewStyle().Foreground(categoryColor(c.Category)).Render(c.NodeName)
	}
	stars := lipgloss.NewStyle().Foreground(colorKeyLabel).Render(strings.Repeat("★", c.Difficulty)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorTrack)).Render(strings.Repeat("☆", max(5-c.Difficulty, 0)))
	reward := chipLabel(fmt.Sprintf("+%d XP", c.XP), colorSuccess)
	if c.Done {
		reward = stSuccess.Render(fmt.Sprintf("✓ earned %d xp", c.XP))
	}
	lines = append(lines, ansi.Truncate(" "+target+"   "+stars+stDim.Render(fmt.Sprintf("   ◷ %d min   ", c.Minutes))+reward, width, "…"))

	if m.nodeRusty(c.SkillName, c.NodeName) && !c.Done {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Bold(true).
			Render(fmt.Sprintf("⚠ Rusty node · finishing this pays ×%.2f comeback XP", rpg.ComebackMultiplier)))
	}

	desc := c.Description
	if desc == "" {
		desc = "No description."
	}
	lines = append(lines, "")
	for _, l := range strings.Split(lipgloss.NewStyle().Width(width-2).Render(desc), "\n") {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(l))
	}
	return lines
}

// briefingNextMoves says, per active skill, what to do next: clear its rust,
// or reach the level that opens its next node.
func (m model) briefingNextMoves(width int) []string {
	lines := []string{sectionHeader("🧭  Next Moves", "", width)}
	const nameW = 14
	shown := 0
	for _, sk := range m.skills {
		if !sk.IsActive {
			continue
		}
		nodes := m.nodesFor(sk.ID)
		rows := flattenTree(nodes, rpg.NodeStatuses(toTreeNodes(nodes), rpg.ProgressFor(sk.TotalXP).Level))
		name := lipgloss.NewStyle().Foreground(categoryColor(sk.Category)).Bold(true).Width(nameW).
			Render(truncRunes(sk.Name, nameW-1))
		lines = append(lines, ansi.Truncate(" "+name+skillNext(sk, rows), width, "…"))
		shown++
	}
	if shown == 0 {
		lines = append(lines, " "+stDim.Render("Activate a skill to see its next move."))
	}
	return lines
}

// nodeRusty reports whether the named node of the named skill is rusty.
func (m model) nodeRusty(skill, node string) bool {
	if node == "" {
		return false
	}
	for _, n := range m.nodes {
		if n.IsRusty && strings.EqualFold(n.SkillName, skill) && strings.EqualFold(n.Name, node) {
			return true
		}
	}
	return false
}

// briefingFeed is the latest few awards, so the screen shows recent life.
func (m model) briefingFeed(width int) []string {
	lines := []string{sectionHeader("⚡  Recent XP", "", width)}
	if len(m.xpEvents) == 0 {
		return append(lines, " "+stDim.Render("No XP yet. Finish a directive or save a plan."))
	}
	for i, ev := range m.xpEvents {
		if i >= recentXPCount {
			break
		}
		lines = append(lines, roadEntry(ev, width))
	}
	return lines
}

// ─── Plan badges ─────────────────────────────────────────────────────────────

// planBadges maps each plan to the XP it earned, from the award events, as
// "[Skill +XP]". A plan with no award has no badge.
func (m model) planBadges() map[int64]string {
	badges := map[int64]string{}
	for _, ev := range m.xpEvents {
		if ev.PlanID == 0 {
			continue
		}
		if _, seen := badges[ev.PlanID]; !seen { // events are newest first
			badges[ev.PlanID] = fmt.Sprintf("[%s +%dxp]", ev.SkillName, ev.XP)
		}
	}
	return badges
}
