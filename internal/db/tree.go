package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Node is one step in a skill's tree.
type Node struct {
	ID            int64
	SkillID       int64
	ParentID      int64 // 0 means the node hangs off the skill itself
	SkillName     string
	Name          string
	Description   string
	RequiredLevel int
	XPNeeded      int
	Progress      int
	IsRusty       bool // marked rusty and not practised since
}

// SkillTree is a skill together with all of its nodes.
type SkillTree struct {
	Skill Skill
	Nodes []Node
}

// RoadmapNode is a node as produced by the model, before it is saved.
// Parent is the index of its parent in the same slice, or -1 for none.
type RoadmapNode struct {
	Parent        int
	Name          string
	Description   string
	RequiredLevel int
	XPNeeded      int
}

// NewSkillTree is a skill to create from the setup screen, with its
// generated tree.
type NewSkillTree struct {
	Name     string
	Category string
	// StartLevel is the level the person already has. The skill starts with
	// the XP for that level, and nodes below it start done.
	StartLevel int
	// Rusty marks the skill's frontier node rusty from the start, for
	// someone who used to practise but stopped.
	Rusty bool
	// Garrisoned creates the skill parked (is_active 0) instead of active,
	// so it never rusts until it is brought back to the front.
	Garrisoned bool
	// Nodes must list parents before their children.
	Nodes []RoadmapNode
}

// CreateSkillTree saves a new skill and its nodes in one transaction.
//
// The tree is calibrated to the start level: nodes that require less than
// it are already mastered and start done, nodes at it start open and empty,
// and nodes above it start locked. The practice clock starts now, so an
// untouched skill rusts after rpg.RustAfter like any other.
func (s *Store) CreateSkillTree(ctx context.Context, t NewSkillTree) (Skill, error) {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return Skill{}, errors.New("skill name is required")
	}
	startLevel := max(t.StartLevel, 1)
	startXP := rpg.TotalXPForLevel(startLevel)
	now := time.Now()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Skill{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE name = ?`, name).Scan(&exists)
	if err != nil {
		return Skill{}, fmt.Errorf("check skill: %w", err)
	}
	if exists > 0 {
		return Skill{}, fmt.Errorf("skill %q already exists", name)
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO skills (name, category, total_xp, is_active, last_practiced_at) VALUES (?, ?, ?, ?, ?)`,
		name, t.Category, startXP, boolInt(!t.Garrisoned), formatPracticed(now),
	)
	if err != nil {
		return Skill{}, fmt.Errorf("insert skill: %w", err)
	}
	skillID, err := result.LastInsertId()
	if err != nil {
		return Skill{}, fmt.Errorf("get skill id: %w", err)
	}

	ids := make([]int64, len(t.Nodes))
	placed := make([]rpg.TreeNode, 0, len(t.Nodes))
	for i, n := range t.Nodes {
		var parentID int64
		if n.Parent >= 0 {
			if n.Parent >= i {
				return Skill{}, fmt.Errorf("node %q has parent %d that does not come before it", n.Name, n.Parent)
			}
			parentID = ids[n.Parent]
		}

		required := max(n.RequiredLevel, 1)
		xpNeeded := n.XPNeeded
		if xpNeeded <= 0 {
			xpNeeded = rpg.NodeXPNeeded(required)
		}
		progress := rpg.StartProgress(required, startLevel, xpNeeded)

		result, err := tx.ExecContext(ctx,
			`INSERT INTO skill_nodes (skill_id, parent_id, name, description, required_level, xp_needed, progress)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			skillID, parentID, n.Name, n.Description, required, xpNeeded, progress,
		)
		if err != nil {
			return Skill{}, fmt.Errorf("insert node %q: %w", n.Name, err)
		}
		ids[i], err = result.LastInsertId()
		if err != nil {
			return Skill{}, fmt.Errorf("get node id: %w", err)
		}
		placed = append(placed, rpg.TreeNode{ID: ids[i], ParentID: parentID,
			Required: required, Progress: progress, XPNeeded: xpNeeded})
	}

	if t.Rusty {
		if id, ok := rpg.FrontierNode(placed, startLevel); ok {
			if _, err := tx.ExecContext(ctx, `UPDATE skill_nodes SET is_rusty = 1 WHERE id = ?`, id); err != nil {
				return Skill{}, fmt.Errorf("mark rusty: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return Skill{}, fmt.Errorf("commit skill tree: %w", err)
	}
	return Skill{ID: skillID, Name: name, Category: t.Category, TotalXP: startXP,
		IsActive: !t.Garrisoned, LastPracticed: now}, nil
}

// ListNodes returns every node across all skills, parents before children.
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.skill_id, n.parent_id, s.name, n.name, n.description,
		        n.required_level, n.xp_needed, n.progress, n.is_rusty
		 FROM skill_nodes n JOIN skills s ON s.id = n.skill_id
		 ORDER BY n.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.SkillID, &n.ParentID, &n.SkillName, &n.Name, &n.Description,
			&n.RequiredLevel, &n.XPNeeded, &n.Progress, &n.IsRusty); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return nodes, nil
}

// SkillTrees returns every skill with its nodes, highest total XP first.
func (s *Store) SkillTrees(ctx context.Context) ([]SkillTree, error) {
	skills, err := s.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	trees := make([]SkillTree, len(skills))
	index := make(map[int64]int, len(skills))
	for i, sk := range skills {
		trees[i] = SkillTree{Skill: sk}
		index[sk.ID] = i
	}
	for _, n := range nodes {
		if i, ok := index[n.SkillID]; ok {
			trees[i].Nodes = append(trees[i].Nodes, n)
		}
	}
	return trees, nil
}

// UpdateSkill renames a skill, changes its category, and resets it to level
// 1: its total XP and the progress of every node in its tree go to 0, rust is
// cleared, and the practice clock restarts. The XP history is kept. The tree
// itself is kept too.
func (s *Store) UpdateSkill(ctx context.Context, id int64, name, category string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("skill name is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var clash int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM skills WHERE name = ? AND id != ?`, name, id,
	).Scan(&clash); err != nil {
		return fmt.Errorf("check name: %w", err)
	}
	if clash > 0 {
		return fmt.Errorf("skill %q already exists", name)
	}

	result, err := tx.ExecContext(ctx,
		`UPDATE skills SET name = ?, category = ?, total_xp = 0, last_practiced_at = ? WHERE id = ?`,
		name, category, formatPracticed(time.Now()), id,
	)
	if err != nil {
		return fmt.Errorf("update skill: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("skill %d not found", id)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE skill_nodes SET progress = 0, is_rusty = 0 WHERE skill_id = ?`, id,
	); err != nil {
		return fmt.Errorf("reset node progress: %w", err)
	}

	return tx.Commit()
}
