package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/navid-fn/lazyplaner/internal/db"
)

// formatPlanDate shows a plan's creation time in local time.
func formatPlanDate(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

func (m *model) setPlans(plans []db.Plan) {
	m.plansByID = map[int64]db.Plan{}
	m.plans = plans
	for _, plan := range plans {
		m.plansByID[plan.ID] = plan
	}
	m.plansList.SetItems(m.planItems())
	if len(plans) > 0 {
		m.plansList.Select(0)
	}
}

// refreshPlanItems rebuilds the list rows, for example after new XP events
// change the badges, keeping the selected row.
func (m *model) refreshPlanItems() {
	selected := m.plansList.Index()
	m.plansList.SetItems(m.planItems())
	if n := len(m.plans); n > 0 {
		m.plansList.Select(clamp(selected, 0, n-1))
	}
}

// planItems builds a list row per plan, each with its XP badge if any.
func (m model) planItems() []list.Item {
	badges := m.planBadges()
	items := make([]list.Item, 0, len(m.plans))
	for _, plan := range m.plans {
		items = append(items, planItem{
			id:      plan.ID,
			subject: plan.Subject,
			created: formatPlanDate(plan.CreatedAt),
			badge:   badges[plan.ID],
		})
	}
	return items
}

// modelNamesMatch compares model names ignoring an implicit ":latest" tag,
// since Ollama's API returns fully-tagged names (e.g. "llama3.2:latest")
// even when the client was configured with the bare name ("llama3.2").
func modelNamesMatch(a, b string) bool {
	norm := func(s string) string {
		return strings.TrimSuffix(s, ":latest")
	}
	return norm(a) == norm(b)
}

func (m *model) setModels(models []string) {
	m.models = models
	selectedIdx := 0
	matchedName := ""
	items := make([]list.Item, 0, len(models))
	for i, name := range models {
		active := modelNamesMatch(name, m.selectedModel)
		if active {
			selectedIdx = i
			matchedName = name
		}
		items = append(items, modelItem{name: name, active: active})
	}
	// Adopt the fully-tagged name so future selections compare exactly.
	if matchedName != "" && matchedName != m.selectedModel {
		m.selectedModel = matchedName
		if m.client != nil {
			m.client.SetModel(matchedName)
		}
	}
	m.modelsList.SetItems(items)
	if len(items) > 0 {
		m.modelsList.Select(selectedIdx)
	}
}

// removePlan drops a plan from the list and keeps the selection on a
// neighbouring row.
func (m *model) removePlan(id int64) {
	kept := make([]db.Plan, 0, len(m.plans))
	selected := m.plansList.Index()
	removedAt := -1
	for i, p := range m.plans {
		if p.ID == id {
			removedAt = i
			continue
		}
		kept = append(kept, p)
	}
	m.setPlans(kept)
	if removedAt >= 0 && len(kept) > 0 {
		// Keep the cursor where it was, clamped to the last row.
		m.plansList.Select(min(selected, len(kept)-1))
	}
}

func (m *model) updatePlanInList(plan db.Plan) {
	for i, p := range m.plans {
		if p.ID == plan.ID {
			m.plans[i] = plan
			break
		}
	}
	m.plansByID[plan.ID] = plan
}

func (m model) currentPlan() (db.Plan, bool) {
	item := m.plansList.SelectedItem()
	if item == nil {
		return db.Plan{}, false
	}
	pi, ok := item.(planItem)
	if !ok {
		return db.Plan{}, false
	}
	plan, ok := m.plansByID[pi.id]
	return plan, ok
}
