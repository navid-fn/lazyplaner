package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/internal/cli"
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

	// OLLAMA_MODEL wins, then the model saved from the picker, then the
	// default. The UI checks it against the installed models once Ollama
	// answers, and switches to one that is installed if it is not.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	saved, err := store.GetSelectedModel(ctx)
	cancel()
	if err != nil {
		fmt.Printf("Failed to read settings: %v\n", err)
		os.Exit(1)
	}
	client := llm.NewClient(cfg.OllamaBaseURL, config.ResolveModel(cfg.OllamaModel, saved))

	// With a subcommand, run headless and print JSON instead of the UI.
	if len(os.Args) > 1 {
		if !cli.IsCommand(os.Args[1]) {
			fmt.Fprint(os.Stderr, cli.Usage())
			store.Close()
			os.Exit(2)
		}
		code := cli.Main(store, client, os.Args[1:])
		store.Close()
		os.Exit(code)
	}

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
