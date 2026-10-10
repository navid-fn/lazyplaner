package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) handleEditStart() (tea.Model, tea.Cmd) {
	if m.isLoading || m.isEditing {
		return m, nil
	}
	plan, ok := m.currentPlan()
	if !ok {
		m.errorText = "Select a plan to edit."
		return m, nil
	}

	editorCommand := strings.TrimSpace(os.Getenv("VISUAL"))
	if editorCommand == "" {
		editorCommand = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editorCommand == "" {
		editorCommand = "nano"
	}

	tempFile, err := os.CreateTemp("", "lazyplaner-edit-*.md")
	if err != nil {
		m.errorText = fmt.Sprintf("Failed to open editor: %v", err)
		return m, nil
	}
	if _, err := tempFile.WriteString(plan.Description); err != nil {
		m.errorText = fmt.Sprintf("Failed to prepare editor: %v", err)
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return m, nil
	}
	if err := tempFile.Close(); err != nil {
		m.errorText = fmt.Sprintf("Failed to prepare editor: %v", err)
		_ = os.Remove(tempFile.Name())
		return m, nil
	}

	cmd := exec.Command("sh", "-c", fmt.Sprintf("%s %s", editorCommand, shellQuote(tempFile.Name())))
	m.isEditing = true
	m.errorText = ""
	m.statusText = ""
	m.editTempPath = tempFile.Name()
	m.editPlanID = plan.ID
	m.editOriginal = plan.Description

	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m model) handleEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	m.isEditing = false
	defer m.resetEditorState()

	if msg.err != nil {
		m.errorText = fmt.Sprintf("Editor error: %v", msg.err)
		return m, nil
	}
	updatedDescription, err := m.readEditedDescription()
	if err != nil {
		m.errorText = err.Error()
		return m, nil
	}
	trimmed := strings.TrimSpace(updatedDescription)
	if trimmed == "" {
		m.errorText = "Description is required."
		return m, nil
	}
	if strings.TrimSpace(m.editOriginal) == trimmed {
		m.statusText = "No changes to save."
		return m, nil
	}
	if m.store == nil {
		m.errorText = "Planner is not configured."
		return m, nil
	}
	m.isLoading = true
	m.errorText = ""
	m.statusText = ""
	return m, updateDescriptionCmd(m.store, m.editPlanID, trimmed)
}

func (m model) readEditedDescription() (string, error) {
	if m.editTempPath == "" {
		return "", fmt.Errorf("missing editor file")
	}
	content, err := os.ReadFile(m.editTempPath)
	if err != nil {
		return "", fmt.Errorf("failed to read editor content: %w", err)
	}
	return string(content), nil
}

func (m *model) resetEditorState() {
	if m.editTempPath != "" {
		_ = os.Remove(m.editTempPath)
	}
	m.editTempPath = ""
	m.editPlanID = 0
	m.editOriginal = ""
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
