package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// A skill's tree grows in eras. Era 1 is the tree the skill was created
// with; each expansion appends the next era of nodes, whose two foundations
// hang off the capstone of the era before. Nothing is deleted, so progress
// and history stay.

// Era is the title of one era of a skill's tree.
type Era struct {
	SkillID int64
	Era     int
	Title   string
}

// NewEra is the next era of a skill's tree, ready to save.
type NewEra struct {
	SkillID int64
	// Era must be one more than the skill's latest era. It is checked again
	// inside the transaction, so two expansions at once cannot both land.
	Era int
	// CapstoneID is the node the era's foundations hang off.
	CapstoneID int64
	Title      string
	// Nodes must list parents before their children. A node with Parent -1
	// hangs off CapstoneID.
	Nodes []RoadmapNode
	// Level is the skill's level now; no node of a new era starts done.
	Level int
	// PlanSubject and PlanBody, when set, save a master plan for the era.
	PlanSubject string
	PlanBody    string
}

// AppendEra saves the next era of a skill's tree, and its master plan if it
// has one, in one transaction. It returns the plan, or a zero Plan.
func (s *Store) AppendEra(ctx context.Context, e NewEra) (Plan, error) {
	return inTx(ctx, s, func(tx *sql.Tx) (Plan, error) {
		var latest int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(era), 0) FROM skill_nodes WHERE skill_id = ?`, e.SkillID,
		).Scan(&latest); err != nil {
			return Plan{}, fmt.Errorf("find latest era: %w", err)
		}
		if latest == 0 {
			return Plan{}, fmt.Errorf("skill %d has no tree to expand", e.SkillID)
		}
		if e.Era != latest+1 {
			return Plan{}, fmt.Errorf("the tree is at era %d, so the next era is %d, not %d", latest, latest+1, e.Era)
		}
		var owner int64
		if err := tx.QueryRowContext(ctx,
			`SELECT skill_id FROM skill_nodes WHERE id = ?`, e.CapstoneID,
		).Scan(&owner); err != nil || owner != e.SkillID {
			return Plan{}, fmt.Errorf("capstone %d is not a node of skill %d", e.CapstoneID, e.SkillID)
		}

		if err := insertNodesTx(ctx, tx, e.SkillID, e.Nodes, e.Level, false, e.Era, e.CapstoneID); err != nil {
			return Plan{}, err
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO skill_eras (skill_id, era, title, created_at) VALUES (?, ?, ?, ?)`,
			e.SkillID, e.Era, strings.TrimSpace(e.Title), now,
		); err != nil {
			return Plan{}, fmt.Errorf("save era: %w", err)
		}

		if e.PlanSubject == "" {
			return Plan{}, nil
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO plans (subject, detail, description, created_at) VALUES (?, ?, ?, ?)`,
			e.PlanSubject, e.PlanSubject, e.PlanBody, now)
		if err != nil {
			return Plan{}, fmt.Errorf("insert era plan: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return Plan{}, fmt.Errorf("get plan id: %w", err)
		}
		return Plan{ID: id, Subject: e.PlanSubject, Detail: e.PlanSubject, Description: e.PlanBody, CreatedAt: now}, nil
	})
}

// ListEras returns the saved era titles of every skill, by skill and era.
func (s *Store) ListEras(ctx context.Context) ([]Era, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT skill_id, era, title FROM skill_eras ORDER BY skill_id, era`)
	if err != nil {
		return nil, fmt.Errorf("list eras: %w", err)
	}
	defer rows.Close()
	var out []Era
	for rows.Next() {
		var e Era
		if err := rows.Scan(&e.SkillID, &e.Era, &e.Title); err != nil {
			return nil, fmt.Errorf("scan era: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// oldTreeParents is the wiring of trees made before the diamond was closed:
// the parent index of each of the first eight nodes, -1 for the skill.
var oldTreeParents = [8]int{-1, -1, 0, 0, 1, 3, 4, 5}

// oldTreeJoins are the second parents that close those trees' diamonds, by
// node index, as rpg wires new trees.
var oldTreeJoins = map[int]int{5: 2, 6: 3, 7: 6}

// backfillDiamonds brings trees made by older versions up to the current
// shape, and is safe to run on every start:
//
//   - a first era wired the old way, with no second parents yet, gets the
//     joins that close its diamond and its eighth node marked capstone;
//   - any other skill whose latest era has no capstone gets one: the node
//     of that era with the highest required level (the oldest on a tie).
func (s *Store) backfillDiamonds(ctx context.Context) error {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return err
	}
	bySkill := map[int64][]Node{}
	var order []int64
	for _, n := range nodes {
		if _, ok := bySkill[n.SkillID]; !ok {
			order = append(order, n.SkillID)
		}
		bySkill[n.SkillID] = append(bySkill[n.SkillID], n)
	}

	for _, skillID := range order {
		list := bySkill[skillID]
		if joins := oldDiamondJoins(list); joins != nil {
			for id, extra := range joins {
				if _, err := s.db.ExecContext(ctx,
					`UPDATE skill_nodes SET extra_parent_id = ? WHERE id = ?`, extra, id); err != nil {
					return fmt.Errorf("close diamond: %w", err)
				}
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE skill_nodes SET is_capstone = 1 WHERE id = ?`, list[7].ID); err != nil {
				return fmt.Errorf("mark capstone: %w", err)
			}
			continue
		}

		latest := 0
		for _, n := range list {
			latest = max(latest, n.Era)
		}
		best := -1
		for i, n := range list {
			if n.Era != latest {
				continue
			}
			if n.IsCapstone {
				best = -1
				break
			}
			if best < 0 || n.RequiredLevel > list[best].RequiredLevel {
				best = i
			}
		}
		if best >= 0 {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE skill_nodes SET is_capstone = 1 WHERE id = ?`, list[best].ID); err != nil {
				return fmt.Errorf("mark capstone: %w", err)
			}
		}
	}
	return nil
}

// oldDiamondJoins returns the second parent to set on each node of a first
// era still wired the old way, keyed by node ID, or nil when the skill's
// nodes are not such a tree.
func oldDiamondJoins(list []Node) map[int64]int64 {
	if len(list) < 8 {
		return nil
	}
	first := list[:8]
	index := map[int64]int{}
	for i, n := range first {
		if n.Era != 1 || n.ExtraParentID != 0 || n.IsCapstone {
			return nil
		}
		index[n.ID] = i
	}
	for i, n := range first {
		want := oldTreeParents[i]
		if want < 0 {
			if n.ParentID != 0 {
				return nil
			}
			continue
		}
		if got, ok := index[n.ParentID]; !ok || got != want {
			return nil
		}
	}
	joins := map[int64]int64{}
	for i, j := range oldTreeJoins {
		joins[first[i].ID] = first[j].ID
	}
	return joins
}
