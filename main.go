package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/internal/config"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/llm"
	"github.com/navid-fn/lazyplaner/internal/ui"
)

func main() {
	cfg := config.Load()

	store, err := db.NewStore(cfg.DBPath)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	client := llm.NewClient(cfg.OllamaBaseURL, cfg.OllamaModel)

	p := tea.NewProgram(
		ui.NewModel(store, client),
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
