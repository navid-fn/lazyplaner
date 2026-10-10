package ui

// formPanelH is the fixed outer height of the "New Plan" panel:
// 7 content lines + 2 border lines.
const formPanelH = 9

// modelPanelH is the fixed outer height of the "Model" panel.
const modelPanelH = 7

// bottomBarH is the number of lines the bottom bar occupies.
const bottomBarH = 3

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

func (m *model) updateSizes() {
	leftW, _ := m.columnWidths()

	// Resize text inputs to fit the form panel. The inputs draw a 2-cell
	// prompt ("> ") and a cursor cell on top of Width, so reserve room for
	// both inside the panel's inner width (leftW-2).
	inputW := leftW - 5
	if inputW < 15 {
		inputW = 15
	}
	m.subjectInput.Width = inputW
	m.detailInput.Width = inputW

	// Resize the models list to fit the model panel.
	modelInnerW := leftW - 2
	modelInnerH := modelPanelH - 2
	if modelInnerH < 2 {
		modelInnerH = 2
	}
	m.modelsList.SetSize(modelInnerW, modelInnerH)

	// Resize the plans list to fit the list panel.
	listPanelH := m.height - bottomBarH - formPanelH - modelPanelH
	if listPanelH < 4 {
		listPanelH = 4
	}
	listInnerW := leftW - 2
	listInnerH := listPanelH - 2
	if listInnerH < 2 {
		listInnerH = 2
	}
	m.plansList.SetSize(listInnerW, listInnerH)
}
