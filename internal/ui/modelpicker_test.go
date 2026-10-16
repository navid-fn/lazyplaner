package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/navid-fn/lazyplaner/internal/llm"
)

func TestInstalledModel(t *testing.T) {
	cases := []struct {
		name, current string
		installed     []string
		want          string
		switched      bool
	}{
		{"installed stays", "qwen2.5:7b", []string{"deepseek-v2:latest", "qwen2.5:7b"}, "qwen2.5:7b", false},
		{"bare name matches :latest", "llama3.2", []string{"llama3.2:latest"}, "llama3.2", false},
		{"deleted prefers the default", "llama3.2", []string{"a-model", "deepseek-v2:latest"}, "deepseek-v2:latest", true},
		{"deleted falls back to the first", "llama3.2", []string{"mistral:latest", "qwen2.5:7b"}, "mistral:latest", true},
		{"nothing installed keeps it", "llama3.2", nil, "llama3.2", false},
	}
	for _, c := range cases {
		got, switched := installedModel(c.current, c.installed)
		if got != c.want || switched != c.switched {
			t.Errorf("%s: got %q switched %v, want %q %v", c.name, got, switched, c.want, c.switched)
		}
	}
}

// modelFlow is a flow model with a client that never reaches a server; the
// tests feed the installed models in directly.
func modelFlow(t *testing.T, current string) model {
	t.Helper()
	m := newFlowModel(t)
	m.client = llm.NewClient("http://127.0.0.1:1", current)
	m.selectedModel = current
	return m
}

func TestDeletedModelSwitchesAndIsSaved(t *testing.T) {
	m := modelFlow(t, "llama3.2")
	m = sendRun(t, m, modelsLoadedMsg{models: []string{"deepseek-v2:latest", "qwen2.5:7b"}})
	if m.selectedModel != "deepseek-v2:latest" || m.client.Model() != "deepseek-v2:latest" {
		t.Fatalf("selected %q client %q, want deepseek-v2:latest", m.selectedModel, m.client.Model())
	}
	if !strings.Contains(m.statusText, "llama3.2 is not installed") {
		t.Errorf("status = %q, want it to say why the model changed", m.statusText)
	}
	if !strings.Contains(m.renderBottomBar(), "deepseek-v2") {
		t.Error("the bottom bar should show the new model")
	}
	saved, err := m.store.GetSelectedModel(context.Background())
	if err != nil || saved != "deepseek-v2:latest" {
		t.Errorf("saved %q err %v, want the switched model", saved, err)
	}
}

func TestPickedModelIsSaved(t *testing.T) {
	m := modelFlow(t, "deepseek-v2:latest")
	next, _ := m.openModels()
	m = next.(model)
	m = sendRun(t, m, modelsLoadedMsg{models: []string{"deepseek-v2:latest", "qwen2.5:7b"}})
	m = send(t, m, key("down"))
	m = sendRun(t, m, key("enter"))
	if m.selectedModel != "qwen2.5:7b" {
		t.Fatalf("selected %q, want qwen2.5:7b", m.selectedModel)
	}
	saved, err := m.store.GetSelectedModel(context.Background())
	if err != nil || saved != "qwen2.5:7b" {
		t.Errorf("saved %q err %v, want the picked model", saved, err)
	}
}
