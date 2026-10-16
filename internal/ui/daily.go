package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Today's directives live in the home screen's directives panel. The model
// jobs on them (the day's set, a reroll, the bonus) run without blocking the
// screen; dailyBusy names the running job, and the panel header shows it
// with the spinner.

// startDailyJob is the g key. It generates today's set when there is none,
// and once every directive is done it calls in the day's one bonus. Anything
// else explains why nothing happens, so g cannot be used to farm XP.
func (m *model) startDailyJob() tea.Cmd {
	if m.dailyBusy != "" {
		return nil
	}
	m.mainFocus = focusDirectives
	m.errorText = ""
	m.statusText = ""

	switch {
	case !hasSet(m.challenges):
		return m.runDailyJob(dailyGenerate, db.Challenge{}, "Generating…")
	case !allChallengesDone(m.challenges):
		m.statusText = "Finish today's directives first. r rerolls the highlighted one."
		return nil
	case bonusUsed(m.challenges):
		m.statusText = "Daily cap reached: today's bonus is used. New directives tomorrow."
		return nil
	default:
		return m.runDailyJob(dailyBonus, db.Challenge{}, "Bonus incoming…")
	}
}

// rerollSelected is the r key: it replaces the highlighted directive, and
// only that one, with a fresh one for the same skill and node.
func (m *model) rerollSelected() tea.Cmd {
	if m.dailyBusy != "" {
		return nil
	}
	m.errorText = ""
	m.statusText = ""
	if m.challengeCursor >= len(m.challenges) {
		m.statusText = "No directives yet. Press g to generate today's set."
		return nil
	}
	c := m.challenges[m.challengeCursor]
	if c.Done {
		m.statusText = "Already done. Only open directives can be rerolled."
		return nil
	}
	return m.runDailyJob(dailyReroll, c, "Rerolling…")
}

func (m *model) runDailyJob(job dailyJob, target db.Challenge, text string) tea.Cmd {
	m.dailyBusy = text
	return tea.Batch(m.spinner.Tick, dailyJobCmd(m.client, m.store, job, target))
}

// dailyKeyLabel says what g does right now, or "" when it does nothing (the
// set is unfinished, or the day's bonus is used).
func (m model) dailyKeyLabel() string {
	switch {
	case !hasSet(m.challenges):
		return "generate"
	case allChallengesDone(m.challenges) && !bonusUsed(m.challenges):
		return "bonus"
	default:
		return ""
	}
}

// completeSelectedChallenge ticks off the directive under the cursor.
func (m model) completeSelectedChallenge() (tea.Model, tea.Cmd) {
	if m.challengeCursor >= len(m.challenges) {
		return m, nil
	}
	return m.completeChallenge(m.challenges[m.challengeCursor])
}

// completeChallenge is enter on a directive, from the home screen or the
// skill tree's inspector: the first press starts it, the second opens the
// proof popup that finishes it (see proof.go).
func (m model) completeChallenge(c db.Challenge) (tea.Model, tea.Cmd) {
	m.errorText = ""
	m.statusText = ""
	switch {
	case c.Done:
		m.statusText = "Already done."
		return m, nil
	case !c.Started():
		return m, startChallengeCmd(m.store, c)
	}
	cmd := m.openProof(c)
	return m, cmd
}

// tierIcon is a directive's tier as its colored icon, for the rows of the
// day's list; the selected directive's panel shows the full label.
// Directives from before tiers have none.
func tierIcon(t rpg.Tier) string {
	if t == rpg.TierNone {
		return ""
	}
	ink := colorKeyLabel
	switch t {
	case rpg.TierCoreOp:
		ink = colorActive
	case rpg.TierDeepPush:
		ink = lipgloss.Color(inkRusty)
	case rpg.TierOvertime:
		ink = colorSuccess
	case rpg.TierFocus:
		ink = lipgloss.Color("#8be9fd")
	case rpg.TierRecovery:
		ink = colorSuccess
	}
	return lipgloss.NewStyle().Foreground(ink).Render(t.Icon())
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
