package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// sampleTree is shaped like a real generated tree: a chain with branches.
//
//	1 Basics
//	├ 2 Variables
//	│ ├ 3 Control
//	│ │ ├ 4 Functions
//	│ │ └ 5 Modules
//	│ └ 6 Loops
//	│   ├ 7 OOP
//	│   └ 8 Patterns
//	└ 9 Tooling
func sampleTree() []db.Node {
	mk := func(id, parent int64, name string, lv int) db.Node {
		return db.Node{ID: id, SkillID: 1, ParentID: parent, Name: name, RequiredLevel: lv, XPNeeded: 40 * lv}
	}
	return []db.Node{
		mk(1, 0, "Basics", 1), mk(2, 1, "Variables", 1), mk(3, 2, "Control", 2),
		mk(4, 3, "Functions", 3), mk(5, 3, "Modules", 4), mk(6, 2, "Loops", 3),
		mk(7, 6, "OOP", 5), mk(8, 6, "Patterns", 6), mk(9, 1, "Tooling", 2),
	}
}

// testGeom is the narrowest layout, where overlaps would show first.
var testGeom = mapGeom{cardW: minCardW, rowGap: 1}

func TestTreeLayoutCardsNeverOverlap(t *testing.T) {
	pos, w, h := treeLayout(sampleTree(), testGeom)
	if len(pos) != 10 { // 9 nodes and the skill
		t.Fatalf("placed %d cards, want 10", len(pos))
	}
	// The skill (ID 0) is a one-cell orb; every node is a full card.
	width := func(id int64) int {
		if id == 0 {
			return rootW
		}
		return testGeom.cardW
	}
	for a, pa := range pos {
		if pa.x < 0 || pa.y < 0 || pa.x+width(a) > w || pa.y+cardH > h {
			t.Errorf("card %d at %+v is outside the %dx%d map", a, pa, w, h)
		}
		for b, pb := range pos {
			if a >= b {
				continue
			}
			overlapX := pa.x < pb.x+width(b) && pb.x < pa.x+width(a)
			overlapY := pa.y < pb.y+cardH && pb.y < pa.y+cardH
			if overlapX && overlapY {
				t.Errorf("cards %d %+v and %d %+v overlap", a, pa, b, pb)
			}
		}
	}
}

func TestTreeLayoutParentSitsBetweenChildren(t *testing.T) {
	nodes := sampleTree()
	pos, _, _ := treeLayout(nodes, testGeom)
	for parent, kids := range childrenOf(nodes) {
		p := pos[parent]
		first, last := pos[kids[0].ID], pos[kids[len(kids)-1].ID]
		if p.y < first.y || p.y > last.y {
			t.Errorf("parent %d at y=%d is not between children %d..%d", parent, p.y, first.y, last.y)
		}
		want := p.x + testGeom.pitchX()
		if parent == 0 {
			want = rootW + cardGap // the first cards sit just right of the orb
		}
		for _, k := range kids {
			if pos[k.ID].x != want {
				t.Errorf("child %d at x=%d, want %d (one column right of parent %d)", k.ID, pos[k.ID].x, want, parent)
			}
		}
	}
}

func TestTreeLayoutWithNoNodesIsJustTheSkill(t *testing.T) {
	pos, w, h := treeLayout(nil, testGeom)
	if len(pos) != 1 || w != rootW || h != cardH {
		t.Errorf("empty tree: %d cards, %dx%d; want just the orb, %dx%d", len(pos), w, h, rootW, cardH)
	}
}

func TestJoinGlyph(t *testing.T) {
	cases := map[uint8]rune{
		dirLeft | dirRight:           '─',
		dirUp | dirDown:              '│',
		dirDown | dirRight:           '╭',
		dirUp | dirRight:             '╰',
		dirLeft | dirDown:            '╮',
		dirLeft | dirUp:              '╯',
		dirLeft | dirUp | dirDown:    '┤',
		dirRight | dirUp | dirDown:   '├',
		dirLeft | dirRight | dirDown: '┬',
	}
	for mask, want := range cases {
		if got := joinGlyph(mask); got != want {
			t.Errorf("joinGlyph(%04b) = %q, want %q", mask, got, want)
		}
	}
}

