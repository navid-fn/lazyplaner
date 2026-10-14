package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Sizes of the skill tree map, in terminal cells.
const (
	cardH     = 3  // title border, one content row, bottom border
	cardGap   = 4  // columns between cards, where connectors run
	minCardW  = 24 // narrowest card; a wider map scrolls instead
	maxCardW  = 36 // widest card; past this the extra width is margin
	maxRowGap = 3  // most blank rows between stacked cards
	rootW     = 1  // the skill is a single orb the branches grow from

	nodeBarW = 12 // progress bar inside a node card
)

// mapGeom is the size of the cards and gaps for one drawing of the map. It
// is fitted to the window, so names have room on a wide terminal and the map
// spreads out to fill a tall one.
type mapGeom struct {
	cardW  int
	rowGap int
}

func (g mapGeom) pitchX() int { return g.cardW + cardGap }
func (g mapGeom) pitchY() int { return cardH + g.rowGap }

// colX is the left edge of the cards at a depth. Depth 0 is the skill orb.
func (g mapGeom) colX(depth int) int {
	if depth == 0 {
		return 0
	}
	return rootW + cardGap + (depth-1)*g.pitchX()
}

// fitGeom picks card width and row gap for a map shown in viewW by viewH.
func fitGeom(nodes []db.Node, viewW, viewH int) mapGeom {
	depth, leaves := treeShape(nodes)
	cols := max(depth, 1) // card columns; the skill orb takes almost none
	g := mapGeom{
		cardW:  clamp((viewW-rootW)/cols-cardGap, minCardW, maxCardW),
		rowGap: 1,
	}
	if leaves > 1 {
		g.rowGap = clamp((viewH-leaves*cardH)/(leaves-1), 1, maxRowGap)
	}
	return g
}

// treeShape returns how deep the tree goes below the skill and how many
// leaves it has. A skill with no nodes is depth 0 with one leaf, itself.
func treeShape(nodes []db.Node) (depth, leaves int) {
	children := childrenOf(nodes)
	var walk func(id int64, d int)
	walk = func(id int64, d int) {
		depth = max(depth, d)
		kids := children[id]
		if len(kids) == 0 {
			leaves++
		}
		for _, k := range kids {
			walk(k.ID, d+1)
		}
	}
	walk(0, 0)
	return depth, leaves
}

// Colors used only by the tree map.
const (
	inkDark   = "#0b0f14" // text on filled chips
	inkLocked = "242"     // locked cards and their connectors
	inkSelect = "#ffffff" // border of the selected card
	inkRusty  = "#ffb86c" // rusty nodes: open, but neglected
)

// treeRow is one node with its status, in display order.
type treeRow struct {
	node   db.Node
	status rpg.Status
}

// point is a card's top-left corner on the map.
type point struct{ x, y int }

// ─── Keys ────────────────────────────────────────────────────────────────────

// openTree shows the skill tree screen for the selected skill. Leaving it
// returns to the screen it was opened from.
func (m *model) openTree() {
	m.treeReturn = m.screen
	m.screen = screenTree
	m.errorText = ""
	m.statusText = ""
	if m.treeSkill >= len(m.skills) {
		m.treeSkill = 0
	}
	m.treeNode = 0
}

func (m model) handleTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.skills)
	rows := len(m.treeRows())
	switch msg.String() {
	case "esc", "t":
		m.screen = m.treeReturn
	case "q":
		return m, tea.Quit
	// Arrows and vim keys do the same moves: h/l and j/k.
	case "left", "h":
		if n > 0 {
			m.treeSkill = (m.treeSkill - 1 + n) % n
			m.treeNode = 0
		}
	case "right", "l", "tab":
		if n > 0 {
			m.treeSkill = (m.treeSkill + 1) % n
			m.treeNode = 0
		}
	case "up", "k":
		if rows > 0 {
			m.treeNode = max(m.treeNode-1, 0)
		}
	case "down", "j":
		if rows > 0 {
			m.treeNode = min(m.treeNode+1, rows-1)
		}
	case "e":
		cmd := m.openSkillEdit()
		return m, cmd
	case "?":
		// h moves left on this screen, so help is on ? here.
		m.openHelp()
	}
	return m, nil
}

// treeRows returns the nodes of the selected skill in depth-first order,
// which is the order j and k move through them.
func (m model) treeRows() []treeRow {
	if len(m.skills) == 0 {
		return nil
	}
	skill := m.skills[m.treeSkill]
	nodes := m.nodesFor(skill.ID)
	statuses := rpg.NodeStatuses(toTreeNodes(nodes), rpg.ProgressFor(skill.TotalXP).Level)
	return flattenTree(nodes, statuses)
}

