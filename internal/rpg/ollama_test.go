//go:build ollama

package rpg

import (
	"context"
	"testing"
	"time"

	"github.com/navid-fn/lazyplaner/internal/llm"
)

// Run with: go test -tags ollama -run Ollama -v ./internal/rpg/
// Needs a running Ollama server. The model can be changed below.
func TestOllamaClassifyEndToEnd(t *testing.T) {
	client := llm.NewClient("", "qwen2.5-coder:7b")
	existing := []SkillOutline{{Name: "Go"}, {Name: "Running"}}

	cases := []struct {
		subject, detail string
		wantSkill       string
	}{
		{"Learn Go generics", "Write three small programs using type parameters", "Go"},
		{"Morning run", "Run 5 km around the park before breakfast", "Running"},
		{"Buy groceries", "Pick up milk, eggs and bread from the shop", "Errands"},
	}

	for _, c := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		a, err := Classify(ctx, client, c.subject, c.detail, existing)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", c.subject, err)
		}
		t.Logf("%-20s -> skill=%q category=%s difficulty=%d minutes=%d xp=%d",
			c.subject, a.Skill, a.Category, a.Difficulty, a.Minutes, Award(a))
		if a.Skill != c.wantSkill {
			t.Errorf("%s: skill = %q, want %q", c.subject, a.Skill, c.wantSkill)
		}
	}
}

// Run with: go test -tags ollama -run OllamaRoadmap -v ./internal/rpg/
func TestOllamaRoadmapAndChallenges(t *testing.T) {
	client := llm.NewClient("", "qwen2.5-coder:7b")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	category, nodes, err := BuildRoadmap(ctx, client, "Go", 2, "")
	if err != nil {
		t.Fatalf("roadmap: %v", err)
	}
	t.Logf("category chosen by the model: %s", category)
	for i, n := range nodes {
		t.Logf("%2d parent=%2d lv=%2d %-22s %s", i, n.Parent, n.RequiredLevel, n.Name, n.Description)
	}

	var names []string
	for _, n := range nodes {
		names = append(names, n.Name)
	}
	skills := []SkillOutline{
		{Name: "Go", Level: 2, Nodes: names},
		{Name: "Running", Level: 1},
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel2()
	drafts, err := BuildChallenges(ctx2, client, skills, 3, nil)
	if err != nil {
		t.Fatalf("challenges: %v", err)
	}
	for _, d := range drafts {
		t.Logf("challenge: [%s › %s] %s (%d min, +%d xp)\n    %s", d.Skill, d.Node, d.Title, d.Minutes, d.XP, d.Description)
	}
}

// Run with: go test -tags ollama -run OllamaAudit -v ./internal/rpg/
// Checks the auditor on the small models it is written for.
func TestOllamaAudit(t *testing.T) {
	cases := []struct {
		skill, experience string
		minLv, maxLv      int
		rusty             bool
	}{
		{"Python", "I watched one YouTube tutorial last week.", 1, 2, false},
		{"Go", "I have written Go professionally every day for six years.", 4, 5, false},
		{"Running", "I ran three marathons in my twenties but have not run in five years.", 2, 4, true},
		{"Drawing", "I sketch most evenings and finished a few small comics.", 3, 3, false},
	}
	// The 7B model gets every case. The 3B model tends to rate a borderline
	// case one level high, so it must get at least three of four.
	minCorrect := map[string]int{"llama3.2": 3, "qwen2.5-coder:7b": 4}
	for _, model := range []string{"llama3.2", "qwen2.5-coder:7b"} {
		client := llm.NewClient("", model)
		correct := 0
		for _, c := range cases {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			a, err := AuditSkill(ctx, client, c.skill, c.experience)
			cancel()
			if err != nil {
				t.Errorf("[%s] %s: %v", model, c.skill, err)
				continue
			}
			ok := a.StartLevel >= c.minLv && a.StartLevel <= c.maxLv && a.Rusty == c.rusty
			mark := "ok  "
			if !ok {
				mark = "MISS"
			}
			t.Logf("%s [%-16s] %-8s lv=%d rusty=%-5v cat=%-6s want lv %d-%d rusty=%v  %q",
				mark, model, c.skill, a.StartLevel, a.Rusty, a.Category, c.minLv, c.maxLv, c.rusty, a.Evidence)
			if ok {
				correct++
			}
		}
		if correct < minCorrect[model] {
			t.Errorf("[%s] %d/%d correct, want at least %d", model, correct, len(cases), minCorrect[model])
		}
	}
}
