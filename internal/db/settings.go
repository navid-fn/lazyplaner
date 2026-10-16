package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// modelKey is the settings key of the model chosen in the picker.
const modelKey = "ollama_model"

// GetSelectedModel returns the model saved from the picker, or "" when none
// has been saved.
func (s *Store) GetSelectedModel(ctx context.Context) (string, error) {
	name, _, err := s.GetSetting(ctx, modelKey)
	return name, err
}

// SetSelectedModel saves the model chosen in the picker, so it is used again
// on the next start.
func (s *Store) SetSelectedModel(ctx context.Context, name string) error {
	return s.SetSetting(ctx, modelKey, name)
}

// GetSetting returns the value stored under key, and whether it exists.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, true, nil
}

// SetSetting stores value under key, replacing any earlier value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}
