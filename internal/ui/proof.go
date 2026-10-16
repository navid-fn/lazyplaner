package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
)

// A directive is finished in two steps, so a stray enter cannot hand out
// XP: enter starts it ([▶], the clock runs), and enter again opens the
// proof popup, where the person writes one line on what they built or
// found. That line is kept with the directive and its XP award.

// Proof rules.
const (
	quickFinish    = 2 * time.Minute // finishing faster than this needs a longer note
	quickNoteRunes = 10              // the longer note's least length
	proofNoteMax   = 200
)

// proofForm is the open proof popup.
type proofForm struct {
	challenge db.Challenge
	input     textinput.Model
	err       string
}

// quick reports whether the directive is being finished within quickFinish
// of its start.
func (p proofForm) quick(now time.Time) bool {
	return p.challenge.Started() && now.Sub(p.challenge.StartedAt) < quickFinish
}

// openProof shows the proof popup for a started directive.
func (m *model) openProof(c db.Challenge) tea.Cmd {
	in := textinput.New()
	in.Placeholder = "e.g. worker pool passes -race with 8 workers"
	in.CharLimit = proofNoteMax
	in.Width = 50
	m.proof = &proofForm{challenge: c, input: in}
	m.errorText, m.statusText = "", ""
	return m.proof.input.Focus()
}

// handleProofKey edits the note; enter locks in the XP once the note is
// long enough, and esc goes back to work.
func (m model) handleProofKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.proof
	switch msg.String() {
	case "esc":
		m.proof = nil
		m.statusText = "Still in progress: ▶ " + p.challenge.Title
		return m, nil
	case "enter":
		note := strings.TrimSpace(p.input.Value())
		switch {
		case note == "":
			p.err = "Write one line on what you built or found."
			return m, nil
		case p.quick(time.Now()) && len([]rune(note)) < quickNoteRunes:
			p.err = fmt.Sprintf("Finished in under %d minutes: the note needs at least %d characters.",
				int(quickFinish.Minutes()), quickNoteRunes)
			return m, nil
		}
		m.proof = nil
		m.isLoading = true
		m.loadingText = "Logging " + p.challenge.Title + "…"
		return m, tea.Batch(m.spinner.Tick, completeChallengeCmd(m.store, p.challenge.ID, note))
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.err = ""
	return m, cmd
}

// renderProof is the proof popup.
func (m model) renderProof() string {
	p := m.proof
	w := clamp(m.width-6, 40, 64)
	inner := w - 4
	p.input.Width = inner - 3

	elapsed := time.Since(p.challenge.StartedAt)
	lines := []string{
		stActive.Render(truncRunes("▶ "+p.challenge.Title, inner)) +
			stDim.Render(fmt.Sprintf("  %s", elapsedLabel(elapsed))),
		"",
		"What did you build/find? " + stDim.Render("(1 short line to lock in XP)"),
		p.input.View(),
	}
	if p.quick(time.Now()) {
		warn := fmt.Sprintf("⚠️ Completed in <%dm! Type a brief proof note (≥%d chars) and press Enter to confirm, or Esc to keep working.",
			int(quickFinish.Minutes()), quickNoteRunes)
		lines = append(lines, "")
		lines = append(lines, strings.Split(lipgloss.NewStyle().Width(inner).
			Foreground(lipgloss.Color(inkRusty)).Render(warn), "\n")...)
	}
	if p.err != "" {
		lines = append(lines, stError.Render(truncRunes(p.err, inner)))
	}
	lines = append(lines, "", stDim.Render("enter lock in XP · esc keep working"))
	return panel("✓ Proof of Work", strings.Join(lines, "\n"), w, len(lines)+2, true)
}

// elapsedLabel is a running clock: "12m", or "1h05m".
func elapsedLabel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d.Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh%02dm", mins/60, mins%60)
}

// startChallengeCmd marks a directive in progress and reloads the day.
func startChallengeCmd(store *db.Store, c db.Challenge) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := store.StartChallenge(ctx, c.ID)
		list, stats, loadErr := readDaily(ctx, store)
		if err == nil {
			err = loadErr
		}
		note := "▶ Started " + c.Title + ". Press enter again when it is done."
		return challengesLoadedMsg{challenges: list, stats: stats, note: note, err: err}
	}
}