// ─── Screen ──────────────────────────────────────────────────────────────────

// renderTreeScreen draws the skill pills, then one panel holding the skill
// header, the tree map, and the details of the selected node.
func (m model) renderTreeScreen() string {
	if len(m.skills) == 0 {
		return m.fullPanel("Skill tree", stDim.Render("No skills yet. Press s to create your character."))
	}

	skill := m.skills[m.treeSkill]
	rows := m.treeRows()
	cursor := clamp(m.treeNode, 0, max(len(rows)-1, 0))

	bodyH := m.bodyHeight() - 1 // one line for the skill pills
	innerW := m.width - 2
	innerH := bodyH - 2

	hero := m.treeHero(skill, rows, innerW)
	detail := treeDetail(skill, rows, cursor, innerW)
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", innerW))

	// The map takes whatever height the header (3), details (4) and two
	// rules leave.
	mapH := max(innerH-3-4-2, cardH)
	treeMap := m.treeMap(skill, rows, cursor, innerW, mapH)

	content := strings.Join([]string{hero, rule, treeMap, rule, detail}, "\n")
	return m.renderSkillPills() + "\n" + panel("Skill tree", content, m.width, bodyH, true)
}

// renderSkillPills lists every skill. The selected one is a filled pill in
// its category color; the others show a colored dot and their level.
func (m model) renderSkillPills() string {
	var pills []string
	for i, sk := range m.skills {
		level := rpg.ProgressFor(sk.TotalXP).Level
		if i == m.treeSkill {
			pills = append(pills, lipgloss.NewStyle().
				Background(categoryColor(sk.Category)).
				Foreground(lipgloss.Color(inkDark)).
				Bold(true).
				Render(fmt.Sprintf(" ◉ %s · %d ", sk.Name, level)))
			continue
		}
		pills = append(pills, categoryStyle(sk.Category).Render("●")+" "+
			stDim.Render(fmt.Sprintf("%s %d", sk.Name, level)))
	}
	line := " " + strings.Join(pills, "   ")
	return ansi.Truncate(line, m.width, "…")
}

// treeHero is the three-line header: the skill's name and level, its XP bar,
// and what to aim for next.
func (m model) treeHero(skill db.Skill, rows []treeRow, width int) string {
	p := rpg.ProgressFor(skill.TotalXP)
	cat := string(categoryColor(skill.Category))
	done, open, locked := countStatuses(rows)

	title := " " + lipgloss.NewStyle().Foreground(lipgloss.Color(cat)).Bold(true).
		Render("◉ "+strings.ToUpper(skill.Name)) + "  " +
		chipLabel(skill.Category, lipgloss.Color(cat)) + " " +
		chipLabel(fmt.Sprintf("LV %d", p.Level), colorActive) + " " +
		stDim.Render(rpg.RankTitle(p.Level))
	stats := stSuccess.Render(fmt.Sprintf("✓ %d done", done)) + "   " +
		stActive.Render(fmt.Sprintf("◆ %d open", open)) + "   " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(inkLocked)).Render(fmt.Sprintf("◇ %d locked", locked)) + " "

	barW := clamp(width-36, 10, 90)
	xpLine := " " + gradientBar(p.Fraction, barW, cat, lighten(cat, 0.55)) + "  " +
		stActive.Render(fmt.Sprintf("%d", p.Into)) +
		stDim.Render(fmt.Sprintf(" / %d xp to Lv %d", p.Span, p.Level+1))

	return spread(title, stats, width) + "\n" + xpLine + "\n" + " " + nextGoal(skill, rows)
}

