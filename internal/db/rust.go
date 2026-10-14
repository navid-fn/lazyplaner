package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// practicedLayout is how skills.last_practiced_at is written.
const practicedLayout = time.RFC3339Nano

// timeLayouts are the forms a stored time can take: the one this package
// writes, and the one the SQLite driver uses for time.Time values, which is
// what MAX(created_at) returns.
var timeLayouts = []string{
	practicedLayout,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05",
}

// formatPracticed renders a time for skills.last_practiced_at.
func formatPracticed(t time.Time) string {
	return t.UTC().Format(practicedLayout)
}

// parseStoredTime reads a time in any of the stored forms. An empty or
// unreadable value gives the zero time, which means "never".
func parseStoredTime(s string) time.Time {
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// backfillLastPracticed fills last_practiced_at for skills from before the
// column existed, using their most recent XP event. Skills that never earned
// XP stay empty, so they do not rust until they are practised once.
func (s *Store) backfillLastPracticed(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM skills WHERE last_practiced_at IS NULL`)
	if err != nil {
		return fmt.Errorf("find skills to backfill: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan skill id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	for _, id := range ids {
		var latest sql.NullString
		if err := s.db.QueryRowContext(ctx,
			`SELECT MAX(created_at) FROM xp_events WHERE skill_id = ?`, id,
		).Scan(&latest); err != nil {
			return fmt.Errorf("latest event for skill %d: %w", id, err)
		}
		t := parseStoredTime(latest.String)
		if t.IsZero() {
			continue
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE skills SET last_practiced_at = ? WHERE id = ?`, formatPracticed(t), id,
		); err != nil {
			return fmt.Errorf("backfill skill %d: %w", id, err)
		}
	}
	return nil
}

// SetNodeRusty marks a node rusty or clears the mark.
func (s *Store) SetNodeRusty(ctx context.Context, id int64, rusty bool) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE skill_nodes SET is_rusty = ? WHERE id = ?`, boolInt(rusty), id)
	if err != nil {
		return fmt.Errorf("set node rusty: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("node %d not found", id)
	}
	return nil
}

// SetSkillActive puts a skill in the active stance (it can rust) or the
// garrisoned stance (it is parked and never rusts).
func (s *Store) SetSkillActive(ctx context.Context, id int64, active bool) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE skills SET is_active = ? WHERE id = ?`, boolInt(active), id)
	if err != nil {
		return fmt.Errorf("set skill stance: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("skill %d not found", id)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
