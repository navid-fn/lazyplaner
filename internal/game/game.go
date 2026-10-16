// Package game holds the game actions that need both the store and the
// rules, shared by the terminal UI and the headless CLI.
package game

import (
	"context"
	"time"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/llm"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// AwardPlanXP asks the model which skill and node the plan belongs to and
// records the XP.
func AwardPlanXP(client *llm.Client, store *db.Store, plan db.Plan) (*db.XPEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return nil, err
	}
	activity, err := rpg.Classify(ctx, client, plan.Subject, plan.Detail, Outlines(trees))
	if err != nil {
		return nil, err
	}

	// A node created from this plan starts at the skill's current level, so
	// it is open straight away.
	level := 1
	for _, t := range trees {
		if t.Skill.Name == activity.Skill {
			level = rpg.ProgressFor(t.Skill.TotalXP).Level
		}
	}

	event, err := store.RecordXP(ctx, db.XPAward{
		PlanID:       plan.ID,
		Activity:     plan.Subject,
		SkillName:    activity.Skill,
		Category:     activity.Category,
		Difficulty:   activity.Difficulty,
		Minutes:      activity.Minutes,
		XP:           rpg.Award(activity),
		NodeName:     activity.Node,
		ParentNode:   activity.Parent,
		NodeRequired: level,
		NodeXPNeeded: rpg.NodeXPNeeded(level),
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// Outlines converts stored trees into the form the model sees.
func Outlines(trees []db.SkillTree) []rpg.SkillOutline {
	out := make([]rpg.SkillOutline, 0, len(trees))
	for _, t := range trees {
		o := rpg.SkillOutline{
			Name:  t.Skill.Name,
			Level: rpg.ProgressFor(t.Skill.TotalXP).Level,
		}
		for _, n := range t.Nodes {
			o.Nodes = append(o.Nodes, n.Name)
		}
		out = append(out, o)
	}
	return out
}

// ApplyRust marks the frontier node of every active skill that has gone
// rpg.RustAfter without practice. A node that is already rusty is left as
// it is, so this is safe to run on every load.
func ApplyRust(ctx context.Context, store *db.Store, now time.Time) error {
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return err
	}
	for _, t := range trees {
		level := rpg.ProgressFor(t.Skill.TotalXP).Level
		id, ok := rpg.RustTarget(TreeNodes(t.Nodes), level, t.Skill.IsActive, t.Skill.LastPracticed, now)
		if !ok {
			continue
		}
		for _, n := range t.Nodes {
			if n.ID == id && !n.IsRusty {
				if err := store.SetNodeRusty(ctx, id, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// TreeNodes turns stored nodes into the form the tree rules read.
func TreeNodes(nodes []db.Node) []rpg.TreeNode {
	out := make([]rpg.TreeNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, rpg.TreeNode{
			ID:            n.ID,
			ParentID:      n.ParentID,
			ExtraParentID: n.ExtraParentID,
			Required:      n.RequiredLevel,
			Progress:      n.Progress,
			XPNeeded:      n.XPNeeded,
			Rusty:         n.IsRusty,
		})
	}
	return out
}