// nextGoal says what the person is closest to: the next node their level
// will unlock, and the open node nearest to done.
func nextGoal(skill db.Skill, rows []treeRow) string {
	if len(rows) == 0 {
		return stDim.Render("No tree yet. Save a plan in this skill to grow one.")
	}
	status := statusByID(rows)

	// A locked node whose parent is open or done waits only on level.
	unlock := -1
	closest := -1
	rusty := -1
	for i, r := range rows {
		n := r.node
		switch r.status {
		case rpg.Rusty:
			if rusty < 0 {
				rusty = i
			}
		case rpg.Locked:
			parentOpen := n.ParentID == 0 || status[n.ParentID] != rpg.Locked
			if parentOpen && (unlock < 0 || n.RequiredLevel < rows[unlock].node.RequiredLevel) {
				unlock = i
			}
		case rpg.Unlocked:
			if n.Progress > 0 && (closest < 0 || nodeFraction(n) > nodeFraction(rows[closest].node)) {
				closest = i
			}
		}
	}

	var parts []string
	if rusty >= 0 {
		// Rust is the most urgent thing, so it leads.
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).Bold(true).
			Render("⚠ Rusty ▸ "+rows[rusty].node.Name)+
			stDim.Render(fmt.Sprintf("  practise it for ×%.2f comeback XP", rpg.ComebackMultiplier)))
	}
	switch {
	case unlock >= 0:
		n := rows[unlock].node
		away := rpg.TotalXPForLevel(n.RequiredLevel) - skill.TotalXP
		parts = append(parts, stDim.Render("Next unlock ")+stActive.Render("▸ "+n.Name)+
			stDim.Render(fmt.Sprintf("  at Lv %d · %d xp to go", n.RequiredLevel, away)))
	case allDone(rows):
		parts = append(parts, stSuccess.Render("★ Every node in this tree is done. You have mastered it."))
	default:
		parts = append(parts, stDim.Render("Finish the open nodes to unlock the next branch."))
	}
	if closest >= 0 {
		n := rows[closest].node
		parts = append(parts, stDim.Render("Closest ")+stActive.Render(n.Name)+
			stDim.Render(fmt.Sprintf(" %d/%d xp", n.Progress, n.XPNeeded)))
	}
	return strings.Join(parts, stDim.Render("     ·     "))
}

// treeDetail is the four-line strip about the selected node.
func treeDetail(skill db.Skill, rows []treeRow, cursor, width int) string {
	if len(rows) == 0 {
		return " " + stDim.Render("Nodes appear here once this skill has a tree.") + "\n\n\n"
	}
	r := rows[cursor]
	n := r.node
	level := rpg.ProgressFor(skill.TotalXP).Level
	ink := statusInk(r.status)

	name := lipgloss.NewStyle().Foreground(lipgloss.Color(ink)).Bold(true).Render(statusIcon(r.status) + " " + n.Name)
	line1 := " " + name + "  " + statusChip(r.status) + "   " +
		stDim.Render("requires ") + stActive.Render(fmt.Sprintf("Lv %d", n.RequiredLevel)) +
		stDim.Render(fmt.Sprintf(" · you are Lv %d", level))

	frac := nodeFraction(n)
	if r.status == rpg.Done {
		frac = 1
	}
	parent := skill.Name
	var leadsTo []string
	for _, other := range rows {
		if other.node.ID == n.ParentID {
			parent = other.node.Name
		}
		if other.node.ParentID == n.ID {
			leadsTo = append(leadsTo, other.node.Name)
		}
	}
	line2 := " " + gradientBar(frac, 24, ink, lighten(ink, 0.5)) +
		stDim.Render(fmt.Sprintf(" %d/%d xp", n.Progress, n.XPNeeded)) +
		"     " + stDim.Render("from ") + parent
	if len(leadsTo) > 0 {
		line2 += "     " + stDim.Render("leads to ") + strings.Join(leadsTo, stDim.Render(", "))
	}
	if r.status == rpg.Rusty {
		line2 += "     " + lipgloss.NewStyle().Foreground(lipgloss.Color(inkRusty)).
			Render(fmt.Sprintf("⚠ comeback ×%.2f XP", rpg.ComebackMultiplier))
	}

	desc := n.Description
	if desc == "" {
		desc = "No description yet."
	}
	wrapped := lipgloss.NewStyle().Width(width - 2).Render(desc)
	descLines := strings.Split(wrapped, "\n")
	for len(descLines) < 2 {
		descLines = append(descLines, "")
	}
	if len(descLines) > 2 {
		descLines = descLines[:2]
		descLines[1] = ansi.Truncate(descLines[1], width-3, "") + "…"
	}

	return line1 + "\n" +
		ansi.Truncate(line2, width, "…") + "\n" +
		" " + stDim.Render(descLines[0]) + "\n" +
		" " + stDim.Render(descLines[1])
}

// ─── Map ─────────────────────────────────────────────────────────────────────

