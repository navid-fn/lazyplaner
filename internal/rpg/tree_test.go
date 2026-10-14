package rpg

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNodeStatuses(t *testing.T) {
	nodes := []TreeNode{
		{ID: 1, ParentID: 0, Required: 1, Progress: 0, XPNeeded: 40},  // basics, unlocked at level 1
		{ID: 2, ParentID: 1, Required: 2, Progress: 0, XPNeeded: 80},  // needs level 2
		{ID: 3, ParentID: 2, Required: 1, Progress: 80, XPNeeded: 80}, // done, but parent is locked below
		{ID: 4, ParentID: 0, Required: 1, Progress: 40, XPNeeded: 40}, // done directly off the skill
		{ID: 5, ParentID: 99, Required: 1, Progress: 0, XPNeeded: 40}, // parent missing, stays locked
		{ID: 6, ParentID: 1, Required: 1, Progress: 10, XPNeeded: 40}, // waits for node 1 to be done
	}

	st := NodeStatuses(nodes, 1)
	want := map[int64]Status{1: Unlocked, 2: Locked, 3: Locked, 4: Done, 5: Locked, 6: Locked}
	for id, w := range want {
		if st[id] != w {
			t.Errorf("level 1: node %d = %v, want %v", id, st[id], w)
		}
	}

	// The level alone does not open a child while its parent is unfinished.
	st = NodeStatuses(nodes, 2)
	if st[2] != Locked {
		t.Errorf("level 2, parent open: node 2 = %v, want Locked", st[2])
	}

	nodes[0].Progress = 40 // finish node 1
	st = NodeStatuses(nodes, 2)
	if st[6] != Unlocked {
		t.Errorf("level 2: node 6 = %v, want Unlocked", st[6])
	}
	if st[2] != Unlocked {
		t.Errorf("level 2: node 2 = %v, want Unlocked", st[2])
	}
	// Node 3 has its XP, but its parent (node 2) is still open.
	if st[3] != Locked {
		t.Errorf("level 2: node 3 = %v, want Locked", st[3])
	}
}

func TestNodeXPNeeded(t *testing.T) {
	if got := NodeXPNeeded(3); got != 120 {
		t.Errorf("NodeXPNeeded(3) = %d, want 120", got)
	}
	if got := NodeXPNeeded(0); got != 40 {
		t.Errorf("NodeXPNeeded(0) = %d, want 40 (minimum level 1)", got)
	}
}

