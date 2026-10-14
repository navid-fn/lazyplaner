package rpg

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTotalXPForLevel(t *testing.T) {
	cases := map[int]int{1: 0, 2: 100, 3: 283, 5: 800, 10: 2700}
	for level, want := range cases {
		if got := TotalXPForLevel(level); got != want {
			t.Errorf("TotalXPForLevel(%d) = %d, want %d", level, got, want)
		}
	}
}

func TestProgressFor(t *testing.T) {
	p := ProgressFor(0)
	if p.Level != 1 || p.Into != 0 || p.NextXP != 100 {
		t.Fatalf("ProgressFor(0) = %+v", p)
	}

	p = ProgressFor(150)
	if p.Level != 2 || p.Into != 50 || p.Span != 183 {
		t.Fatalf("ProgressFor(150) = %+v", p)
	}

	// Exactly on a threshold starts the new level.
	if p := ProgressFor(100); p.Level != 2 || p.Into != 0 {
		t.Fatalf("ProgressFor(100) = %+v", p)
	}

	if p := ProgressFor(-5); p.Level != 1 {
		t.Fatalf("negative XP should clamp to level 1, got %+v", p)
	}
}

func TestAward(t *testing.T) {
	cases := []struct {
		name string
		a    Activity
		want int
	}{
		{"short easy", Activity{Minutes: 20, Difficulty: 1}, 4},
		{"run", Activity{Minutes: 35, Difficulty: 3}, 11},
		{"long hard", Activity{Minutes: 120, Difficulty: 3}, 36},
		{"capped minutes", Activity{Minutes: 600, Difficulty: 5}, 120},
		{"zero minutes is raised to minimum", Activity{Minutes: 0, Difficulty: 1}, 1},
		{"bad difficulty is clamped", Activity{Minutes: 60, Difficulty: 99}, 30},
	}
	for _, c := range cases {
		if got := Award(c.a); got != c.want {
			t.Errorf("%s: Award = %d, want %d", c.name, got, c.want)
		}
	}
}

// fakeGen returns a canned reply and records the schema it was given.
type fakeGen struct {
	reply     string
	err       error
	gotPrompt string
	gotSchema json.RawMessage
}

func (f *fakeGen) GenerateJSON(_ context.Context, _ string, prompt string, schema json.RawMessage) (string, error) {
	f.gotPrompt = prompt
	f.gotSchema = schema
	return f.reply, f.err
}

func TestClassifyReusesExistingSkillIgnoringCase(t *testing.T) {
	g := &fakeGen{reply: `{"skill":"go","category":"Craft","difficulty":3,"minutes":120}`}
	a, err := Classify(context.Background(), g, "Build a CLI", "in Go", []SkillOutline{{Name: "Go"}, {Name: "Running"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Skill != "Go" {
		t.Errorf("Skill = %q, want existing spelling %q", a.Skill, "Go")
	}
	if a.Category != "Craft" || a.Difficulty != 3 || a.Minutes != 120 {
		t.Errorf("unexpected activity %+v", a)
	}
}

func TestClassifyPromptListsExistingSkills(t *testing.T) {
	g := &fakeGen{reply: `{"skill":"Go","category":"Craft","difficulty":2,"minutes":30}`}
	if _, err := Classify(context.Background(), g, "Sub", "Det", []SkillOutline{{Name: "Go", Level: 2}, {Name: "Running"}}); err != nil {
		t.Fatal(err)
	}
	if want := "- Go (level 2): (no nodes)"; !strings.Contains(g.gotPrompt, want) {
		t.Errorf("prompt %q does not contain %q", g.gotPrompt, want)
	}
	if len(g.gotSchema) == 0 {
		t.Error("schema was not sent to the generator")
	}
}

func TestClassifyClampsRanges(t *testing.T) {
	g := &fakeGen{reply: `{"skill":"Chess","category":"Mind","difficulty":9,"minutes":0}`}
	a, err := Classify(context.Background(), g, "s", "d", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Difficulty != 5 || a.Minutes != 5 {
		t.Errorf("expected clamped values, got %+v", a)
	}
}

func TestClassifyRejectsBadReplies(t *testing.T) {
	cases := map[string]string{
		"empty skill":      `{"skill":"  ","category":"Mind","difficulty":1,"minutes":10}`,
		"unknown category": `{"skill":"Go","category":"Art","difficulty":1,"minutes":10}`,
		"not json":         `Sure! Here is your JSON`,
	}
	for name, reply := range cases {
		g := &fakeGen{reply: reply}
		if _, err := Classify(context.Background(), g, "s", "d", nil); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestClassifyPropagatesGeneratorError(t *testing.T) {
	g := &fakeGen{err: errors.New("ollama down")}
	if _, err := Classify(context.Background(), g, "s", "d", nil); err == nil {
		t.Fatal("expected error from generator")
	}
}

func TestRankTitle(t *testing.T) {
	cases := map[int]string{1: "Novice", 4: "Novice", 5: "Apprentice", 20: "Expert", 99: "Grandmaster"}
	for level, want := range cases {
		if got := RankTitle(level); got != want {
			t.Errorf("RankTitle(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestNextRank(t *testing.T) {
	if title, from, ok := NextRank(9); !ok || title != "Journeyman" || from != 10 {
		t.Errorf("NextRank(9) = %q, %d, %v; want Journeyman at 10", title, from, ok)
	}
	if title, _, ok := NextRank(1); !ok || title != "Apprentice" {
		t.Errorf("NextRank(1) = %q, %v; want Apprentice", title, ok)
	}
	if _, _, ok := NextRank(50); ok {
		t.Error("there is no rank above Grandmaster")
	}
}