// treeMap draws the skill and its nodes as cards joined by lines, cut to a
// window of viewW by viewH. A map smaller than the window is centered; a
// larger one scrolls to keep the selected card in view.
func (m model) treeMap(skill db.Skill, rows []treeRow, cursor, viewW, viewH int) string {
	nodes := m.nodesFor(skill.ID)
	g := fitGeom(nodes, viewW, viewH)
	pos, mapW, mapH := treeLayout(nodes, g)
	status := statusByID(rows)
	children := childrenOf(nodes)
	cat := string(categoryColor(skill.Category))

	var selected int64 = -1
	if len(rows) > 0 {
		selected = rows[cursor].node.ID
	}
	path := ancestors(nodes, selected)

	cv := newCanvas(mapW, mapH)

	// Lines go first so the cards drawn after them cover the line ends.
	// Within one parent, brighter lines are drawn last so they win where
	// lines share cells, and the path to the selected node is brightest.
	for parentID, kids := range children {
		p, ok := pos[parentID]
		if !ok {
			continue
		}
		ordered := append([]db.Node(nil), kids...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return edgeRank(ordered[i].ID, status, path) < edgeRank(ordered[j].ID, status, path)
		})
		parentW := g.cardW
		if parentID == 0 {
			parentW = rootW
		}
		for _, k := range ordered {
			if c, ok := pos[k.ID]; ok {
				drawEdge(cv, p, c, parentW, edgeInk(k.ID, status, path, cat))
			}
		}
	}

	// The skill is an orb that the first branches grow from.
	root := pos[0]
	cv.set(root.x, root.y+1, '◉', cat, true)

	for _, n := range nodes {
		if p, ok := pos[n.ID]; ok {
			drawNodeCard(cv, p, g.cardW, n, status[n.ID], n.ID == selected)
		}
	}

	// Where a line meets a card, the border gets a joint so they connect.
	for parentID, kids := range children {
		p, ok := pos[parentID]
		if !ok || len(kids) == 0 {
			continue
		}
		// The orb is its own joint; node cards get one on the right border.
		if parentID != 0 {
			bright := kids[0].ID
			for _, k := range kids {
				if edgeRank(k.ID, status, path) > edgeRank(bright, status, path) {
					bright = k.ID
				}
			}
			cv.set(p.x+g.cardW-1, p.y+1, joint('├', parentID == selected), edgeInk(bright, status, path, cat), false)
		}
		for _, k := range kids {
			if c, ok := pos[k.ID]; ok {
				cv.set(c.x, c.y+1, joint('┤', k.ID == selected), edgeInk(k.ID, status, path, cat), false)
			}
		}
	}

	focusX, focusY := root.x, root.y
	if p, ok := pos[selected]; ok {
		focusX, focusY = p.x+g.cardW/2, p.y
	}
	x0 := viewOffset(mapW, viewW, focusX)
	y0 := viewOffset(mapH, viewH, focusY+cardH/2)
	return strings.Join(cv.window(x0, y0, viewW, viewH), "\n")
}

// treeLayout places the skill (ID 0) and every node on the map. Depth sets
// the column; the skill is a one-cell orb on the left. Each leaf gets its own row and a parent sits midway between its
// first and last child, so cards never overlap. It returns each card's
// top-left corner and the map's size.
func treeLayout(nodes []db.Node, g mapGeom) (map[int64]point, int, int) {
	children := childrenOf(nodes)
	pos := map[int64]point{}
	leaves, deepest := 0, 0

	var place func(id int64, depth int) int
	place = func(id int64, depth int) int {
		deepest = max(deepest, depth)
		kids := children[id]
		var y int
		if len(kids) == 0 {
			y = leaves * g.pitchY()
			leaves++
		} else {
			first := place(kids[0].ID, depth+1)
			last := first
			for _, k := range kids[1:] {
				last = place(k.ID, depth+1)
			}
			y = (first + last) / 2
		}
		pos[id] = point{x: g.colX(depth), y: y}
		return y
	}
	place(0, 0)

	w := rootW
	if deepest > 0 {
		w = g.colX(deepest) + g.cardW
	}
	return pos, w, (leaves-1)*g.pitchY() + cardH
}

