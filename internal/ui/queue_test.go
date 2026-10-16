package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/game"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

func TestDailySetComesFromTheCuratedQueueWithoutAModel(t *testing.T) {
	m := newFlowModel(t)
	ctx := context.Background()
	b := game.Blueprint{SkillName: "Java", StartLevel: 1, Category: "Craft"}
	b.MasterPlan.Subject, b.MasterPlan.MarkdownBody = "Java Ramp-Up", "# Plan"
	for i := range 8 {
		n := rpg.ExternalNode{Name: fmt.Sprintf("Java Step %d", i+1)}
		for j, mins := range []int{15, 40, 60} {
			n.Tasks = append(n.Tasks, rpg.CuratedTask{Title: fmt.Sprintf("Step %d Part %d", i+1, j+1), Difficulty: 2, Minutes: mins})
		}
		b.Nodes = append(b.Nodes, n)
	}
	if _, _, err := game.ApplyBlueprint(ctx, m.store, b, false); err != nil {
		t.Fatal(err)
	}

	// m.client is nil: the queue fills what it can and says what is missing.
	note, err := runDailyJob(nil, m.store, dailyGenerate, db.Challenge{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "📜 2 from your curated queue") || !strings.Contains(note, "1 slot left empty") {
		t.Errorf("note = %q", note)
	}
	list, _ := m.store.ListChallenges(ctx, db.Today())
	if len(list) != 2 || list[0].NodeTaskID == 0 {
		t.Fatalf("today = %+v", list)
	}

	// The tree's g pulls the node's next task too, skipping the one taken.
	note, err = runDailyJob(nil, m.store, dailyNode, db.Challenge{SkillName: "Java", NodeName: list[0].NodeName})
	if err == nil {
		t.Errorf("the node already has a directive today, got %q", note)
	}
	if _, err := runDailyJob(nil, m.store, dailyReroll, list[0]); err == nil {
		t.Error("a reroll needs the model")
	}
}
