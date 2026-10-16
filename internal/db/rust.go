package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
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

// ErrActiveCap is returned when a skill is made active while
// rpg.MaxActiveSkills others already are.
var ErrActiveCap = fmt.Errorf("%d skills are already active; freeze one first", rpg.MaxActiveSkills)

// activeSkillsTx counts the active skills.
func activeSkillsTx(ctx context.Context, q querier) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE is_active = 1`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active skills: %w", err)
	}
	return n, nil
}

// SetSkillActive puts a skill in the active stance (it can rust) or the
// garrisoned stance (it is parked and never rusts). A skill is made active
// only while fewer than rpg.MaxActiveSkills others are (ErrActiveCap).
func (s *Store) SetSkillActive(ctx context.Context, id int64, active bool) error {
	if active {
		var n int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM skills WHERE is_active = 1 AND id != ?`, id).Scan(&n); err != nil {
			return fmt.Errorf("count active skills: %w", err)
		}
		if n >= rpg.MaxActiveSkills {
			return ErrActiveCap
		}
	}
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
