package rpg

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCategorizeSkillUsesTheModel(t *testing.T) {
	g := &fakeGen{reply: `{"category":"Life"}`}
	cat, err := CategorizeSkill(context.Background(), g, "Bonsai")
	if err != nil || cat != "Life" {
		t.Errorf("got %q, %v; want the model's Life", cat, err)
	}
	if !strings.Contains(g.gotPrompt, "Bonsai") {
		t.Errorf("prompt should name the skill: %q", g.gotPrompt)
	}
}

func TestCategorizeSkillFallsBackToKeywords(t *testing.T) {
	down := &fakeGen{err: errors.New("ollama is down")}
	if cat, err := CategorizeSkill(context.Background(), down, "Cooking"); err != nil || cat != "Craft" {
		t.Errorf("model down: got %q, %v; want the keyword guess Craft", cat, err)
	}
	odd := &fakeGen{reply: `{"category":"Sports"}`}
	if cat, _ := CategorizeSkill(context.Background(), odd, "Trail Running"); cat != "Body" {
		t.Errorf("unknown answer: got %q, want Body", cat)
	}
	if _, err := CategorizeSkill(context.Background(), down, "Zorbing"); err == nil {
		t.Error("with no model and no keyword there is nothing to go on")
	}
}
