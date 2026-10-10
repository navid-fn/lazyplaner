package ui

// setFocus moves focus to the given panel, updating blur/focus state on
// the text inputs accordingly.
func (m *model) setFocus(idx int) {
	m.focusIndex = idx
	m.subjectInput.Blur()
	m.detailInput.Blur()
	switch m.focusIndex {
	case focusSubject:
		m.subjectInput.Focus()
	case focusDetail:
		m.detailInput.Focus()
	}
}

func (m *model) updateFocus(key string) {
	next := m.focusIndex
	if key == "shift+tab" {
		next--
	} else {
		next++
	}
	if next > focusPlans {
		next = focusSubject
	}
	if next < focusSubject {
		next = focusPlans
	}
	m.setFocus(next)
}
