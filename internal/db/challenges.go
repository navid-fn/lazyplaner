package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// dayLayout is the format of Challenge.Day. Local dates keep "today" aligned
// with the person's day, not UTC.
const dayLayout = "2006-01-02"

// Today returns the local date in dayLayout.
func Today() string {
	return time.Now().Format(dayLayout)
}

// Challenge is one daily directive. Done is true once it has been ticked
// off. Tier is its slot in the day's set; rpg.TierNone for directives made
// before tiers existed.
type Challenge struct {
	ID          int64
	Tier        rpg.Tier
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
	Tier        rpg.Tier
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

var (
	// ErrDaySet is returned when a day that already has its set is given
	// another one. A day gets one set; single directives are rerolled
	// instead.
	ErrDaySet = errors.New("today's directives are already set")
	// ErrSetUnfinished is returned when the bonus is asked for before every
	// directive in the day's set is done.
	ErrSetUnfinished = errors.New("finish today's directives first")
	// ErrBonusUsed is returned when the day's one bonus directive has
	// already been taken.
	ErrBonusUsed = errors.New("today's bonus directive is already used")
	// ErrNodeHasDirective is returned when a node already has a directive
	// today.
	ErrNodeHasDirective = errors.New("this node already has a directive today")
)

// SaveChallenges stores the set for a day in one transaction. Skill names
// must already exist. A day gets one set: it fails with ErrDaySet if the day
// already has directives, apart from focus directives asked for from the
// skill tree, which do not count as a set.
func (s *Store) SaveChallenges(ctx context.Context, day string, items []NewChallenge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var existing int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM challenges WHERE day = ? AND tier != ?`, day, int(rpg.TierFocus),
	).Scan(&existing); err != nil {
		return fmt.Errorf("count day: %w", err)
	}
	if existing > 0 {
		return ErrDaySet
	}
	batch, err := currentBatch(ctx, tx, day)
	if err != nil {
		return err
	}

	for _, c := range items {
		if err := insertChallengeTx(ctx, tx, day, batch, c); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RerollChallenge replaces one unfinished directive with c, in place, so it
// keeps its spot in the list. A finished directive cannot be rerolled.
func (s *Store) RerollChallenge(ctx context.Context, id int64, c NewChallenge) error {
	skillID, nodeID, err := resolveTarget(ctx, s.db, c)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE challenges SET tier = ?, skill_id = ?, node_id = ?, title = ?, description = ?,
		        difficulty = ?, minutes = ?, xp = ?
		 WHERE id = ? AND done_at IS NULL`,
		int(c.Tier), skillID, nodeID, c.Title, c.Description, c.Difficulty, c.Minutes, c.XP, id,
	)
	if err != nil {
		return fmt.Errorf("reroll challenge: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("challenge %d is done or missing", id)
	}
	return nil
}

// AddBonusChallenge adds the day's one bonus directive, as rpg.TierOvertime,
// to the end of the day's set. It needs every directive in the set done
// (ErrSetUnfinished) and works once per day (ErrBonusUsed).
func (s *Store) AddBonusChallenge(ctx context.Context, day string, c NewChallenge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	batch, err := currentBatch(ctx, tx, day)
	if err != nil {
		return err
	}
	var total, done, bonus int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(done_at IS NOT NULL), 0), COALESCE(SUM(tier = ?), 0)
		 FROM challenges WHERE day = ? AND batch = ?`, int(rpg.TierOvertime), day, batch,
	).Scan(&total, &done, &bonus); err != nil {
		return fmt.Errorf("count day: %w", err)
	}
	switch {
	case total == 0 || done < total:
		return ErrSetUnfinished
	case bonus > 0:
		return ErrBonusUsed
	}

	c.Tier = rpg.TierOvertime
	if err := insertChallengeTx(ctx, tx, day, batch, c); err != nil {
		return err
	}
	return tx.Commit()
}

// AddNodeChallenge adds a focus directive for one node, as rpg.TierFocus,
// to today's list. Each node gets at most one directive a day: it fails with
// ErrNodeHasDirective otherwise. c must name the node.
func (s *Store) AddNodeChallenge(ctx context.Context, day string, c NewChallenge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, nodeID, err := resolveTarget(ctx, tx, c)
	if err != nil {
		return err
	}
	if nodeID == 0 {
		return fmt.Errorf("node %q of %s not found", c.NodeName, c.SkillName)
	}
	var existing int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM challenges WHERE day = ? AND node_id = ?`, day, nodeID,
	).Scan(&existing); err != nil {
		return fmt.Errorf("count node: %w", err)
	}
	if existing > 0 {
		return ErrNodeHasDirective
	}
	batch, err := currentBatch(ctx, tx, day)
	if err != nil {
		return err
	}

	c.Tier = rpg.TierFocus
	if err := insertChallengeTx(ctx, tx, day, batch, c); err != nil {
		return err
	}
	return tx.Commit()
}

// currentBatch is the batch today's list is shown from (the newest), or 1
// for a day with nothing yet.
func currentBatch(ctx context.Context, tx *sql.Tx, day string) (int, error) {
	var batch int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(batch), 1) FROM challenges WHERE day = ?`, day,
	).Scan(&batch); err != nil {
		return 0, fmt.Errorf("current batch: %w", err)
	}
	return batch, nil
}

// querier is what both *sql.DB and *sql.Tx offer for single-row reads.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// resolveTarget looks up the skill and node a challenge names. An unknown
// node gives node ID 0 rather than failing.
func resolveTarget(ctx context.Context, q querier, c NewChallenge) (skillID, nodeID int64, err error) {
	if err := q.QueryRowContext(ctx,
		`SELECT id FROM skills WHERE name = ?`, c.SkillName,
	).Scan(&skillID); err != nil {
		return 0, 0, fmt.Errorf("skill %q: %w", c.SkillName, err)
	}
	if c.NodeName != "" {
		_ = q.QueryRowContext(ctx,
			`SELECT id FROM skill_nodes WHERE skill_id = ? AND name = ?`, skillID, c.NodeName,
		).Scan(&nodeID)
	}
	return skillID, nodeID, nil
}

func insertChallengeTx(ctx context.Context, tx *sql.Tx, day string, batch int, c NewChallenge) error {
	skillID, nodeID, err := resolveTarget(ctx, tx, c)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO challenges (day, batch, tier, skill_id, node_id, title, description, difficulty, minutes, xp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		day, batch, int(c.Tier), skillID, nodeID, c.Title, c.Description, c.Difficulty, c.Minutes, c.XP,
	); err != nil {
		return fmt.Errorf("insert challenge: %w", err)
	}
	return nil
}

// ListChallenges returns the challenges for one day, in the order they were
// saved.
func (s *Store) ListChallenges(ctx context.Context, day string) ([]Challenge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.tier, c.day, s.name, s.category, COALESCE(n.name, ''), c.title, c.description,
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
		if err := rows.Scan(&c.ID, &c.Tier, &c.Day, &c.SkillName, &c.Category, &c.NodeName, &c.Title,
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