func TestCanvasWindowKeepsWidthWhenCenteredOrScrolled(t *testing.T) {
	cv := newCanvas(10, 2)
	cv.text(0, 0, "abcdefghij", "#ffffff", false)

	for _, x0 := range []int{-5, 0, 4} {
		for _, line := range cv.window(x0, 0, 20, 3) {
			if w := lipgloss.Width(line); w != 20 {
				t.Errorf("x0=%d: line width %d, want 20", x0, w)
			}
		}
	}
	if got := cv.window(4, 0, 3, 1)[0]; !strings.Contains(got, "efg") {
		t.Errorf("window at x=4 should show efg, got %q", got)
	}
}

func TestLinkDoesNotDrawOverCards(t *testing.T) {
	cv := newCanvas(3, 1)
	cv.set(1, 0, '│', "", false)
	cv.link(1, 0, dirLeft|dirRight, "")
	if cv.cells[0][1].mask != 0 {
		t.Error("a connector should not draw over a card border")
	}
}

func TestViewOffsetCentersSmallMapsAndFollowsFocus(t *testing.T) {
	if got := viewOffset(20, 40, 10); got != -10 {
		t.Errorf("small map should center with offset -10, got %d", got)
	}
	if got := viewOffset(100, 40, 5); got != 0 {
		t.Errorf("focus near the start should not scroll past 0, got %d", got)
	}
	if got := viewOffset(100, 40, 95); got != 60 {
		t.Errorf("focus near the end should stop at the edge, got %d", got)
	}
	if got := viewOffset(100, 40, 50); got != 30 {
		t.Errorf("focus in the middle should be centered, got %d", got)
	}
}

func TestTreeScreenShowsCardsAndDetail(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 160, 40
	m.skills = []db.Skill{{ID: 1, Name: "Python", Category: "Craft"}}
	m.nodes = sampleTree()
	m.nodes[0].Progress = 40 // Basics done, so Tooling waits only on level
	m.openTree()

	view := m.View()
	for _, want := range []string{"PYTHON", "◉", "Basics", "Variables", "Next unlock", "leads to"} {
		if !strings.Contains(view, want) {
			t.Errorf("tree screen should show %q", want)
		}
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d wide, wider than the %d-column terminal", i, w, m.width)
		}
	}
}

func TestTreeScreenFitsANarrowTerminal(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 80, 24
	m.skills = []db.Skill{{ID: 1, Name: "Python", Category: "Craft"}}
	m.nodes = sampleTree()
	m.openTree()
	// Down to Functions, then along its column to Patterns.
	for _, k := range []string{"l", "l", "l", "j", "j", "j"} {
		m = send(t, m, key(k))
	}
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Errorf("view is %d lines, want exactly %d", len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d wide, wider than %d", i, w, m.width)
		}
	}
	// The map scrolls so the selected node (Patterns, Lv 6) is on screen,
	// but at Lv 1 it is hidden in fog.
	if !strings.Contains(view, fogLabel) || !strings.Contains(view, "reach Lv 4 to reveal") {
		t.Error("the selected node should be scrolled into view, masked by fog")
	}
	if strings.Contains(view, "Patterns") {
		t.Error("a node in fog must not show its name")
	}
}

func TestFogLiftsWithinTwoLevels(t *testing.T) {
	n := db.Node{Name: "Patterns", RequiredLevel: 6}
	if !fogged(n, rpg.Locked, 3) {
		t.Error("Lv 6 node at Lv 3 should be in fog")
	}
	if fogged(n, rpg.Locked, 4) {
		t.Error("Lv 6 node at Lv 4 is within two levels and should show")
	}
	if fogged(n, rpg.Unlocked, 1) {
		t.Error("only locked nodes are fogged")
	}
}

func TestTreeEnterCompletesTheNodesDirective(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 160, 40
	m.skills = []db.Skill{{ID: 1, Name: "Python", Category: "Craft"}}
	m.nodes = sampleTree()
	m.openTree()

	m = send(t, m, key("enter"))
	if m.isLoading || m.statusText != "No directive for this node today." {
		t.Errorf("no directive: loading=%v status=%q", m.isLoading, m.statusText)
	}

	m.challenges = []db.Challenge{
		{ID: 7, SkillName: "Python", NodeName: "Tooling", Title: "Set up ruff"},
		{ID: 8, SkillName: "python", NodeName: "basics", Title: "Write a REPL session log", XP: 9},
	}
	if view := m.View(); !strings.Contains(view, "Write a REPL session log") || !strings.Contains(view, "complete") {
		t.Error("the inspector should show the node's directive and how to complete it")
	}
	m.challenges[1].StartedAt = time.Now().Add(-20 * time.Minute)
	m = send(t, m, key("enter"))
	if m.proof == nil || m.proof.challenge.ID != 8 {
		t.Fatal("enter on the node's started directive should ask for proof")
	}
	m = typeText(t, m, "logged a session")
	m = send(t, m, key("enter"))
	if !m.isLoading || !strings.Contains(m.loadingText, "Write a REPL session log") {
		t.Errorf("enter should complete the selected node's directive, loading=%v text=%q", m.isLoading, m.loadingText)
	}
	if m.screen != screenTree {
		t.Error("completing from the tree stays on the tree")
	}
	if !strings.Contains(m.renderKeys(), "complete directive") {
		t.Error("the tree's key bar should list enter")
	}
}