// drawEdge draws a line from a parent card's right side to a child card's
// left side, turning down or up in the gap between them.
func drawEdge(cv *canvas, parent, child point, parentW int, ink string) {
	pm, cm := parent.y+1, child.y+1 // middle rows of the two cards
	start := parent.x + parentW
	jx := start + cardGap/2

	for x := start; x < jx; x++ {
		cv.link(x, pm, dirLeft|dirRight, ink)
	}
	cv.link(jx, pm, dirLeft, ink)

	switch {
	case cm > pm:
		cv.link(jx, pm, dirDown, ink)
		for y := pm + 1; y < cm; y++ {
			cv.link(jx, y, dirUp|dirDown, ink)
		}
		cv.link(jx, cm, dirUp, ink)
	case cm < pm:
		cv.link(jx, pm, dirUp, ink)
		for y := cm + 1; y < pm; y++ {
			cv.link(jx, y, dirUp|dirDown, ink)
		}
		cv.link(jx, cm, dirDown, ink)
	}

	cv.link(jx, cm, dirRight, ink)
	for x := jx + 1; x < child.x; x++ {
		cv.link(x, cm, dirLeft|dirRight, ink)
	}
}

// drawCard draws a card frame with its title set into the top border. A
// thick frame marks the selected card.
func drawCard(cv *canvas, p point, w int, title, borderInk, titleInk string, thick bool) {
	tl, tr, bl, br, hz, vt := '╭', '╮', '╰', '╯', '─', '│'
	if thick {
		tl, tr, bl, br, hz, vt = '┏', '┓', '┗', '┛', '━', '┃'
	}
	x, y := p.x, p.y
	right := x + w - 1

	cv.set(x, y, tl, borderInk, false)
	cv.set(x+1, y, hz, borderInk, false)
	tx := cv.text(x+2, y, " "+truncRunes(title, w-7)+" ", titleInk, true)
	for i := tx; i < right; i++ {
		cv.set(i, y, hz, borderInk, false)
	}
	cv.set(right, y, tr, borderInk, false)

	cv.set(x, y+1, vt, borderInk, false)
	for i := x + 1; i < right; i++ {
		cv.set(i, y+1, ' ', "", false)
	}
	cv.set(right, y+1, vt, borderInk, false)

	cv.set(x, y+2, bl, borderInk, false)
	for i := x + 1; i < right; i++ {
		cv.set(i, y+2, hz, borderInk, false)
	}
	cv.set(right, y+2, br, borderInk, false)
}

// drawNodeCard draws one node: its name in the border, and its status icon
// with a progress bar or the level it needs inside.
func drawNodeCard(cv *canvas, p point, w int, n db.Node, st rpg.Status, selected bool) {
	ink := statusInk(st)
	border := ink
	if selected {
		border = inkSelect
	}
	drawCard(cv, p, w, n.Name, border, ink, selected)

	// The status icon leads the content row, so the title has room for the
	// whole name.
	x, y := p.x+2, p.y+1
	cv.text(x, y, statusIcon(st), ink, true)
	x += 2
	// On narrow cards the bar gives up width so "120/240" still fits with a
	// space before the border.
	barW := clamp(w-15, 6, nodeBarW)
	switch st {
	case rpg.Done:
		cv.bar(x, y, 1, barW, ink, lighten(ink, 0.5))
		cv.text(x+barW+1, y, "done", ink, true)
	case rpg.Unlocked:
		cv.bar(x, y, nodeFraction(n), barW, ink, lighten(ink, 0.5))
		cv.text(x+barW+1, y, fmt.Sprintf("%d/%d", n.Progress, n.XPNeeded), "245", false)
	case rpg.Rusty:
		cv.bar(x, y, nodeFraction(n), barW, ink, lighten(ink, 0.5))
		cv.text(x+barW+1, y, "rusty", ink, true)
	default:
		cv.text(x, y, fmt.Sprintf("needs Lv %d", n.RequiredLevel), inkLocked, false)
	}
}

// joint is the border character where a line meets a card, thick on the
// selected card.
func joint(r rune, thick bool) rune {
	if !thick {
		return r
	}
	if r == '├' {
		return '┣'
	}
	return '┫'
}

