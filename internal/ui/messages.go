package ui

import (
	"github.com/navid-fn/lazyplaner/internal/db"
)

// ─── Message types ────────────────────────────────────────────────────────────

type plansLoadedMsg struct {
	plans []db.Plan
	err   error
}

type planSavedMsg struct {
	plan db.Plan
	err  error
}

type modelsLoadedMsg struct {
	models []string
	err    error
}

type planUpdatedMsg struct {
	plan db.Plan
	err  error
}

type editorFinishedMsg struct {
	err error
}
