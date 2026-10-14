package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

const (
	// DefaultDailyChallenges is the set size used until the person picks one.
	DefaultDailyChallenges = 3
	// MinDailyChallenges and MaxDailyChallenges bound the set size.
	MinDailyChallenges = 1
	MaxDailyChallenges = 5

	settingDailyChallenges = "daily_challenges"
)

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

// DailyChallengeCount returns how many challenges a new daily set should
// have. A missing or invalid value falls back to the default.
func (s *Store) DailyChallengeCount(ctx context.Context) (int, error) {
	value, ok, err := s.GetSetting(ctx, settingDailyChallenges)
	if err != nil || !ok {
		return DefaultDailyChallenges, err
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return DefaultDailyChallenges, nil
	}
	return min(max(n, MinDailyChallenges), MaxDailyChallenges), nil
}

// SetDailyChallengeCount saves how many challenges a new daily set should
// have, limited to the allowed range.
func (s *Store) SetDailyChallengeCount(ctx context.Context, n int) error {
	n = min(max(n, MinDailyChallenges), MaxDailyChallenges)
	return s.SetSetting(ctx, settingDailyChallenges, strconv.Itoa(n))
}
