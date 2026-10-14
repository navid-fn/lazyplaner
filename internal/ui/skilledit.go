package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// skillEditWarning explains what saving does, shown above the form.
const skillEditWarning = "Saving resets this skill to level 1. Its XP and node progress go to 0. XP history and the tree are kept."

// skillEditForm is the popup for renaming a skill or changing its category.
type skillEditForm struct {
	skillID   int64
	category  string // kept as it is; the AI chose it
	name      textinput.Model
	note      string
	noteError bool
}

// openSkillEdit shows the edit popup for the skill selected on the tree
// screen.
func (m *model) openSkillEdit() tea.Cmd {
	if len(m.skills) == 0 {
		m.errorText = "No skill to edit yet."
		return nil
	}
	sk := m.skills[m.treeSkill]

	name := textinput.New()
	name.CharLimit = rpg.MaxSkillLength
	name.Width = 30
	name.SetValue(sk.Name)
	name.CursorEnd()

	m.prevScreen = screenTree
	m.screen = screenSkillEdit
	m.errorText = ""
	m.statusText = ""
	m.skillEdit = skillEditForm{
		skillID:  sk.ID,
		category: sk.Category,
		name:     name,
		note:     "Enter to save · esc cancel",
	}
	return m.skillEdit.name.Focus()
}

func (m model) handleSkillEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isLoading {
		return m, nil
	}
	f := &m.skillEdit

	switch msg.String() {
	case "esc":
		m.screen = m.prevScreen
		f.name.Blur()
		return m, nil

	case "enter":
		return m.saveSkillEdit()
	}

	var cmd tea.Cmd
	f.name, cmd = f.name.Update(msg)
	if f.noteError {
		f.note, f.noteError = "Enter to save · esc cancel", false
	}
	return m, cmd
}

// saveSkillEdit validates the new name and starts the reset.
func (m model) saveSkillEdit() (tea.Model, tea.Cmd) {
	f := &m.skillEdit
	name := rpg.NormalizeSkillName(f.name.Value())

	// The skill's own name is not a clash, so it is left out of the check.
	var others []string
	for _, s := range m.skills {
		if s.ID != f.skillID {
			others = append(others, s.Name)
		}
	}
	if err := rpg.ValidateSkillName(name, others); err != nil {
		f.note, f.noteError = err.Error(), true
		return m, nil
	}

	m.isLoading = true
	m.loadingText = "Saving " + name + "…"
	m.errorText = ""
	return m, tea.Batch(m.spinner.Tick, updateSkillCmd(m.store, f.skillID, name, f.category))
}

// skillUpdatedMsg reports the result of saving a skill edit.
type skillUpdatedMsg struct {
	name string
	err  error
}

func updateSkillCmd(store *db.Store, id int64, name, category string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := store.UpdateSkill(ctx, id, name, category)
		return skillUpdatedMsg{name: name, err: err}
	}
}

// renderSkillEdit draws the edit popup over the tree screen.
func (m model) renderSkillEdit() string {
	w := clamp(m.width-6, 44, 64)
	f := m.skillEdit

	var b strings.Builder
	b.WriteString(stError.Render(lipgloss.NewStyle().Width(w-4).Render(skillEditWarning)) + "\n\n")
	b.WriteString(stActive.Render("Name") + "\n")
	b.WriteString(f.name.View() + "\n\n")
	b.WriteString(stDim.Render("Category: "+f.category+" (chosen by the AI)") + "\n\n")

	noteStyle := stDim
	if f.noteError {
		noteStyle = stError
	}
	b.WriteString(noteStyle.Render(f.note))

	h := min(16, m.bodyHeight())
	return panel("Edit skill", b.String(), w, h, true)
}
