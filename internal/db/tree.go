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

// Node is one step in a skill's tree.
type Node struct {
	ID            int64
	SkillID       int64
	ParentID      int64 // 0 means the node hangs off the skill itself
	ExtraParentID int64 // a second parent, 0 for none
	Era           int   // 1 for the first tree, then one more per expansion
	IsCapstone    bool  // the node its era ends on
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
	Parent int
	// MoreParents are indexes of further parents in the same slice; at
	// most one is stored.
	MoreParents   []int
	Capstone      bool
	Name          string
	Description   string
	RequiredLevel int
	XPNeeded      int
	Tasks         []NewNodeTask // curated tasks, in the order to do them
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
	// so it never rusts until it is brought back to the front. A skill is
	// also created parked when rpg.MaxActiveSkills are already active.
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
	return inTx(ctx, s, func(tx *sql.Tx) (Skill, error) { return createSkillTreeTx(ctx, tx, t) })
}

func createSkillTreeTx(ctx context.Context, tx *sql.Tx, t NewSkillTree) (Skill, error) {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return Skill{}, errors.New("skill name is required")
	}
	startLevel := max(t.StartLevel, 1)
	startXP := rpg.TotalXPForLevel(startLevel)
	now := time.Now()

	var exists int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE name = ?`, name).Scan(&exists)
	if err != nil {
		return Skill{}, fmt.Errorf("check skill: %w", err)
	}
	if exists > 0 {
		return Skill{}, fmt.Errorf("skill %q already exists", name)
	}

	active, err := activeSkillsTx(ctx, tx)
	if err != nil {
		return Skill{}, err
	}
	garrisoned := t.Garrisoned || active >= rpg.MaxActiveSkills
	result, err := tx.ExecContext(ctx,
		`INSERT INTO skills (name, category, total_xp, is_active, last_practiced_at) VALUES (?, ?, ?, ?, ?)`,
		name, t.Category, startXP, boolInt(!garrisoned), formatPracticed(now),
	)
	if err != nil {
		return Skill{}, fmt.Errorf("insert skill: %w", err)
	}
	skillID, err := result.LastInsertId()
	if err != nil {
		return Skill{}, fmt.Errorf("get skill id: %w", err)
	}

	if err := insertNodesTx(ctx, tx, skillID, t.Nodes, startLevel, t.Rusty, 1, 0); err != nil {
		return Skill{}, err
	}
	return Skill{ID: skillID, Name: name, Category: t.Category, TotalXP: startXP,
		IsActive: !garrisoned, LastPracticed: now}, nil
}

// ReplaceSkillNodes swaps an existing skill's tree, and its curated tasks,
// for new nodes. The skill keeps its XP and history; the new tree is
// calibrated to the skill's current level, so nodes below it start done.
// Directives and XP events that pointed at the old nodes keep their XP but
// lose the node link.
func (s *Store) ReplaceSkillNodes(ctx context.Context, skillName string, nodes []RoadmapNode) (Skill, error) {
	return inTx(ctx, s, func(tx *sql.Tx) (Skill, error) { return replaceSkillNodesTx(ctx, tx, skillName, nodes) })
}

func replaceSkillNodesTx(ctx context.Context, tx *sql.Tx, skillName string, nodes []RoadmapNode) (Skill, error) {
	var sk Skill
	if err := tx.QueryRowContext(ctx,
		`SELECT id, name, category, total_xp FROM skills WHERE name = ?`, strings.TrimSpace(skillName),
	).Scan(&sk.ID, &sk.Name, &sk.Category, &sk.TotalXP); err != nil {
		return Skill{}, fmt.Errorf("skill %q: %w", skillName, err)
	}
	for _, q := range []string{
		`UPDATE challenges SET node_id = 0, node_task_id = 0 WHERE node_id IN (SELECT id FROM skill_nodes WHERE skill_id = ?)`,
		`UPDATE xp_events SET node_id = 0 WHERE node_id IN (SELECT id FROM skill_nodes WHERE skill_id = ?)`,
		`DELETE FROM node_tasks WHERE skill_id = ?`,
		`DELETE FROM skill_nodes WHERE skill_id = ?`,
		`DELETE FROM skill_eras WHERE skill_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, sk.ID); err != nil {
			return Skill{}, fmt.Errorf("clear old tree: %w", err)
		}
	}
	level := rpg.ProgressFor(sk.TotalXP).Level
	if err := insertNodesTx(ctx, tx, sk.ID, nodes, level, false, 1, 0); err != nil {
		return Skill{}, err
	}
	return sk, nil
}

