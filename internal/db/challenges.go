package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// dayLayout is the format of Challenge.Day. Local dates keep "today" aligned
// with the person's day, not UTC.
const dayLayout = "2006-01-02"

// Today returns the local date in dayLayout.
func Today() string {
	return time.Now().Format(dayLayout)
}

// Challenge is one daily task. Done is true once it has been ticked off.
type Challenge struct {
	ID          int64
	Day         string
	SkillName   string
	Category    string
	NodeName    string
	Title       string
	Description string
	Difficulty  int
	Minutes     int
	XP          int
	Done        bool
}

// NewChallenge is a challenge to save. Skill and node are matched by name.
// An empty node name leaves the challenge attached to the skill only.
type NewChallenge struct {
	SkillName   string
	NodeName    string
	Title       string
	Description string
	Difficulty  int
	Minutes     int
	XP          int
}

// ChallengeStats summarises today's challenges and the running score.
type ChallengeStats struct {
	Done   int // challenges ticked off today
	Total  int // challenges generated for today
	Score  int // XP earned from all completed challenges
	Streak int // consecutive days with at least one completed challenge
}

// SaveChallenges stores the challenges for a day in one transaction. Skill
// names must already exist.
func (s *Store) SaveChallenges(ctx context.Context, day string, items []NewChallenge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// A new set for the day is the next batch number.
	var batch int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(batch), 0) + 1 FROM challenges WHERE day = ?`, day,
	).Scan(&batch); err != nil {
		return fmt.Errorf("next batch: %w", err)
	}

	for _, c := range items {
		var skillID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM skills WHERE name = ?`, c.SkillName,
		).Scan(&skillID); err != nil {
			return fmt.Errorf("skill %q: %w", c.SkillName, err)
		}

		var nodeID int64
		if c.NodeName != "" {
			// An unknown node is saved without one rather than failing.
			_ = tx.QueryRowContext(ctx,
				`SELECT id FROM skill_nodes WHERE skill_id = ? AND name = ?`, skillID, c.NodeName,
			).Scan(&nodeID)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO challenges (day, batch, skill_id, node_id, title, description, difficulty, minutes, xp)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			day, batch, skillID, nodeID, c.Title, c.Description, c.Difficulty, c.Minutes, c.XP,
		); err != nil {
			return fmt.Errorf("insert challenge: %w", err)
		}
	}
	return tx.Commit()
}

// ListChallenges returns the challenges for one day, in the order they were
// saved.
func (s *Store) ListChallenges(ctx context.Context, day string) ([]Challenge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.day, s.name, s.category, COALESCE(n.name, ''), c.title, c.description,
		        c.difficulty, c.minutes, c.xp, c.done_at IS NOT NULL
		 FROM challenges c
		 JOIN skills s ON s.id = c.skill_id
		 LEFT JOIN skill_nodes n ON n.id = c.node_id
		 WHERE c.day = ? AND c.batch = (SELECT MAX(batch) FROM challenges WHERE day = ?)
		 ORDER BY c.id`,
		day, day,
	)
	if err != nil {
		return nil, fmt.Errorf("list challenges: %w", err)
	}
	defer rows.Close()

	var out []Challenge
	for rows.Next() {
		var c Challenge
		if err := rows.Scan(&c.ID, &c.Day, &c.SkillName, &c.Category, &c.NodeName, &c.Title,
			&c.Description, &c.Difficulty, &c.Minutes, &c.XP, &c.Done); err != nil {
			return nil, fmt.Errorf("scan challenge: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return out, nil
}

// CompleteChallenge ticks a challenge off and awards its XP to its skill and
// node. It fails if the challenge is already done.
func (s *Store) CompleteChallenge(ctx context.Context, id int64) (XPEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return XPEvent{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var (
		c        Challenge
		skillID  int64
		nodeID   int64
		doneAt   sql.NullTime
		skillCat string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT c.id, c.day, c.skill_id, c.node_id, s.name, s.category, c.title,
		        c.difficulty, c.minutes, c.xp, c.done_at
		 FROM challenges c JOIN skills s ON s.id = c.skill_id
		 WHERE c.id = ?`, id,
	).Scan(&c.ID, &c.Day, &skillID, &nodeID, &c.SkillName, &skillCat, &c.Title,
		&c.Difficulty, &c.Minutes, &c.XP, &doneAt)
	if errors.Is(err, sql.ErrNoRows) {
		return XPEvent{}, fmt.Errorf("challenge %d not found", id)
	}
	if err != nil {
		return XPEvent{}, fmt.Errorf("load challenge: %w", err)
	}
	if doneAt.Valid {
		return XPEvent{}, errors.New("challenge is already done")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE challenges SET done_at = ? WHERE id = ?`, time.Now().UTC(), id,
	); err != nil {
		return XPEvent{}, fmt.Errorf("mark challenge done: %w", err)
	}

	ev, err := recordXPTx(ctx, tx, XPAward{
		Activity:   "Challenge: " + c.Title,
		SkillName:  c.SkillName,
		Category:   skillCat,
		Difficulty: c.Difficulty,
		Minutes:    c.Minutes,
		XP:         c.XP,
		NodeID:     nodeID,
	})
	if err != nil {
		return XPEvent{}, err
	}

	// A comeback bonus raises the award; store it on the challenge too, so
	// the score counts what was actually earned.
	if ev.XP != c.XP {
		if _, err := tx.ExecContext(ctx, `UPDATE challenges SET xp = ? WHERE id = ?`, ev.XP, id); err != nil {
			return XPEvent{}, fmt.Errorf("record comeback xp: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return XPEvent{}, fmt.Errorf("commit challenge: %w", err)
	}
	return ev, nil
}

// ChallengeStats returns today's progress along with the running score and
// streak. today is in dayLayout.
func (s *Store) ChallengeStats(ctx context.Context, today string) (ChallengeStats, error) {
	var st ChallengeStats

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(done_at IS NOT NULL), 0) FROM challenges
		 WHERE day = ? AND batch = (SELECT MAX(batch) FROM challenges WHERE day = ?)`, today, today,
	).Scan(&st.Total, &st.Done); err != nil {
		return st, fmt.Errorf("count today's challenges: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(xp), 0) FROM challenges WHERE done_at IS NOT NULL`,
	).Scan(&st.Score); err != nil {
		return st, fmt.Errorf("sum score: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT day FROM challenges WHERE done_at IS NOT NULL`,
	)
	if err != nil {
		return st, fmt.Errorf("list active days: %w", err)
	}
	defer rows.Close()

	days := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return st, fmt.Errorf("scan day: %w", err)
		}
		days[d] = true
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("rows error: %w", err)
	}

	today0, err := time.ParseInLocation(dayLayout, today, time.Local)
	if err != nil {
		return st, fmt.Errorf("parse today: %w", err)
	}
	st.Streak = streakEndingAt(days, today0)
	return st, nil
}

// streakEndingAt counts consecutive active days ending at today. If today has
// no completed challenge yet, the streak counts back from yesterday, so it
// is not lost before the day is over.
func streakEndingAt(days map[string]bool, today time.Time) int {
	d := today
	if !days[d.Format(dayLayout)] {
		d = d.AddDate(0, 0, -1)
	}
	n := 0
	for days[d.Format(dayLayout)] {
		n++
		d = d.AddDate(0, 0, -1)
	}
	return n
}
