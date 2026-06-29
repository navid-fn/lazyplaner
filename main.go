package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/db"
	"github.com/navid-fn/lazyplaner/llm"
	"github.com/navid-fn/lazyplaner/ui"
)

func main() {
	dbPath := os.Getenv("LAZYPLANER_DB_PATH")
	if dbPath == "" {
		dbPath = "./lazyplaner.db"
	}

	store, err := db.NewStore(dbPath)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ollamaBaseURL := os.Getenv("OLLAMA_BASE_URL")
	ollamaModel := os.Getenv("OLLAMA_MODEL")
	client := llm.NewClient(ollamaBaseURL, ollamaModel)

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
