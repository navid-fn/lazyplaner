package ui

// ─── List item ───────────────────────────────────────────────────────────────

type planItem struct {
	id      int64
	subject string
	created string
	badge   string // "[Skill +XP]" once the plan has earned XP
}

func (p planItem) Title() string {
	if p.badge == "" {
		return p.subject
	}
	return p.subject + "  " + p.badge
}
func (p planItem) Description() string { return p.created }
func (p planItem) FilterValue() string { return p.subject }

// ─── Model list item ─────────────────────────────────────────────────────────

type modelItem struct {
	name   string
	active bool
}

func (m modelItem) Title() string {
	if m.active {
		return "✓ " + m.name
	}
	return "  " + m.name
}
func (m modelItem) Description() string { return "" }
func (m modelItem) FilterValue() string { return m.name }
