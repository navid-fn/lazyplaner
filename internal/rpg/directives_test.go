package rpg

import (
	"context"
	"strings"
	"testing"
)

func TestDailySlotsDealsTiersOverSkills(t *testing.T) {
	three := []SkillOutline{{Name: "Go"}, {Name: "Java"}, {Name: "Drawing"}}
	slots := DailySlots(three)
	if len(slots) != 3 {
		t.Fatalf("got %d slots, want 3", len(slots))
	}
	want := []struct {
		tier  Tier
		skill string
	}{{TierRecon, "Go"}, {TierCoreOp, "Java"}, {TierDeepPush, "Drawing"}}
	for i, w := range want {
		if slots[i].Tier != w.tier || slots[i].Skill != w.skill {
			t.Errorf("slot %d = %v/%s, want %v/%s", i, slots[i].Tier, slots[i].Skill, w.tier, w.skill)
		}
	}

	// One skill still gets the full three-tier set.
	if got := DailySlots([]SkillOutline{{Name: "Go"}}); len(got) != 3 || got[2].Skill != "Go" {
		t.Errorf("one skill: %+v", got)
	}
	// Never more than four.
	many := append(three, SkillOutline{Name: "Chess"}, SkillOutline{Name: "Cooking"})
	if got := DailySlots(many); len(got) != MaxDailyDirectives {
		t.Errorf("five skills gave %d slots, want %d", len(got), MaxDailyDirectives)
	}
	if DailySlots(nil) != nil {
		t.Error("no skills, no slots")
	}
}

func TestBuildDirectivesHoldsAnswersToTheirSlots(t *testing.T) {
	g := &fakeGen{reply: `{"directives":[
		{"slot":3,"node":"web development","title":"Java Web Development Challenge","description":"x","difficulty":1,"minutes":5},
		{"slot":1,"node":"channels","title":"Write a buffered channel worker pool","description":"Ten jobs, three workers.","difficulty":5,"minutes":90},
		{"slot":2,"node":"","title":"  ","description":"","difficulty":3,"minutes":30}
	]}`}
	slots := []DirectiveSlot{
		{Tier: TierRecon, Skill: "Go", Level: 4, Nodes: []string{"Channels", "Generics"}},
		{Tier: TierCoreOp, Skill: "Java", Level: 1, Nodes: []string{"JVM Memory"}},
		{Tier: TierDeepPush, Skill: "Java", Level: 1, Nodes: []string{"JVM Memory", "Streams"}},
	}
	out, err := BuildDirectives(context.Background(), g, slots, []string{"Old one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d directives, want 2 (empty title dropped): %+v", len(out), out)
	}

	recon := out[0]
	if recon.Tier != TierRecon || recon.Skill != "Go" || recon.Node != "Channels" {
		t.Errorf("recon = %+v", recon)
	}
	if recon.Difficulty != 2 || recon.Minutes != 15 {
		t.Errorf("recon should be held to difficulty 1-2 and 15 min, got %d/%d", recon.Difficulty, recon.Minutes)
	}

	deep := out[1]
	if deep.Node != "" {
		t.Errorf("a node outside the slot's open nodes (a locked one) must be cleared, got %q", deep.Node)
	}
	if deep.Title != "Java Web Development" {
		t.Errorf("title should lose its trailing Challenge, got %q", deep.Title)
	}
	if deep.Difficulty != 4 || deep.Minutes != 60 {
		t.Errorf("deep push should be held to difficulty 4-5 and 60 min, got %d/%d", deep.Difficulty, deep.Minutes)
	}
	if deep.XP != Award(Activity{Difficulty: 4, Minutes: 60}) {
		t.Errorf("XP %d does not match Award", deep.XP)
	}

	for _, want := range []string{"Slot 1: Recon", "Skill: Go (level 4)", "Channels, Generics",
		"Slot 3: Deep Push", "Do not repeat these directives from earlier today: Old one"} {
		if !strings.Contains(g.gotPrompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, g.gotPrompt)
		}
	}
	if strings.Contains(g.gotPrompt, "Web Development") {
		t.Error("the prompt must only offer the slot's open nodes")
	}
}

func TestBuildDirectivesFillsUnnumberedAnswersInOrder(t *testing.T) {
	g := &fakeGen{reply: `{"directives":[{"slot":9,"node":"","title":"Sketch 4 angry/happy eye studies","description":"","difficulty":3,"minutes":30}]}`}
	out, err := BuildDirectives(context.Background(), g,
		[]DirectiveSlot{{Tier: TierOvertime, Skill: "Drawing Face", Nodes: []string{"Eyes"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Skill != "Drawing Face" || out[0].Tier != TierOvertime || out[0].Node != "Eyes" {
		t.Errorf("got %+v", out[0])
	}
	if _, err := BuildDirectives(context.Background(), g, nil, nil); err == nil {
		t.Error("expected an error with no slots")
	}
}

func TestDirectiveSystemForbidsFillerTitles(t *testing.T) {
	for _, w := range []string{`"Challenge"`, `"Task"`, `"Study"`, "2 to 5 words", "32 characters", "details"} {
		if !strings.Contains(directiveSystem, w) {
			t.Errorf("system prompt should mention %s", w)
		}
	}
}

func TestCleanDirectiveTitle(t *testing.T) {
	cases := map[string]string{
		"Core Rotation Challenge":            "Core Rotation",
		"  Write a worker pool.  ":           "Write a worker pool",
		"Review closures task":               "Review closures",
		"Sketch 4 angry/happy eye studies":   "Sketch 4 angry/happy eye studies",
		"Challenge":                          "",
		"Anatomy Study Task":                 "Anatomy",
		"Cook pan-seared chicken with sauce": "Cook pan-seared chicken with sauce",
	}
	for in, want := range cases {
		if got := CleanDirectiveTitle(in); got != want {
			t.Errorf("CleanDirectiveTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTierLabels(t *testing.T) {
	if got := TierRecon.Label(); got != "⚡ Recon · 15m" {
		t.Errorf("recon label = %q", got)
	}
	if TierNone.Label() != "" {
		t.Error("an untiered directive has no label")
	}
}
