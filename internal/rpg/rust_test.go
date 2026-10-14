package rpg

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStartProgressPremastersNodesBelowStartLevel(t *testing.T) {
	cases := []struct {
		required, start, want int
	}{
		{1, 4, 40}, // basics below the start level are already done
		{3, 4, 120},
		{4, 4, 0}, // at the start level: open, nothing done yet
		{6, 4, 0}, // above: locked, nothing done
		{1, 1, 0}, // a beginner has nothing pre-mastered
	}
	for _, c := range cases {
		cost := NodeXPNeeded(c.required)
		if got := StartProgress(c.required, c.start, cost); got != c.want {
			t.Errorf("StartProgress(req %d, start %d) = %d, want %d", c.required, c.start, got, c.want)
		}
	}
}

func TestPremasteredTreeHasRightStatuses(t *testing.T) {
	// A skill starting at level 4 with nodes at levels 1, 3, 4 and 6.
	start := 4
	reqs := []int{1, 3, 4, 6}
	var nodes []TreeNode
	for i, r := range reqs {
		cost := NodeXPNeeded(r)
		parent := int64(i) // a chain: each node hangs off the previous one
		nodes = append(nodes, TreeNode{ID: int64(i + 1), ParentID: parent, Required: r,
			XPNeeded: cost, Progress: StartProgress(r, start, cost)})
	}

	st := NodeStatuses(nodes, start)
	want := []Status{Done, Done, Unlocked, Locked}
	for i, w := range want {
		if got := st[int64(i+1)]; got != w {
			t.Errorf("node at level %d = %v, want %v", reqs[i], got, w)
		}
	}
}

func TestRustyStatus(t *testing.T) {
	nodes := []TreeNode{
		{ID: 1, Required: 1, XPNeeded: 40, Progress: 40, Rusty: true}, // done wins over rusty
		{ID: 2, ParentID: 1, Required: 2, XPNeeded: 80, Rusty: true},
		{ID: 3, ParentID: 2, Required: 2, XPNeeded: 80}, // a rusty parent is not done, so its children wait
		{ID: 4, ParentID: 2, Required: 9, XPNeeded: 360, Rusty: true},
	}
	st := NodeStatuses(nodes, 2)
	want := map[int64]Status{1: Done, 2: Rusty, 3: Locked, 4: Locked}
	for id, w := range want {
		if st[id] != w {
			t.Errorf("node %d = %v, want %v", id, st[id], w)
		}
	}
}

func TestFrontierNodeIsHighestOpenNode(t *testing.T) {
	nodes := []TreeNode{
		{ID: 1, Required: 1, XPNeeded: 40, Progress: 40}, // done
		{ID: 2, ParentID: 1, Required: 2, XPNeeded: 80},
		{ID: 3, ParentID: 1, Required: 3, XPNeeded: 120},
		{ID: 4, ParentID: 1, Required: 3, XPNeeded: 120}, // same level, newer: wins the tie
		{ID: 5, ParentID: 1, Required: 6, XPNeeded: 240}, // locked
	}
	if id, ok := FrontierNode(nodes, 3); !ok || id != 4 {
		t.Errorf("FrontierNode = %d, %v; want 4", id, ok)
	}

	allDone := []TreeNode{{ID: 1, Required: 1, XPNeeded: 40, Progress: 40}}
	if _, ok := FrontierNode(allDone, 3); ok {
		t.Error("a tree with nothing open has no frontier")
	}
}

func TestRustTarget(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	nodes := []TreeNode{
		{ID: 1, Required: 1, XPNeeded: 40, Progress: 40},
		{ID: 2, ParentID: 1, Required: 2, XPNeeded: 80},
	}
	stale := now.Add(-15 * 24 * time.Hour)
	fresh := now.Add(-13 * 24 * time.Hour)

	if id, ok := RustTarget(nodes, 2, true, stale, now); !ok || id != 2 {
		t.Errorf("active skill idle 15 days: got %d, %v; want node 2", id, ok)
	}
	if _, ok := RustTarget(nodes, 2, true, fresh, now); ok {
		t.Error("practised 13 days ago: should not rust yet")
	}
	if _, ok := RustTarget(nodes, 2, false, stale, now); ok {
		t.Error("a garrisoned (inactive) skill should never rust")
	}
	if _, ok := RustTarget(nodes, 2, true, time.Time{}, now); ok {
		t.Error("a skill never practised has nothing to measure from")
	}
	if _, ok := RustTarget(nodes, 2, true, now.Add(-RustAfter), now); !ok {
		t.Error("exactly 14 days idle should rust")
	}
}

func TestComebackXP(t *testing.T) {
	cases := map[int]int{8: 10, 12: 15, 36: 45, 1: 2, 2: 3}
	for xp, want := range cases {
		if got := ComebackXP(xp); got != want {
			t.Errorf("ComebackXP(%d) = %d, want %d", xp, got, want)
		}
	}
}

