// Package config loads runtime settings from environment variables.
package config

import "os"

// Config holds the settings the app needs at startup.
type Config struct {
	DBPath        string
	OllamaBaseURL string
	OllamaModel   string
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
