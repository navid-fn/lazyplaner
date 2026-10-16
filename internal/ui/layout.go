package ui

// bottomBarH is the number of lines the footer takes: status, separator, keys.
const bottomBarH = 3

// Outer heights (borders included) of the centered windows.
const (
	newPlanPanelH = 18
	newPlanTextH  = 6
	modelPanelH   = 14
)

// bodyHeight is the number of lines above the footer.
func (m model) bodyHeight() int {
	return max(m.height-bottomBarH, 3)
}

// columnWidths returns the plan list width and the preview width.
// columnWidths returns the left column (directives and plans) and right
// panel widths on the home screen. The left column is wide enough for a
// directive's title, skill and XP on one line.
func (m model) columnWidths() (int, int) {
	if m.width == 0 {
		return 38, 62
	}
	left := clamp(m.width*2/5, 34, 64)
	return left, m.width - left
}

// newPlanSize returns the outer size of the New Plan window.
func (m model) newPlanSize() (int, int) {
	return clamp(m.width-6, 40, 72), min(newPlanPanelH, m.bodyHeight())
}

// modelPanelSize returns the outer size of the model picker window.
func (m model) modelPanelSize() (int, int) {
	return clamp(m.width-6, 30, 50), min(modelPanelH, m.bodyHeight())
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// updateSizes resizes every component to match the current terminal size.
func (m *model) updateSizes() {
	m.sizeOnboardInputs()
	leftW, _ := m.columnWidths()
	_, _, plansH := m.homeHeights()
	m.plansList.SetSize(max(leftW-2, 10), max(plansH-2, 2))

	formW, _ := m.newPlanSize()
	// Each input sits in a bordered box with one cell of padding, inside the
	// panel's own border. The subject also has a 2-cell prompt and a cursor.
	boxW := max(formW-4, 15)
	m.newPlan.subject.Width = max(boxW-4-3, 10)
	m.newPlan.description.SetWidth(max(boxW-4, 10))

	pickW, pickH := m.modelPanelSize()
	m.modelsList.SetSize(max(pickW-2, 10), max(pickH-2, 2))

	m.sizeNoteEditor()
	if m.screen == screenPlanView {
		m.layoutPlanView()
	}
}
