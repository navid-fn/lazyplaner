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

type onboardingDoneMsg struct {
	count int
	err   error
}

type challengesLoadedMsg struct {
	challenges []db.Challenge
	stats      db.ChallengeStats
	// count is the default set size, shown when asking how many to make.
	count int
	err   error
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
