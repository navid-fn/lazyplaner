package rpg

import (
	"fmt"
	"strings"
)

// A node can carry a queue of curated tasks, designed ahead by an external
// agent. When the day's directives are made, each slot first takes the
// next curated task of an open node; the local model only writes the slots
// the queue cannot fill.

// Limits on a node's curated tasks.
const (
	MinNodeTasks  = 2
	MaxNodeTasks  = 4
	maxTaskMinute = 120
)

// CuratedTask is one checked task in a node's queue.
type CuratedTask struct {
	Title       string `json:"title"`             // 2-5 words, at most MaxTitleRunes
	Description string `json:"description"`       // one-sentence definition of done
	Details     string `json:"details,omitempty"` // markdown execution guide
	Difficulty  int    `json:"difficulty"`
	Minutes     int    `json:"minutes"`
}

// cleanTasks checks a node's tasks: none, or MinNodeTasks to MaxNodeTasks
// with short titles (see CheckTitle), a one-sentence description and an
// optional guide. Difficulty and minutes are held to 1-5 and
// 5-maxTaskMinute.
func cleanTasks(node string, tasks []CuratedTask) ([]CuratedTask, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	if len(tasks) < MinNodeTasks || len(tasks) > MaxNodeTasks {
		return nil, fmt.Errorf("node %q has %d tasks; give %d to %d, or none", node, len(tasks), MinNodeTasks, MaxNodeTasks)
	}
	out := make([]CuratedTask, 0, len(tasks))
	for i, t := range tasks {
		title := CleanDirectiveTitle(t.Title)
		desc := strings.TrimSpace(t.Description)
		details := strings.TrimSpace(t.Details)
		if err := CheckTitle(title); err != nil {
			return nil, fmt.Errorf("node %q task %d: %w", node, i+1, err)
		}
		if err := checkTexts(desc, details); err != nil {
			return nil, fmt.Errorf("node %q task %d: %w", node, i+1, err)
		}
		out = append(out, CuratedTask{
			Title:       title,
			Description: desc,
			Details:     details,
			Difficulty:  clamp(t.Difficulty, minDiff, maxDiff),
			Minutes:     clamp(t.Minutes, minMinutes, maxTaskMinute),
		})
	}
	return out, nil
}

// QueuedTask is the next curated task of one open or rusty node.
type QueuedTask struct {
	TaskID      int64
	Skill       string
	Node        string
	Rusty       bool
	Title       string
	Description string
	Details     string
	Difficulty  int
	Minutes     int
}

// FillFromQueue takes curated tasks for the slots it can fill. The result
// lines up with slots; nil marks a slot left for the model. A slot takes a
// task of its own skill on one of its nodes, preferring a rusty node, then
// the task whose length best fits the slot's tier. Each task is used once.
//
// A curated task keeps its own difficulty and minutes, and links back to
// the task so finishing the directive finishes it. In a Recovery slot the
// task is not done but eased into: 10-15 easy minutes on its start, with no
// link, so the task stays next in the queue.
func FillFromQueue(slots []DirectiveSlot, queue []QueuedTask) []*ChallengeDraft {
	used := make([]bool, len(queue))
	out := make([]*ChallengeDraft, len(slots))
	for s, slot := range slots {
		spec := tierSpecs[slot.Tier]
		best := -1
		for i, q := range queue {
			if used[i] || q.Skill != slot.Skill || !contains(slot.Nodes, q.Node) {
				continue
			}
			if best < 0 || better(q, queue[best], spec) {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		used[best] = true
		q := queue[best]
		d := ChallengeDraft{Tier: slot.Tier, Skill: q.Skill, Node: q.Node, Title: q.Title,
			Description: q.Description, Details: q.Details, Difficulty: q.Difficulty, Minutes: q.Minutes, TaskID: q.TaskID}
		if slot.Tier == TierRecovery {
			d.Difficulty, d.Minutes, d.TaskID = spec.minDiff, spec.maxMins, 0
			d.Description = strings.TrimSpace(fmt.Sprintf(
				"Recovery pace: %d easy minutes on the start of this task, no more. %s", spec.maxMins, q.Description))
		}
		d.XP = Award(Activity{Difficulty: d.Difficulty, Minutes: d.Minutes})
		out[s] = &d
	}
	return out
}

// better reports whether queued task a suits a slot of spec more than b: a
// rusty node first, then the length closest to the tier's range.
func better(a, b QueuedTask, spec tierSpec) bool {
	if a.Rusty != b.Rusty {
		return a.Rusty
	}
	return minutesOff(a.Minutes, spec) < minutesOff(b.Minutes, spec)
}

// minutesOff is how far minutes falls outside a tier's range.
func minutesOff(minutes int, spec tierSpec) int {
	switch {
	case minutes < spec.minMins:
		return spec.minMins - minutes
	case minutes > spec.maxMins:
		return minutes - spec.maxMins
	}
	return 0
}
