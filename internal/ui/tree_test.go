package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
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
	m.skills = []db.Skill{{ID: 1, Name: "Python", Category: "Craft", TotalXP: 300}}
	m.nodes = sampleTree()
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
	for range 7 {
		m = send(t, m, key("j"))
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
	// The map scrolls so the selected node (Patterns) is on screen.
	if !strings.Contains(view, "Patterns") {
		t.Error("the selected node should be scrolled into view")
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
