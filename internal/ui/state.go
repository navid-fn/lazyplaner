package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/navid-fn/lazyplaner/internal/db"
)

func (m *model) setPlans(plans []db.Plan) {
	items := make([]list.Item, 0, len(plans))
	m.plansByID = map[int64]db.Plan{}
	m.plans = plans
	for _, plan := range plans {
		m.plansByID[plan.ID] = plan
		items = append(items, planItem{id: plan.ID, subject: plan.Subject})
	}
	m.plansList.SetItems(items)
	if len(items) > 0 {
		m.plansList.Select(0)
	}
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
