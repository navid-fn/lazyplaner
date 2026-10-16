package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// NodeTask is a curated exercise for a skill node, designed ahead of time by
// an external agent. A node's tasks are done in Position order; the next
// unfinished one is pulled into the day's directives before the local model
// is asked for anything.
type NodeTask struct {
	ID          int64
	SkillID     int64
	NodeID      int64
	Position    int // 1 for the first task of the node
	Title       string
	Description string
	Details     string // markdown execution guide
	Difficulty  int
	Minutes     int
	Completed   bool
}

// NewNodeTask is a curated task to save with its node.
type NewNodeTask struct {
	Title       string
	Description string
	Details     string
	Difficulty  int
	Minutes     int
}

func insertNodeTasksTx(ctx context.Context, tx *sql.Tx, skillID, nodeID int64, tasks []NewNodeTask) error {
	for i, t := range tasks {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO node_tasks (skill_id, node_id, position, title, description, details, difficulty, minutes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			skillID, nodeID, i+1, t.Title, t.Description, t.Details, t.Difficulty, t.Minutes,
		); err != nil {
			return fmt.Errorf("insert task %q: %w", t.Title, err)
		}
	}
	return nil
}

const nodeTaskColumns = `id, skill_id, node_id, position, title, description, details, difficulty, minutes, is_completed`

func scanNodeTasks(rows *sql.Rows) ([]NodeTask, error) {
	defer rows.Close()
	var out []NodeTask
	for rows.Next() {
		var t NodeTask
		if err := rows.Scan(&t.ID, &t.SkillID, &t.NodeID, &t.Position, &t.Title, &t.Description, &t.Details,
			&t.Difficulty, &t.Minutes, &t.Completed); err != nil {
			return nil, fmt.Errorf("scan node task: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return out, nil
}

// ListNodeTasks returns every curated task, by node and position.
func (s *Store) ListNodeTasks(ctx context.Context) ([]NodeTask, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+nodeTaskColumns+` FROM node_tasks ORDER BY node_id, position`)
	if err != nil {
		return nil, fmt.Errorf("list node tasks: %w", err)
	}
	return scanNodeTasks(rows)
}

// NextNodeTasks returns each node's next unfinished task, keyed by node ID.
// Nodes whose queue is empty or done are left out.
func (s *Store) NextNodeTasks(ctx context.Context) (map[int64]NodeTask, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+nodeTaskColumns+` FROM node_tasks t
		 WHERE is_completed = 0 AND position = (
		   SELECT MIN(position) FROM node_tasks WHERE node_id = t.node_id AND is_completed = 0)`)
	if err != nil {
		return nil, fmt.Errorf("next node tasks: %w", err)
	}
	tasks, err := scanNodeTasks(rows)
	if err != nil {
		return nil, err
	}
	next := make(map[int64]NodeTask, len(tasks))
	for _, t := range tasks {
		next[t.NodeID] = t
	}
	return next, nil
}

// Blueprint is everything an external agent sets up for one skill in one
// go: the skill with its tree and curated tasks, and a master plan that
// lands in the plan list as the person's reference guide.
type Blueprint struct {
	Tree NewSkillTree
	// Replace swaps the tree of the existing skill named Tree.Name instead
	// of creating it (see ReplaceSkillNodes).
	Replace     bool
	PlanSubject string
	PlanBody    string // markdown
}

// ApplyBlueprint saves a blueprint in one transaction: if any part fails,
// nothing is saved.
func (s *Store) ApplyBlueprint(ctx context.Context, b Blueprint) (Skill, Plan, error) {
	type result struct {
		skill Skill
		plan  Plan
	}
	r, err := inTx(ctx, s, func(tx *sql.Tx) (result, error) {
		var r result
		var err error
		if b.Replace {
			r.skill, err = replaceSkillNodesTx(ctx, tx, b.Tree.Name, b.Tree.Nodes)
		} else {
			r.skill, err = createSkillTreeTx(ctx, tx, b.Tree)
		}
		if err != nil {
			return r, err
		}
		createdAt := time.Now().UTC()
		res, err := tx.ExecContext(ctx,
			`INSERT INTO plans (subject, detail, description, created_at) VALUES (?, ?, ?, ?)`,
			b.PlanSubject, b.PlanSubject, b.PlanBody, createdAt)
		if err != nil {
			return r, fmt.Errorf("insert master plan: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return r, fmt.Errorf("get plan id: %w", err)
		}
		r.plan = Plan{ID: id, Subject: b.PlanSubject, Detail: b.PlanSubject, Description: b.PlanBody, CreatedAt: createdAt}
		return r, nil
	})
	return r.skill, r.plan, err
}
