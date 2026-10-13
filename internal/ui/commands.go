package ui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/llm"
)

func loadPlansCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		plans, err := store.ListPlans(ctx)
		return plansLoadedMsg{plans: plans, err: err}
	}
}

func loadModelsCmd(client *llm.Client) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return modelsLoadedMsg{err: errors.New("planner is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models, err := client.ListModels(ctx)
		return modelsLoadedMsg{models: models, err: err}
	}
}

func generatePlanCmd(client *llm.Client, store *db.Store, subject, detail string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		description, err := client.GenerateDescription(ctx, subject, detail)
		if err != nil {
			return planSavedMsg{err: err}
		}
		plan, err := store.CreatePlan(ctx, subject, detail, description)
		return planSavedMsg{plan: plan, err: err}
	}
}

func deletePlanCmd(store *db.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := store.DeletePlan(ctx, id)
		return planDeletedMsg{id: id, err: err}
	}
}

func updateDescriptionCmd(store *db.Store, id int64, description string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		plan, err := store.UpdatePlanDescription(ctx, id, description)
		return planUpdatedMsg{plan: plan, err: err}
	}
}
