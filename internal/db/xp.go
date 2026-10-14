package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Skill is one entry on the character sheet. Each skill owns a tree of nodes.
type Skill struct {
	ID       int64
	Name     string
	Category string
	TotalXP  int
	// IsActive is the active stance; a garrisoned skill (false) never rusts.
	IsActive bool
	// LastPracticed is when the skill last earned XP. Zero means never.
	LastPracticed time.Time
}

// XPEvent is one award of XP, shown on the road.
type XPEvent struct {
	ID         int64
	SkillName  string
	Category   string
	NodeName   string // empty when the award was not for a node
	PlanID     int64
	Activity   string
	Difficulty int
	Minutes    int
	XP         int
	CreatedAt  time.Time
	// Comeback is true when the award was on a rusty node, so XP includes
	// the comeback bonus. It is reported, not stored.
	Comeback bool
}

// XPAward is what RecordXP needs to store an event.
//
// The node is picked in one of two ways. NodeID names an existing node. If
// NodeID is 0 and NodeName is set, the node is found under the skill or, if
// it does not exist, created under ParentNode. NodeRequired and NodeXPNeeded
// are only used when a node is created.
type XPAward struct {
	PlanID     int64
	Activity   string
	SkillName  string
	Category   string
	Difficulty int
	Minutes    int
	XP         int

	NodeID       int64
	NodeName     string
	ParentNode   string
	NodeRequired int
	NodeXPNeeded int
}