// inTx runs fn in a transaction and commits it when fn succeeds.
func inTx[T any](ctx context.Context, s *Store, fn func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	v, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("commit: %w", err)
	}
	return v, nil
}

// insertNodesTx saves a skill's nodes into era, calibrated to startLevel.
// A node with no parent hangs off rootParent: 0, the skill itself, for the
// first era, or the capstone of the era before. With rusty, the frontier
// node starts rusty.
func insertNodesTx(ctx context.Context, tx *sql.Tx, skillID int64, nodes []RoadmapNode, startLevel int, rusty bool, era int, rootParent int64) error {
	ids := make([]int64, len(nodes))
	placed := make([]rpg.TreeNode, 0, len(nodes))
	for i, n := range nodes {
		parentID := rootParent
		if n.Parent >= 0 {
			if n.Parent >= i {
				return fmt.Errorf("node %q has parent %d that does not come before it", n.Name, n.Parent)
			}
			parentID = ids[n.Parent]
		}
		var extraID int64
		switch {
		case len(n.MoreParents) > 1:
			return fmt.Errorf("node %q has %d extra parents; at most one is stored", n.Name, len(n.MoreParents))
		case len(n.MoreParents) == 1:
			j := n.MoreParents[0]
			if j < 0 || j >= i || j == n.Parent {
				return fmt.Errorf("node %q has extra parent %d that does not come before it", n.Name, j)
			}
			extraID = ids[j]
		}

		required := max(n.RequiredLevel, 1)
		xpNeeded := n.XPNeeded
		if xpNeeded <= 0 {
			xpNeeded = rpg.NodeXPNeeded(required)
		}
		progress := rpg.StartProgress(required, startLevel, xpNeeded)

		result, err := tx.ExecContext(ctx,
			`INSERT INTO skill_nodes (skill_id, parent_id, extra_parent_id, era, is_capstone,
			                          name, description, required_level, xp_needed, progress)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			skillID, parentID, extraID, max(era, 1), boolInt(n.Capstone), n.Name, n.Description, required, xpNeeded, progress,
		)
		if err != nil {
			return fmt.Errorf("insert node %q: %w", n.Name, err)
		}
		ids[i], err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("get node id: %w", err)
		}
		placed = append(placed, rpg.TreeNode{ID: ids[i], ParentID: parentID, ExtraParentID: extraID,
			Required: required, Progress: progress, XPNeeded: xpNeeded})
		if err := insertNodeTasksTx(ctx, tx, skillID, ids[i], n.Tasks); err != nil {
			return err
		}
	}

	if rusty {
		if id, ok := rpg.FrontierNode(placed, startLevel); ok {
			if _, err := tx.ExecContext(ctx, `UPDATE skill_nodes SET is_rusty = 1 WHERE id = ?`, id); err != nil {
				return fmt.Errorf("mark rusty: %w", err)
			}
		}
	}

	return nil
}

// ListNodes returns every node across all skills, parents before children.
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.skill_id, n.parent_id, n.extra_parent_id, n.era, n.is_capstone, s.name, n.name, n.description,
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
		if err := rows.Scan(&n.ID, &n.SkillID, &n.ParentID, &n.ExtraParentID, &n.Era, &n.IsCapstone, &n.SkillName, &n.Name, &n.Description,
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
