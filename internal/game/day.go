package game

import (
	"context"
	"errors"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Queue returns the next curated task of every open or rusty node of an
// active skill, rusty nodes first. Tasks already in today's list are left
// out, so one task is never handed out twice in a day.
func Queue(ctx context.Context, store *db.Store, trees []db.SkillTree, today []db.Challenge) ([]rpg.QueuedTask, error) {
	next, err := store.NextNodeTasks(ctx)
	if err != nil {
		return nil, err
	}
	taken := map[int64]bool{}
	for _, c := range today {
		taken[c.NodeTaskID] = true
	}
	var rusty, open []rpg.QueuedTask
	for _, t := range trees {
		if !t.Skill.IsActive {
			continue
		}
		statuses := rpg.NodeStatuses(TreeNodes(t.Nodes), rpg.ProgressFor(t.Skill.TotalXP).Level)
		for _, n := range t.Nodes {
			task, ok := next[n.ID]
			st := statuses[n.ID]
			if !ok || taken[task.ID] || (st != rpg.Unlocked && st != rpg.Rusty) {
				continue
			}
			q := rpg.QueuedTask{TaskID: task.ID, Skill: t.Skill.Name, Node: n.Name, Rusty: st == rpg.Rusty,
				Title: task.Title, Description: task.Description, Details: task.Details, Difficulty: task.Difficulty, Minutes: task.Minutes}
			if q.Rusty {
				rusty = append(rusty, q)
			} else {
				open = append(open, q)
			}
		}
	}
	return append(rusty, open...), nil
}

// BuildDay fills slots: first from the curated queue, then, for the slots
// it cannot fill, with the local model. g may be nil when every slot is
// filled from the queue. avoid lists titles already set today. It returns
// the drafts in slot order and how many came from the queue.
func BuildDay(ctx context.Context, g rpg.Generator, slots []rpg.DirectiveSlot, queue []rpg.QueuedTask, avoid []string) ([]rpg.ChallengeDraft, int, error) {
	filled := rpg.FillFromQueue(slots, queue)
	var rest []rpg.DirectiveSlot
	curated := 0
	for i, d := range filled {
		if d == nil {
			rest = append(rest, slots[i])
		} else {
			curated++
			avoid = append(avoid, d.Title)
		}
	}

	var generated []rpg.ChallengeDraft
	if len(rest) > 0 {
		if g == nil {
			if curated == 0 {
				return nil, 0, errors.New("planner is not configured")
			}
		} else {
			var err error
			if generated, err = rpg.BuildDirectives(ctx, g, rest, avoid); err != nil && curated == 0 {
				return nil, 0, err
			}
			// With curated directives in hand, a model failure only leaves
			// the set shorter.
		}
	}

	out := make([]rpg.ChallengeDraft, 0, len(slots))
	for _, d := range filled {
		switch {
		case d != nil:
			out = append(out, *d)
		case len(generated) > 0:
			out = append(out, generated[0])
			generated = generated[1:]
		}
	}
	return out, curated, nil
}