func TestFitGeomUsesTheSpace(t *testing.T) {
	nodes := sampleTree() // depth 4 below the skill, 5 leaves

	wide := fitGeom(nodes, 186, 33)
	if wide.cardW <= minCardW {
		t.Errorf("a wide window should give wider cards, got %d", wide.cardW)
	}
	if wide.rowGap <= 1 {
		t.Errorf("a tall window should spread the rows, got gap %d", wide.rowGap)
	}
	_, w, h := treeLayout(nodes, wide)
	if w > 186 || h > 33 {
		t.Errorf("fitted map %dx%d should fit the 186x33 window", w, h)
	}

	narrow := fitGeom(nodes, 80, 12)
	if narrow.cardW != minCardW || narrow.rowGap != 1 {
		t.Errorf("a small window should use the smallest layout, got %+v", narrow)
	}
}

// A long error or status must not wrap: an extra terminal row scrolls the
// skill pills and the tree header off the top.
func TestLongStatusDoesNotPushTheScreenUp(t *testing.T) {
	long := strings.Repeat("build roadmap for Go: context deadline exceeded\n", 6)
	for _, size := range [][2]int{{186, 50}, {120, 30}, {80, 24}} {
		for _, screen := range []string{"tree", "home", "setup"} {
			m := newFlowModel(t)
			m.width, m.height = size[0], size[1]
			m.updateSizes()
			m.skills = []db.Skill{{ID: 1, Name: "Python", Category: "Craft"}}
			m.nodes = sampleTree()
			switch screen {
			case "tree":
				m.openTree()
			case "setup":
				m.openOnboard()
			}
			m.errorText = long
			lines := strings.Split(m.View(), "\n")
			if len(lines) != m.height {
				t.Errorf("%s %dx%d: %d lines, want %d", screen, m.width, m.height, len(lines), m.height)
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > m.width {
					t.Errorf("%s %dx%d: line %d is %d wide", screen, m.width, m.height, i, w)
				}
			}
			if screen == "tree" && !strings.Contains(lines[0], "Python") {
				t.Errorf("%dx%d: the skill pills should be the first line, got %q", m.width, m.height, lines[0])
			}
		}
	}
}

// diamondNodes is the 8-node tree BuildRoadmap shapes, for a skill at start:
// node IDs are index+1, N5 joins N2 and N3, N6 joins N3 and N4, and the
// capstone N7 joins N5 and N6.
func diamondNodes(start int) []db.Node {
	parents := []int64{0, 0, 1, 1, 2, 4, 5, 6}
	joins := map[int]int64{5: 3, 6: 4, 7: 7}
	levels := map[int][]int{1: {1, 1, 2, 2, 2, 4, 4, 6}, 2: {1, 1, 1, 2, 2, 3, 3, 5}}[start]
	var out []db.Node
	for i := range 8 {
		cost := rpg.NodeXPNeeded(levels[i])
		out = append(out, db.Node{ID: int64(i + 1), SkillID: 1, ParentID: parents[i], ExtraParentID: joins[i],
			Era: 1, IsCapstone: i == 7, Name: fmt.Sprintf("N%d", i), RequiredLevel: levels[i], XPNeeded: cost,
			Progress: rpg.StartProgress(levels[i], start, cost)})
	}
	return out
}

// twoEras is diamondNodes(1) with every node done, followed by a second era
// that grows from its capstone.
func twoEras() []db.Node {
	nodes := diamondNodes(1)
	for i := range nodes {
		nodes[i].Progress = nodes[i].XPNeeded
	}
	parents := []int64{8, 8, 9, 9, 10, 12, 13, 14}
	joins := map[int]int64{5: 11, 6: 12, 7: 15}
	for i := range 8 {
		lv := 7 + []int{0, 0, 1, 1, 1, 3, 3, 5}[i]
		nodes = append(nodes, db.Node{ID: int64(i + 9), SkillID: 1, ParentID: parents[i], ExtraParentID: joins[i],
			Era: 2, IsCapstone: i == 7, Name: fmt.Sprintf("E%d", i), RequiredLevel: lv, XPNeeded: rpg.NodeXPNeeded(lv)})
	}
	return nodes
}

