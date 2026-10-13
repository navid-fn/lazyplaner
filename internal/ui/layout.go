package ui

// bottomBarH is the number of lines the footer takes: status, separator, keys.
const bottomBarH = 3

// Outer heights (borders included) of the centered windows.
const (
	newPlanPanelH = 16
	newPlanTextH  = 8
	modelPanelH   = 14
)

// bodyHeight is the number of lines above the footer.
func (m model) bodyHeight() int {
	return max(m.height-bottomBarH, 3)
}

// columnWidths returns the plan list width and the preview width.
func (m model) columnWidths() (int, int) {
	if m.width == 0 {
		return 38, 62
	}
	left := m.width / 3
	if left < 30 {
		left = 30
	}
	if left > 45 {
		left = 45
	}
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
	leftW, _ := m.columnWidths()
	bodyH := m.bodyHeight()
	m.plansList.SetSize(max(leftW-2, 10), max(bodyH-2, 2))

	formW, _ := m.newPlanSize()
	// The subject input draws a 2-cell prompt and a cursor cell on top of
	// Width, so reserve room for both inside the window's inner width.
	m.newPlan.subject.Width = max(formW-7, 15)
	m.newPlan.description.SetWidth(max(formW-4, 15))

	pickW, pickH := m.modelPanelSize()
	m.modelsList.SetSize(max(pickW-2, 10), max(pickH-2, 2))
}
