package ui

import (
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// ─── Message types ────────────────────────────────────────────────────────────

type plansLoadedMsg struct {
	plans []db.Plan
	err   error
}

type planSavedMsg struct {
	plan db.Plan
	err  error
	// xp is the award for the new plan. It is nil when the plan was saved
	// but the XP step failed, in which case xpErr says why.
	xp    *db.XPEvent
	xpErr error
}

type roadLoadedMsg struct {
	skills []db.Skill
	nodes  []db.Node
	events []db.XPEvent
	stats  db.ChallengeStats // streak and score, shown on the character screen
	err    error
}

// treeBuiltMsg says the tree for the pending skill at index was saved, or
// why it was not.
type treeBuiltMsg struct {
	index int
	err   error
}

// categoryDoneMsg carries the classifier's category for one pending skill,
// matched back to it by name.
type categoryDoneMsg struct {
	name     string
	category string
	err      error
}

type challengesLoadedMsg struct {
	challenges []db.Challenge
	stats      db.ChallengeStats
	note       string // what a daily job did, for the status line
	err        error
}

type challengeDoneMsg struct {
	event db.XPEvent
	err   error
}

type modelsLoadedMsg struct {
	models []string
	err    error
}

type planUpdatedMsg struct {
	plan db.Plan
	err  error
}

type planDeletedMsg struct {
	id  int64
	err error
}

type editorFinishedMsg struct {
	err error
}

// auditDoneMsg carries the skill auditor's answer for one pending skill,
// matched back to it by name.
type auditDoneMsg struct {
	name  string
	audit rpg.Audit
	err   error
}
