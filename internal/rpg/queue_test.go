package rpg

import (
	"strings"
	"testing"
)

func TestFillFromQueue(t *testing.T) {
	slots := []DirectiveSlot{
		{Tier: TierRecon, Skill: "Java", Nodes: []string{"JVM", "Collections"}},
		{Tier: TierCoreOp, Skill: "Java", Nodes: []string{"JVM", "Collections"}},
		{Tier: TierDeepPush, Skill: "Go", Nodes: []string{"Channels"}},
	}
	queue := []QueuedTask{
		{TaskID: 1, Skill: "Java", Node: "JVM", Title: "Profile heap", Difficulty: 3, Minutes: 45},
		{TaskID: 2, Skill: "Java", Node: "Collections", Title: "Map a CSV", Difficulty: 2, Minutes: 15},
		{TaskID: 3, Skill: "Java", Node: "Streams", Title: "Not open", Minutes: 15},
	}
	got := FillFromQueue(slots, queue)
	if got[0] == nil || got[0].TaskID != 2 {
		t.Errorf("recon should take the 15-minute task, got %+v", got[0])
	}
	if got[1] == nil || got[1].TaskID != 1 || got[1].Minutes != 45 || got[1].Tier != TierCoreOp {
		t.Errorf("core op should take the 45-minute task as it is, got %+v", got[1])
	}
	if got[1].XP != Award(Activity{Difficulty: 3, Minutes: 45}) {
		t.Errorf("XP = %d, want the formula's", got[1].XP)
	}
	if got[2] != nil {
		t.Errorf("Go has no queued task, so its slot is the model's: %+v", got[2])
	}

	// A rusty node wins over a better fit.
	queue[0].Rusty = true
	if got := FillFromQueue(slots[:1], queue); got[0].TaskID != 1 {
		t.Errorf("rusty first: got task %d", got[0].TaskID)
	}
}

func TestFillFromQueueEasesARecoveryDay(t *testing.T) {
	slots := RecoverySlots([]SkillOutline{{Name: "Java", Nodes: []string{"JVM"}}})
	got := FillFromQueue(slots, []QueuedTask{{TaskID: 7, Skill: "Java", Node: "JVM", Title: "Profile heap", Difficulty: 4, Minutes: 60}})
	d := got[0]
	if d == nil || d.TaskID != 0 || d.Minutes != 15 || d.Difficulty != 1 || !strings.HasPrefix(d.Description, "Recovery pace") {
		t.Errorf("a sick day eases into the task without using it up: %+v", d)
	}
}

func TestCuratedTasksOnExternalNodes(t *testing.T) {
	nodes := agentNodes()
	nodes[0].Tasks = []CuratedTask{{Title: "Read the JVM spec intro", Difficulty: 9, Minutes: 500}, {Title: "Inspect a heap dump", Minutes: 30, Difficulty: 3}}
	shaped, err := ShapeExternalTree(nodes, 1)
	if err != nil {
		t.Fatal(err)
	}
	if task := shaped[0].Tasks[0]; task.Difficulty != 5 || task.Minutes != maxTaskMinute {
		t.Errorf("task limits not applied: %+v", task)
	}

	nodes[1].Tasks = []CuratedTask{{Title: "Only one"}}
	if _, err := ShapeExternalTree(nodes, 1); err == nil {
		t.Error("a node needs 2-4 tasks or none")
	}
	nodes[1].Tasks = []CuratedTask{{Title: "Practice"}, {Title: "  "}}
	if _, err := ShapeExternalTree(nodes, 1); err == nil {
		t.Error("a task needs a title")
	}
}