// viewOffset picks where a window of size view starts on a map of size
// size. A small map is centered with a negative offset; a large one is
// scrolled so focus is in the middle, without running past either edge.
func viewOffset(size, view, focus int) int {
	if size <= view {
		return -(view - size) / 2
	}
	return clamp(focus-view/2, 0, size-view)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// edgeRank orders lines for drawing: locked, then open, then done, then
// the path to the selected node.
func edgeRank(id int64, status map[int64]rpg.Status, path map[int64]bool) int {
	if path[id] {
		return 10
	}
	return int(status[id])
}

// edgeInk is the color of the line into a node.
func edgeInk(id int64, status map[int64]rpg.Status, path map[int64]bool, cat string) string {
	if path[id] {
		return lighten(cat, 0.25)
	}
	return statusInk(status[id])
}

// ancestors returns the selected node and every node above it.
func ancestors(nodes []db.Node, selected int64) map[int64]bool {
	parent := map[int64]int64{}
	for _, n := range nodes {
		parent[n.ID] = n.ParentID
	}
	path := map[int64]bool{}
	for id := selected; id > 0 && !path[id]; id = parent[id] {
		if _, ok := parent[id]; !ok {
			break
		}
		path[id] = true
	}
	return path
}

func statusInk(st rpg.Status) string {
	switch st {
	case rpg.Done:
		return string(colorSuccess)
	case rpg.Unlocked:
		return string(colorActive)
	case rpg.Rusty:
		return inkRusty
	default:
		return inkLocked
	}
}

func statusIcon(st rpg.Status) string {
	switch st {
	case rpg.Done:
		return "✓"
	case rpg.Unlocked:
		return "◆"
	case rpg.Rusty:
		return "⚠"
	default:
		return "◇"
	}
}

// statusChip draws a node's status as a filled label.
func statusChip(st rpg.Status) string {
	chip := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	switch st {
	case rpg.Done:
		return chip.Background(colorSuccess).Foreground(lipgloss.Color(inkDark)).Render("DONE")
	case rpg.Unlocked:
		return chip.Background(colorActive).Foreground(lipgloss.Color(inkDark)).Render("OPEN")
	case rpg.Rusty:
		return chip.Background(lipgloss.Color(inkRusty)).Foreground(lipgloss.Color(inkDark)).Render("RUSTY")
	default:
		return chip.Background(lipgloss.Color("238")).Foreground(lipgloss.Color("250")).Render("LOCKED")
	}
}

// countStatuses splits nodes into done, open and locked. A rusty node is
// still open: it can be worked on, and doing so clears the rust.
func countStatuses(rows []treeRow) (done, open, locked int) {
	for _, r := range rows {
		switch r.status {
		case rpg.Done:
			done++
		case rpg.Unlocked, rpg.Rusty:
			open++
		default:
			locked++
		}
	}
	return done, open, locked
}

func allDone(rows []treeRow) bool {
	for _, r := range rows {
		if r.status != rpg.Done {
			return false
		}
	}
	return len(rows) > 0
}

func statusByID(rows []treeRow) map[int64]rpg.Status {
	out := make(map[int64]rpg.Status, len(rows))
	for _, r := range rows {
		out[r.node.ID] = r.status
	}
	return out
}

// nodeFraction is how far a node's progress is toward its cost, 0 to 1.
func nodeFraction(n db.Node) float64 {
	if n.XPNeeded <= 0 {
		return 0
	}
	return min(float64(n.Progress)/float64(n.XPNeeded), 1)
}

// spread puts left at the start of a line and right at its end.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// truncRunes shortens s to at most n runes, ending with … when cut.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:max(n, 0)])
	}
	return string(r[:n-1]) + "…"
}

// childrenOf groups nodes by parent, keeping insertion order.
func childrenOf(nodes []db.Node) map[int64][]db.Node {
	children := map[int64][]db.Node{}
	for _, n := range nodes {
		children[n.ParentID] = append(children[n.ParentID], n)
	}
	return children
}

// nodesFor returns the nodes that belong to one skill, in insertion order.
func (m model) nodesFor(skillID int64) []db.Node {
	var out []db.Node
	for _, n := range m.nodes {
		if n.SkillID == skillID {
			out = append(out, n)
		}
	}
	return out
}

func toTreeNodes(nodes []db.Node) []rpg.TreeNode {
	out := make([]rpg.TreeNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, rpg.TreeNode{
			ID:       n.ID,
			ParentID: n.ParentID,
			Required: n.RequiredLevel,
			Progress: n.Progress,
			XPNeeded: n.XPNeeded,
			Rusty:    n.IsRusty,
		})
	}
	return out
}

// flattenTree orders nodes depth first, the order j and k move through.
func flattenTree(nodes []db.Node, statuses map[int64]rpg.Status) []treeRow {
	children := childrenOf(nodes)
	var rows []treeRow
	var walk func(parentID int64)
	walk = func(parentID int64) {
		for _, n := range children[parentID] {
			rows = append(rows, treeRow{node: n, status: statuses[n.ID]})
			walk(n.ID)
		}
	}
	walk(0)
	return rows
}

// fullPanel draws a panel across the whole body area, used by the screens
// that are not split into two columns.
func (m model) fullPanel(title, content string) string {
	return panel(title, content, m.width, m.bodyHeight(), true)
}