func TestCleanRoadmapDropsBadNodes(t *testing.T) {
	reply := roadmapReply{Nodes: []struct {
		Parent        int    `json:"parent"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		RequiredLevel int    `json:"required_level"`
	}{
		{Parent: -1, Name: "Basics", RequiredLevel: 1},
		{Parent: 0, Name: "  ", RequiredLevel: 2},        // nameless: dropped
		{Parent: 0, Name: "basics", RequiredLevel: 2},    // duplicate: dropped
		{Parent: 1, Name: "Loops", RequiredLevel: 2},     // parent 1 was dropped, so attaches to skill
		{Parent: 0, Name: "Closures", RequiredLevel: 42}, // level clamped to 10
	}}

	out, err := cleanRoadmap(reply, "Go")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d nodes, want 3: %+v", len(out), out)
	}
	if out[0].Name != "Basics" || out[0].Parent != -1 {
		t.Errorf("node 0 = %+v", out[0])
	}
	if out[1].Name != "Loops" || out[1].Parent != -1 {
		t.Errorf("node 1 should attach to the skill, got %+v", out[1])
	}
	if out[2].Name != "Closures" || out[2].RequiredLevel != 10 {
		t.Errorf("node 2 = %+v", out[2])
	}
}

func TestCleanRoadmapRequiresNodes(t *testing.T) {
	if _, err := cleanRoadmap(roadmapReply{}, "Go"); err == nil {
		t.Error("expected error for an empty roadmap")
	}
}

func TestBuildRoadmapUsesModelReply(t *testing.T) {
	g := &fakeGen{reply: `{"category":"Craft","nodes":[
		{"parent":-1,"name":"Syntax","description":"Variables and functions","required_level":1},
		{"parent":0,"name":"Interfaces","description":"Implicit satisfaction","required_level":4}
	]}`}
	category, nodes, err := BuildRoadmap(context.Background(), g, "Go", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if category != "Craft" {
		t.Errorf("category = %q, want the one the model chose", category)
	}
	if len(nodes) != 2 || nodes[1].Parent != 0 || nodes[1].RequiredLevel != 4 {
		t.Errorf("unexpected roadmap %+v", nodes)
	}
	if !strings.Contains(g.gotPrompt, "already has level 2") {
		t.Errorf("prompt should include the starting level: %q", g.gotPrompt)
	}
}

func TestBuildChallengesFiltersAndNormalizes(t *testing.T) {
	g := &fakeGen{reply: `{"challenges":[
		{"skill":"go","node":"syntax","title":"Write a function that returns two values","difficulty":2,"minutes":20},
		{"skill":"Chess","node":"","title":"Play a game","difficulty":1,"minutes":15},
		{"skill":"Running","node":"Intervals","title":"  ","difficulty":1,"minutes":10},
		{"skill":"Running","node":"Warm-up","title":"Jog 2 km","difficulty":2,"minutes":20}
	]}`}
	skills := []SkillOutline{
		{Name: "Go", Nodes: []string{"Syntax"}},
		{Name: "Running", Nodes: []string{"Warm-up"}},
	}

	out, err := BuildChallenges(context.Background(), g, skills, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d challenges, want 2 (unknown skill and empty title dropped): %+v", len(out), out)
	}
	if out[0].Skill != "Go" || out[0].Node != "Syntax" {
		t.Errorf("challenge 0 = %+v, want Go/Syntax", out[0])
	}
	if out[0].XP != Award(Activity{Difficulty: 2, Minutes: 20}) {
		t.Errorf("challenge 0 XP = %d, does not match Award", out[0].XP)
	}
	if out[1].Skill != "Running" || out[1].Node != "Warm-up" {
		t.Errorf("challenge 1 = %+v", out[1])
	}
}

func TestBuildChallengesClearsUnknownNode(t *testing.T) {
	g := &fakeGen{reply: `{"challenges":[{"skill":"Go","node":"Nonexistent","title":"Do a thing","difficulty":1,"minutes":10}]}`}
	out, err := BuildChallenges(context.Background(), g, []SkillOutline{{Name: "Go", Nodes: []string{"Syntax"}}}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Node != "" {
		t.Errorf("unknown node should be cleared, got %q", out[0].Node)
	}
}

func TestBuildChallengesNeedsUsableOutput(t *testing.T) {
	g := &fakeGen{reply: `{"challenges":[{"skill":"Chess","node":"","title":"x","difficulty":1,"minutes":10}]}`}
	// With two skills an unknown name cannot be placed, so it is dropped.
	if _, err := BuildChallenges(context.Background(), g, []SkillOutline{{Name: "Go"}, {Name: "Running"}}, 1, nil); err == nil {
		t.Error("expected error when no challenge matches a known skill")
	}
	// With one skill the same unknown name is attached to that skill.
	out, err := BuildChallenges(context.Background(), g, []SkillOutline{{Name: "Go"}}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Skill != "Go" {
		t.Errorf("single-skill fallback gave %q, want Go", out[0].Skill)
	}
	if _, err := BuildChallenges(context.Background(), g, nil, 1, nil); err == nil {
		t.Error("expected error with no skills")
	}
}

func TestClassifyPlacesPlanOnNode(t *testing.T) {
	skills := []SkillOutline{{Name: "Go", Nodes: []string{"Syntax", "Interfaces"}}}

	g := &fakeGen{reply: `{"skill":"go","category":"Craft","difficulty":2,"minutes":60,"node":"interfaces","parent":"syntax"}`}
	a, err := Classify(context.Background(), g, "Read about interfaces", "", skills)
	if err != nil {
		t.Fatal(err)
	}
	if a.Node != "Interfaces" || a.Parent != "Syntax" {
		t.Errorf("node/parent = %q/%q, want Interfaces/Syntax", a.Node, a.Parent)
	}

	// A parent without a node is meaningless and is dropped.
	g = &fakeGen{reply: `{"skill":"Go","category":"Craft","difficulty":2,"minutes":60,"node":"","parent":"Syntax"}`}
	a, err = Classify(context.Background(), g, "Misc", "", skills)
	if err != nil {
		t.Fatal(err)
	}
	if a.Node != "" || a.Parent != "" {
		t.Errorf("expected no node, got %q/%q", a.Node, a.Parent)
	}
}

func TestClassifySchemaHasNodeFields(t *testing.T) {
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(classifySchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(schema.Required, ","), "node") {
		t.Error("classify schema must require node")
	}
}

func TestBuildChallengesAvoidsEarlierTitles(t *testing.T) {
	g := &fakeGen{reply: `{"challenges":[{"skill":"Go","node":"","title":"New one","difficulty":1,"minutes":10}]}`}
	_, err := BuildChallenges(context.Background(), g, []SkillOutline{{Name: "Go"}}, 1, []string{"Write a loop"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.gotPrompt, "Do not repeat these challenges from earlier today: Write a loop") {
		t.Errorf("prompt should list earlier titles: %q", g.gotPrompt)
	}
}

func TestBuildChallengesKeepsDescription(t *testing.T) {
	g := &fakeGen{reply: `{"challenges":[{"skill":"Go","node":"","title":"Sum","description":"Add 1 to 10 in a loop and print the result.","difficulty":1,"minutes":10}]}`}
	out, err := BuildChallenges(context.Background(), g, []SkillOutline{{Name: "Go"}}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Description != "Add 1 to 10 in a loop and print the result." {
		t.Errorf("description = %q", out[0].Description)
	}
	if !strings.Contains(string(g.gotSchema), `"description"`) {
		t.Error("the schema should ask for a description")
	}
}