func TestAuditSkillPromptAndSchema(t *testing.T) {
	g := &fakeGen{reply: `{"evidence_summary":"six years of Go at work","start_level":5,"is_rusty":false}`}
	a, err := AuditSkill(context.Background(), g, "Go", "I have written Go professionally for six years.")
	if err != nil {
		t.Fatal(err)
	}
	if a.StartLevel != 5 || a.Rusty || a.Evidence != "six years of Go at work" {
		t.Errorf("audit = %+v", a)
	}
	if !strings.Contains(g.gotPrompt, "Skill: Go") || !strings.Contains(g.gotPrompt, "six years") {
		t.Errorf("prompt should carry the skill and the experience: %q", g.gotPrompt)
	}

	// The schema is flat and evidence_summary comes first, so a small model
	// writes its evidence before it picks a level.
	schema := string(g.gotSchema)
	ev := strings.Index(schema, `"evidence_summary"`)
	lv := strings.Index(schema, `"start_level"`)
	ru := strings.Index(schema, `"is_rusty"`)
	if ev < 0 || lv < 0 || ru < 0 || !(ev < lv && lv < ru) {
		t.Errorf("schema fields out of order: evidence %d, level %d, rusty %d", ev, lv, ru)
	}
	var parsed struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(g.gotSchema, &parsed); err != nil {
		t.Fatal(err)
	}
	for name, p := range parsed.Properties {
		if p.Type == "object" || p.Type == "array" {
			t.Errorf("field %s is %s; the schema must stay flat", name, p.Type)
		}
	}
}

func TestAuditSystemPromptHasRubricAndExample(t *testing.T) {
	rubric := 0
	for _, line := range strings.Split(auditSystem, "\n") {
		if strings.HasPrefix(line, "start_level ") || strings.HasPrefix(line, "is_rusty:") {
			rubric++
		}
	}
	if rubric != 4 {
		t.Errorf("rubric has %d lines, want 4", rubric)
	}
	if strings.Count(auditSystem, "Example:") != 1 {
		t.Error("the prompt should have exactly one example")
	}
	// The example's JSON starts with evidence_summary too.
	if !strings.Contains(auditSystem, `{"evidence_summary":`) {
		t.Error("the example should lead with evidence_summary")
	}
}

func TestAuditSkillClampsAndRejects(t *testing.T) {
	g := &fakeGen{reply: `{"evidence_summary":"x","start_level":9,"is_rusty":true}`}
	a, err := AuditSkill(context.Background(), g, "Go", "lots")
	if err != nil {
		t.Fatal(err)
	}
	if a.StartLevel != 5 || !a.Rusty {
		t.Errorf("level should clamp to 5 and rusty carry through, got %+v", a)
	}

	if _, err := AuditSkill(context.Background(), g, "Go", "   "); err == nil {
		t.Error("empty experience should be rejected before calling the model")
	}
	bad := &fakeGen{reply: `not json`}
	if _, err := AuditSkill(context.Background(), bad, "Go", "some"); err == nil {
		t.Error("a reply that is not JSON should be an error")
	}
	down := &fakeGen{err: errors.New("ollama down")}
	if _, err := AuditSkill(context.Background(), down, "Go", "some"); err == nil {
		t.Error("a generator error should be returned")
	}
}

func TestAuditSkillReturnsCategory(t *testing.T) {
	g := &fakeGen{reply: `{"evidence_summary":"x","start_level":2,"is_rusty":false,"category":"Body"}`}
	a, err := AuditSkill(context.Background(), g, "Running", "I jog twice a week")
	if err != nil {
		t.Fatal(err)
	}
	if a.Category != "Body" {
		t.Errorf("category = %q, want Body", a.Category)
	}

	// An unknown category is dropped rather than trusted.
	g = &fakeGen{reply: `{"evidence_summary":"x","start_level":2,"is_rusty":false,"category":"Sports"}`}
	if a, _ := AuditSkill(context.Background(), g, "Running", "I jog"); a.Category != "" {
		t.Errorf("unknown category should be dropped, got %q", a.Category)
	}
}

func TestBuildRoadmapPassesTheGoal(t *testing.T) {
	g := &fakeGen{reply: `{"category":"Craft","nodes":[{"parent":-1,"name":"Syntax","description":"","required_level":1}]}`}
	if _, _, err := BuildRoadmap(context.Background(), g, "Java", 1, "build an Android app"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.gotPrompt, "Their goal: build an Android app") {
		t.Errorf("prompt should carry the goal: %q", g.gotPrompt)
	}
	if _, _, err := BuildRoadmap(context.Background(), g, "Java", 1, "  "); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(g.gotPrompt, "Their goal") {
		t.Error("an empty goal should leave the prompt alone")
	}
}
