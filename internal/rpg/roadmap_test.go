package rpg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// eightNodes is a model reply with RoadmapSize nodes named N0..N7.
func eightNodes(category string) string {
	var nodes []string
	for i := range RoadmapSize {
		nodes = append(nodes, fmt.Sprintf(`{"name":"N%d","description":"milestone %d"}`, i, i))
	}
	return fmt.Sprintf(`{"category":%q,"nodes":[%s]}`, category, strings.Join(nodes, ","))
}

func TestBuildRoadmapWiresTheDiamondInCode(t *testing.T) {
	g := &fakeGen{reply: eightNodes("Craft")}
	category, nodes, err := BuildRoadmap(context.Background(), g, "Python", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if category != "Craft" || len(nodes) != RoadmapSize {
		t.Fatalf("category %q, %d nodes", category, len(nodes))
	}
	// 2 roots, 3 mid branches, 2 advanced, 1 capstone.
	wantParents := []int{-1, -1, 0, 0, 1, 3, 4, 5}
	for i, n := range nodes {
		if n.Parent != wantParents[i] || n.Name != fmt.Sprintf("N%d", i) {
			t.Errorf("node %d = %+v, want parent %d", i, n, wantParents[i])
		}
	}
}

func TestRoadmapAsksOnlyForNamesAndDescriptions(t *testing.T) {
	var schema struct {
		Properties struct {
			Nodes struct {
				MinItems int `json:"minItems"`
				MaxItems int `json:"maxItems"`
				Items    struct {
					Properties map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"nodes"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(roadmapSchemaJSON), &schema); err != nil {
		t.Fatal(err)
	}
	n := schema.Properties.Nodes
	if n.MinItems != RoadmapSize || n.MaxItems != RoadmapSize {
		t.Errorf("schema should ask for exactly %d nodes, got %d-%d", RoadmapSize, n.MinItems, n.MaxItems)
	}
	if _, ok := n.Items.Properties["parent"]; ok {
		t.Error("the model must not be asked for parents")
	}
	if _, ok := n.Items.Properties["required_level"]; ok {
		t.Error("the model must not be asked for levels")
	}
}

func TestRoadmapPromptStaysOnTheSkill(t *testing.T) {
	g := &fakeGen{reply: eightNodes("Craft")}
	if _, _, err := BuildRoadmap(context.Background(), g, "Python", 3, "build a web scraper"); err != nil {
		t.Fatal(err)
	}
	all := roadmapSystem + g.gotPrompt
	for _, leak := range []string{"JVM", "Maven", "Gradle", "slices", "Slices", "Streams", "Java", "Cooking", "Gesture", "Pan sauce"} {
		if strings.Contains(all, leak) {
			t.Errorf("prompt mentions %q; no other domain may reach the model", leak)
		}
	}
	for _, want := range []string{"Skill: Python", "Starting level: 3", `Target goal: "build a web scraper"`,
		"strictly about Python", "exactly 8 sub-skills", "at least 6 of the 8 nodes MUST directly train",
		"Do NOT output generic filler", `Build the nodes toward the goal: "build a web scraper"`} {
		if !strings.Contains(g.gotPrompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, g.gotPrompt)
		}
	}
	if !strings.Contains(roadmapSystem, "Never combine it with or\nmention other hobbies, skills or languages") {
		t.Error("the system prompt should forbid mixing in other skills")
	}

	if _, _, err := BuildRoadmap(context.Background(), g, "Python", 1, "  "); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(g.gotPrompt, "goal") {
		t.Error("an empty goal should leave the prompt alone")
	}
}

func TestShapeTreeLevels(t *testing.T) {
	cases := map[int][]int{
		1: {1, 1, 2, 2, 2, 4, 4, 6},
		2: {1, 1, 1, 2, 2, 3, 3, 5},
		4: {2, 2, 3, 4, 4, 5, 5, 7},
		5: {3, 3, 4, 5, 5, 6, 6, 8},
	}
	for start, want := range cases {
		nodes := shapeTree(make([]RoadNode, RoadmapSize), start)
		for i, n := range nodes {
			if n.RequiredLevel != want[i] {
				t.Errorf("start Lv %d: node %d needs Lv %d, want %d", start, i, n.RequiredLevel, want[i])
			}
		}
	}
}

// At any start level the new tree has open work, the capstone is far enough
// out to be fogged, and past Lv 1 the next column is locked but visible.
func TestNewTreeIsNeverSoftLocked(t *testing.T) {
	for start := 1; start <= 5; start++ {
		nodes := shapeTree(make([]RoadNode, RoadmapSize), start)
		tree := make([]TreeNode, len(nodes))
		for i, n := range nodes {
			cost := NodeXPNeeded(n.RequiredLevel)
			var parent int64
			if n.Parent >= 0 {
				parent = int64(n.Parent + 1)
			}
			tree[i] = TreeNode{ID: int64(i + 1), ParentID: parent, Required: n.RequiredLevel,
				XPNeeded: cost, Progress: StartProgress(n.RequiredLevel, start, cost)}
		}
		st := NodeStatuses(tree, start)

		open := 0
		for _, s := range st {
			if s == Unlocked {
				open++
			}
		}
		if open < 2 {
			t.Errorf("start Lv %d: %d open nodes, want at least 2: %v", start, open, st)
		}
		if start > 1 {
			for i, want := range []Status{Done, Done, Done, Unlocked, Unlocked, Locked, Locked, Locked} {
				if st[int64(i+1)] != want {
					t.Errorf("start Lv %d: node %d = %v, want %v", start, i, st[int64(i+1)], want)
				}
			}
		}
		if capstone := nodes[7].RequiredLevel; capstone <= start+2 {
			t.Errorf("start Lv %d: capstone at Lv %d should be past the fog line", start, capstone)
		}
		// At Lv 1 column 3 needs Lv 4, so it opens out of the fog at Lv 2.
		if adv := nodes[5].RequiredLevel; start > 1 && adv > start+2 {
			t.Errorf("start Lv %d: column 3 at Lv %d should be visible", start, adv)
		}
	}
}

func TestCleanRoadmapKeepsAShortReplyValid(t *testing.T) {
	g := &fakeGen{reply: `{"category":"Craft","nodes":[
		{"name":"Syntax","description":""},
		{"name":"  ","description":"nameless: dropped"},
		{"name":"syntax","description":"duplicate: dropped"},
		{"name":"Modules","description":""},
		{"name":"Testing","description":""}
	]}`}
	_, nodes, err := BuildRoadmap(context.Background(), g, "Go", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[1].Name != "Modules" || nodes[2].Parent != 0 {
		t.Errorf("a short reply should take the first slots of the shape, got %+v", nodes)
	}

	if _, _, err := BuildRoadmap(context.Background(), &fakeGen{reply: `{"category":"Craft","nodes":[]}`}, "Go", 1, ""); err == nil {
		t.Error("expected an error for an empty roadmap")
	}
	if _, _, err := BuildRoadmap(context.Background(), &fakeGen{reply: eightNodes("Sports")}, "Zorbing", 1, ""); err == nil {
		t.Error("expected an error for an unknown category")
	}
}

// A programming language keeps one category whatever a model says, so
// Golang does not flip between Craft and Mind.
func TestKnownSkillsKeepTheirCategory(t *testing.T) {
	g := &fakeGen{reply: eightNodes("Mind")}
	if cat, _, _ := BuildRoadmap(context.Background(), g, "Golang", 4, ""); cat != "Craft" {
		t.Errorf("roadmap category = %q, want Craft", cat)
	}
	audit := &fakeGen{reply: `{"evidence_summary":"x","start_level":4,"is_rusty":false,"category":"Mind"}`}
	if a, _ := AuditSkill(context.Background(), audit, "Golang", "years at work"); a.Category != "Craft" {
		t.Errorf("audit category = %q, want Craft", a.Category)
	}
	plan := &fakeGen{reply: `{"skill":"Golang","category":"Mind","difficulty":2,"minutes":30}`}
	if a, _ := Classify(context.Background(), plan, "Write a worker pool", "", []SkillOutline{{Name: "Golang"}}); a.Category != "Craft" {
		t.Errorf("plan category = %q, want Craft", a.Category)
	}
	if cat, _ := CategorizeSkill(context.Background(), &fakeGen{reply: `{"category":"Mind"}`}, "Golang"); cat != "Craft" {
		t.Errorf("classifier category = %q, want Craft", cat)
	}
	// An unknown skill still takes the model's answer.
	if cat, _, _ := BuildRoadmap(context.Background(), g, "Zorbing", 1, ""); cat != "Mind" {
		t.Errorf("unknown skill category = %q, want the model's Mind", cat)
	}
}