func TestLeafNodesExplainThemselves(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 186, 50
	m.skills = []db.Skill{{ID: 1, Name: "Cook", Category: "Craft"}}
	m.nodes = diamondNodes(1)
	m.openTree()
	rows := m.treeRows()

	for i, r := range rows {
		m.treeNode = i
		view := m.View()
		switch r.node.Name {
		case "N2": // column 2's top node joins into N5: no longer a dead end
			if strings.Contains(view, "side branch") || !strings.Contains(view, "leads to") {
				t.Errorf("N2 should lead on to N5, not be a side branch")
			}
		case "N5": // two parents
			if !strings.Contains(view, "N3") || !strings.Contains(view, "N2") {
				t.Errorf("N5 should name both of its parents")
			}
		case "N7":
			if strings.Contains(view, "capstone") {
				t.Error("at Lv 1 the capstone is in fog and says nothing")
			}
		}
	}

	// Out of the fog (Lv 5), N7 shows as the end of the tree.
	m.skills[0].TotalXP = rpg.TotalXPForLevel(5)
	for i, r := range m.treeRows() {
		if r.node.Name == "N7" {
			m.treeNode = i
		}
	}
	if !strings.Contains(m.View(), "capstone") {
		t.Error("N7 should be marked the capstone")
	}
}

func TestGAsksForADirectiveOnAnOpenNode(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 186, 50
	m.skills = []db.Skill{{ID: 1, Name: "Cook", Category: "Craft"}}
	m.nodes = diamondNodes(1) // N0 and N1 open at Lv 1
	m.openTree()

	if !strings.Contains(m.renderKeys(), "g new directive") {
		t.Error("the footer should offer g on an open node without a directive")
	}
	if !strings.Contains(m.View(), "No directive for this node today.") {
		t.Fatal("inspector should say there is no directive")
	}
	m = send(t, m, key("g"))
	if !strings.Contains(m.dailyBusy, "Drafting a directive for N0") {
		t.Fatalf("g should start a directive for N0, busy=%q", m.dailyBusy)
	}
	if !strings.Contains(m.View(), "Drafting a directive for N0") {
		t.Error("the inspector should show the spinner line")
	}

	// A locked node is refused, and so is one that already has a directive.
	m.dailyBusy = ""
	m.treeNode = 2 // depth-first order: N0, N2, N3, … so row 2 is N3 (locked)
	if m.treeRows()[2].status != rpg.Locked {
		t.Fatalf("row 2 should be locked, got %v", m.treeRows()[2].status)
	}
	m = send(t, m, key("g"))
	if m.dailyBusy != "" || !strings.Contains(m.statusText, "locked") {
		t.Errorf("g on a locked node: busy=%q status=%q", m.dailyBusy, m.statusText)
	}
	if strings.Contains(m.renderKeys(), "g new directive") {
		t.Error("g should not be offered on a locked node")
	}

	// An open focus directive is rerolled by g instead of piling up another.
	m.treeNode = 0
	m.challenges = []db.Challenge{{ID: 1, Tier: rpg.TierFocus, SkillName: "Cook", NodeName: "N0", Title: "Dice an onion"}}
	if !strings.Contains(m.View(), "reroll") {
		t.Error("an open focus directive should offer g to reroll it")
	}
	m = send(t, m, key("g"))
	if !strings.Contains(m.dailyBusy, "Drafting a directive for N0") {
		t.Errorf("g on an open focus directive should reroll it: busy=%q status=%q", m.dailyBusy, m.statusText)
	}

	// A directive of the day's set, or a finished one, is refused.
	m.dailyBusy = ""
	m.challenges = []db.Challenge{{ID: 1, Tier: rpg.TierCoreOp, SkillName: "Cook", NodeName: "N0", Title: "Dice an onion"}}
	m = send(t, m, key("g"))
	if m.dailyBusy != "" || !strings.Contains(m.statusText, "part of the set or done") {
		t.Errorf("g on a node with a set directive: busy=%q status=%q", m.dailyBusy, m.statusText)
	}
}