// RecordXP adds an event and credits its skill and node.
func (s *Store) RecordXP(ctx context.Context, a XPAward) (XPEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return XPEvent{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	ev, err := recordXPTx(ctx, tx, a)
	if err != nil {
		return XPEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return XPEvent{}, fmt.Errorf("commit xp: %w", err)
	}
	return ev, nil
}

// recordXPTx does the work of RecordXP inside an existing transaction, so
// other writes such as completing a challenge can commit with it.
func recordXPTx(ctx context.Context, tx *sql.Tx, a XPAward) (XPEvent, error) {
	if strings.TrimSpace(a.SkillName) == "" {
		return XPEvent{}, errors.New("skill name is required")
	}
	if a.XP <= 0 {
		return XPEvent{}, errors.New("xp must be positive")
	}

	skill, err := findOrCreateSkill(ctx, tx, a.SkillName, a.Category)
	if err != nil {
		return XPEvent{}, err
	}

	nodeID, nodeName, err := resolveNode(ctx, tx, skill.ID, a)
	if err != nil {
		return XPEvent{}, err
	}

	// Practising a rusty node clears the rust and earns the comeback bonus.
	xp, comeback := a.XP, false
	if nodeID != 0 {
		var rusty int
		if err := tx.QueryRowContext(ctx,
			`SELECT is_rusty FROM skill_nodes WHERE id = ?`, nodeID,
		).Scan(&rusty); err != nil {
			return XPEvent{}, fmt.Errorf("check node rust: %w", err)
		}
		if rusty != 0 {
			xp, comeback = rpg.ComebackXP(a.XP), true
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE skill_nodes SET progress = progress + ?, is_rusty = 0 WHERE id = ?`,
			xp, nodeID,
		); err != nil {
			return XPEvent{}, fmt.Errorf("update node progress: %w", err)
		}
	}

	createdAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx,
		`UPDATE skills SET total_xp = total_xp + ?, last_practiced_at = ? WHERE id = ?`,
		xp, formatPracticed(createdAt), skill.ID,
	); err != nil {
		return XPEvent{}, fmt.Errorf("update skill xp: %w", err)
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO xp_events (skill_id, node_id, plan_id, activity, difficulty, minutes, xp, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		skill.ID, nodeID, a.PlanID, a.Activity, a.Difficulty, a.Minutes, xp, createdAt,
	)
	if err != nil {
		return XPEvent{}, fmt.Errorf("insert xp event: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return XPEvent{}, fmt.Errorf("get event id: %w", err)
	}

	return XPEvent{
		ID:         id,
		SkillName:  skill.Name,
		Category:   skill.Category,
		NodeName:   nodeName,
		PlanID:     a.PlanID,
		Activity:   a.Activity,
		Difficulty: a.Difficulty,
		Minutes:    a.Minutes,
		XP:         xp,
		CreatedAt:  createdAt,
		Comeback:   comeback,
	}, nil
}

// findOrCreateSkill returns the skill with this name, ignoring case, and
// creates it with no XP if it does not exist.
func findOrCreateSkill(ctx context.Context, tx *sql.Tx, name, category string) (Skill, error) {
	var skill Skill
	err := tx.QueryRowContext(ctx,
		`SELECT id, name, category, total_xp FROM skills WHERE name = ?`,
		name,
	).Scan(&skill.ID, &skill.Name, &skill.Category, &skill.TotalXP)
	if err == nil {
		return skill, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Skill{}, fmt.Errorf("find skill: %w", err)
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO skills (name, category, total_xp) VALUES (?, ?, 0)`,
		name, category,
	)
	if err != nil {
		return Skill{}, fmt.Errorf("insert skill: %w", err)
	}
	skill.ID, err = result.LastInsertId()
	if err != nil {
		return Skill{}, fmt.Errorf("get skill id: %w", err)
	}
	skill.Name = name
	skill.Category = category
	return skill, nil
}

// resolveNode returns the node to credit for an award, or 0 when the award
// has no node. It creates the node when the award names a new one.
func resolveNode(ctx context.Context, tx *sql.Tx, skillID int64, a XPAward) (int64, string, error) {
	if a.NodeID != 0 {
		var name string
		err := tx.QueryRowContext(ctx,
			`SELECT name FROM skill_nodes WHERE id = ? AND skill_id = ?`, a.NodeID, skillID,
		).Scan(&name)
		if err != nil {
			return 0, "", fmt.Errorf("node %d does not belong to this skill: %w", a.NodeID, err)
		}
		return a.NodeID, name, nil
	}

	name := strings.TrimSpace(a.NodeName)
	if name == "" {
		return 0, "", nil
	}

	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM skill_nodes WHERE skill_id = ? AND name = ?`, skillID, name,
	).Scan(&id)
	if err == nil {
		return id, name, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("find node: %w", err)
	}

	// A parent that is not in this tree is ignored and the node attaches to
	// the skill itself.
	var parentID int64
	if p := strings.TrimSpace(a.ParentNode); p != "" {
		_ = tx.QueryRowContext(ctx,
			`SELECT id FROM skill_nodes WHERE skill_id = ? AND name = ?`, skillID, p,
		).Scan(&parentID)
	}

	required := max(a.NodeRequired, 1)
	xpNeeded := a.NodeXPNeeded
	if xpNeeded <= 0 {
		xpNeeded = required * 40
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO skill_nodes (skill_id, parent_id, name, required_level, xp_needed)
		 VALUES (?, ?, ?, ?, ?)`,
		skillID, parentID, name, required, xpNeeded,
	)
	if err != nil {
		return 0, "", fmt.Errorf("insert node: %w", err)
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("get node id: %w", err)
	}
	return id, name, nil
}

// ListSkills returns every skill, highest total XP first.
func (s *Store) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, category, total_xp, is_active, last_practiced_at
		 FROM skills ORDER BY total_xp DESC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()

	var skills []Skill
	for rows.Next() {
		var sk Skill
		var practiced sql.NullString
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Category, &sk.TotalXP, &sk.IsActive, &practiced); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}
		sk.LastPracticed = parseStoredTime(practiced.String)
		skills = append(skills, sk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return skills, nil
}

// ListXPEvents returns the most recent events, newest first.
func (s *Store) ListXPEvents(ctx context.Context, limit int) ([]XPEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, s.name, s.category, COALESCE(n.name, ''), e.plan_id, e.activity,
		        e.difficulty, e.minutes, e.xp, e.created_at
		 FROM xp_events e
		 JOIN skills s ON s.id = e.skill_id
		 LEFT JOIN skill_nodes n ON n.id = e.node_id
		 ORDER BY e.created_at DESC, e.id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list xp events: %w", err)
	}
	defer rows.Close()

	var events []XPEvent
	for rows.Next() {
		var ev XPEvent
		if err := rows.Scan(&ev.ID, &ev.SkillName, &ev.Category, &ev.NodeName, &ev.PlanID,
			&ev.Activity, &ev.Difficulty, &ev.Minutes, &ev.XP, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan xp event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return events, nil
}
