// Package config loads runtime settings from environment variables.
package config

import (
	"os"
	"strings"
)

// DefaultOllamaModel is the model used when neither OLLAMA_MODEL nor a saved
// choice names one. If it is not installed either, the app switches to an
// installed model once Ollama lists them.
const DefaultOllamaModel = "deepseek-v2:latest"

// Config holds the settings the app needs at startup.
type Config struct {
	DBPath        string
	OllamaBaseURL string
	// OllamaModel is OLLAMA_MODEL as set in the environment, "" when unset.
	OllamaModel string
}

// ResolveModel picks the model to start with: OLLAMA_MODEL when set, else
// the model saved from the picker, else DefaultOllamaModel.
func ResolveModel(env, saved string) string {
	for _, m := range []string{env, saved} {
		if m = strings.TrimSpace(m); m != "" {
			return m
		}
	}
	return DefaultOllamaModel
}

// Load reads settings from the environment, falling back to defaults.
func Load() Config {
	dbPath := os.Getenv("LAZYPLANER_DB_PATH")
	if dbPath == "" {
		dbPath = "./lazyplaner.db"
	}

	return Config{
		DBPath:        dbPath,
		OllamaBaseURL: os.Getenv("OLLAMA_BASE_URL"),
		OllamaModel:   os.Getenv("OLLAMA_MODEL"),
	}
}