// Focus directives from the tree do not count as the day's set, so g on
// the home screen still generates it.
func TestFocusDirectivesAreNotTheDailySet(t *testing.T) {
	m := newFlowModel(t)
	m.challenges = []db.Challenge{{ID: 1, Tier: rpg.TierFocus, SkillName: "Cook", NodeName: "N0", Title: "Dice an onion", Done: true}}
	if m.dailyKeyLabel() != "generate" {
		t.Errorf("g label = %q, want generate", m.dailyKeyLabel())
	}
	if !strings.Contains(m.View(), "generate today's set") {
		t.Error("the panel should point at g for the set")
	}
	m = send(t, m, key("g"))
	if m.dailyBusy != "Generating…" {
		t.Errorf("g should generate the set, busy=%q", m.dailyBusy)
	}
}

func TestMoveInTreeWalksTheMap(t *testing.T) {
	rows := flattenTree(sampleTree(), nil)
	at := func(name string) int {
		for i, r := range rows {
			if r.node.Name == name {
				return i
			}
		}
		t.Fatalf("no row %q", name)
		return -1
	}
	cases := []struct{ from, key, want string }{
		{"Basics", "l", "Variables"},
		{"Variables", "h", "Basics"},
		{"Basics", "h", "Basics"},     // the top level has no parent card
		{"Variables", "j", "Tooling"}, // same column, next card down
		{"Tooling", "k", "Variables"},
		{"Tooling", "l", "Tooling"},   // a leaf has no children
		{"Functions", "j", "Modules"}, // siblings
		{"Modules", "j", "OOP"},       // crosses to a cousin in the same column
		{"Patterns", "j", "Patterns"}, // bottom of the column
		{"Control", "down", "Loops"},
	}
	for _, c := range cases {
		if got := rows[moveInTree(rows, at(c.from), c.key)].node.Name; got != c.want {
			t.Errorf("%s from %s = %s, want %s", c.key, c.from, got, c.want)
		}
	}
}

func TestTreeScreenShowsErasAndJumpsBetweenThem(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 200, 50
	m.skills = []db.Skill{{ID: 1, Name: "Java", Category: "Craft", TotalXP: rpg.TotalXPForLevel(7)}}
	m.nodes = twoEras()
	m.eras = []db.Era{{SkillID: 1, Era: 2, Title: "Spring at Scale"}}
	m.openTree()

	view := m.View()
	for _, want := range []string{"Era II", "8/16 done", "Spring at Scale"} {
		if !strings.Contains(view, want) {
			t.Errorf("tree screen should show %q", want)
		}
	}
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d wide, wider than the %d-column terminal", i, w, m.width)
		}
	}

	// ] jumps to the first node of era II, [ back to era I.
	next, _ := m.handleTreeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	m = next.(model)
	if r := m.treeRows()[m.treeNode]; r.node.Name != "E0" {
		t.Errorf("] landed on %s, want E0", r.node.Name)
	}
	if !strings.Contains(m.View(), "from N7") {
		t.Error("an era's foundation should show it grows from the last capstone")
	}
	next, _ = m.handleTreeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	m = next.(model)
	if r := m.treeRows()[m.treeNode]; r.node.Era != 1 {
		t.Errorf("[ landed in era %d, want 1", r.node.Era)
	}
}

func TestXExpandsOnlyAFinishedEra(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 200, 50
	m.skills = []db.Skill{{ID: 1, Name: "Java", Category: "Craft"}}
	m.nodes = diamondNodes(1)
	m.openTree()

	next, cmd := m.handleTreeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(model)
	if cmd != nil || !strings.Contains(m.statusText, "Finish Era I") {
		t.Errorf("x on an open era: status %q", m.statusText)
	}

	for i := range m.nodes {
		m.nodes[i].Progress = m.nodes[i].XPNeeded
	}
	m.skills[0].TotalXP = rpg.TotalXPForLevel(6)
	if !strings.Contains(m.View(), "expand into Era II") {
		t.Error("a finished era should offer x")
	}
	next, cmd = m.handleTreeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = next.(model)
	if cmd == nil || !strings.Contains(m.dailyBusy, "Era II") {
		t.Errorf("x on a finished era should start designing era II, busy %q", m.dailyBusy)
	}
}

func TestJoinLinesReachEveryCard(t *testing.T) {
	nodes := diamondNodes(1)
	if got := len(joinEdges(nodes)); got != 3 {
		t.Fatalf("got %d join lines, want 3", got)
	}
	// Every node but the capstone leads somewhere, so none is a dead end.
	leads := map[int64]bool{}
	for _, n := range nodes {
		leads[n.ParentID] = true
		leads[n.ExtraParentID] = true
	}
	for _, n := range nodes {
		if !n.IsCapstone && !leads[n.ID] {
			t.Errorf("%s leads nowhere", n.Name)
		}
	}
}
