package rpg

import (
	"strings"
	"testing"
)

func TestCheckTitleWantsAShortName(t *testing.T) {
	for _, ok := range []string{"Ghosted Line Drills", "Vector Dunder Methods", "Mutex vs Atomic Bench"} {
		if err := CheckTitle(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Drills", "Fill a page with ghosted straight lines between dots",
		"One Two Three Four Five Six", "Supercalifragilistic Expialidocious"} {
		if err := CheckTitle(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestShortenTitleCutsBetweenWords(t *testing.T) {
	got := ShortenTitle("Fill a page with ghosted straight lines between dots")
	if got != "Fill a page with ghosted" {
		t.Errorf("got %q", got)
	}
	if got := ShortenTitle("Write a buffered channel worker pool today"); len([]rune(got)) > MaxTitleRunes ||
		len(strings.Fields(got)) > MaxTitleWords {
		t.Errorf("got %q, over the limits", got)
	}
	if got := ShortenTitle(strings.Repeat("x", 50)); len([]rune(got)) != MaxTitleRunes {
		t.Errorf("a single long word is cut to %d runes, got %q", MaxTitleRunes, got)
	}
}

func TestCuratedTasksCarryTheirGuide(t *testing.T) {
	tasks, err := cleanTasks("Node", []CuratedTask{
		{Title: "Line Drills", Description: "One page of lines.", Details: "- Ghost each line\n- Check straightness"},
		{Title: "Ellipse Drills", Description: "Two pages."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].Details != "- Ghost each line\n- Check straightness" {
		t.Errorf("details = %q", tasks[0].Details)
	}
	if _, err := cleanTasks("Node", []CuratedTask{
		{Title: "Line Drills", Description: strings.Repeat("a", MaxSummaryRunes+1)},
		{Title: "Ellipse Drills"},
	}); err == nil || !strings.Contains(err.Error(), "one sentence") {
		t.Errorf("a long description: err = %v", err)
	}
}

func TestBuildDirectivesKeepsTitlesShortAndTheGuide(t *testing.T) {
	g := &fakeGen{reply: `{"directives":[{"slot":1,"node":"Basics","title":"Write a small program that sums all the numbers in a list",
		"description":"Done when it prints 55.","details":"- Make main.go\n- Use a for range loop\n- go run prints 55","difficulty":1,"minutes":10}]}`}
	drafts, err := BuildDirectives(t.Context(), g, []DirectiveSlot{{Tier: TierRecon, Skill: "Go", Nodes: []string{"Basics"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := drafts[0]
	if len([]rune(d.Title)) > MaxTitleRunes || len(strings.Fields(d.Title)) > MaxTitleWords {
		t.Errorf("title %q is over the limits", d.Title)
	}
	if !strings.Contains(d.Details, "go run prints 55") {
		t.Errorf("details = %q", d.Details)
	}
	if !strings.Contains(string(g.gotSchema), `"details"`) {
		t.Error("the schema should ask for details")
	}
}
