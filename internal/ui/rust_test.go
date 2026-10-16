package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/game"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

func TestSetupExperienceRunsTheAuditor(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 160, 44
	m.updateSizes()
	m = send(t, m, roadLoadedMsg{})

	m = typeText(t, m, "Go")
	m = tabTo(t, m, fieldExperience)
	m = typeText(t, m, "Wrote Go at work for two years, stopped last spring")
	m = send(t, m, key("enter"))

	if len(m.onboard.pending) != 1 || !m.onboard.pending[0].Auditing {
		t.Fatalf("a skill with experience should wait for the auditor: %+v", m.onboard.pending)
	}
	if m.onboard.experience.Value() != "" {
		t.Error("the experience field should clear after adding")
	}

	// Building is blocked until the auditor answers.
	m = send(t, m, key("ctrl+s"))
	if m.isLoading || !strings.Contains(m.onboard.note, "auditor is still reading") {
		t.Errorf("build should wait for the auditor, loading=%v note=%q", m.isLoading, m.onboard.note)
	}

	m = send(t, m, auditDoneMsg{name: "go", audit: rpg.Audit{Evidence: "two years at work", StartLevel: 4, Rusty: true}})
	p := m.onboard.pending[0]
	if p.Auditing || p.Level != 4 || !p.Rusty || p.Evidence != "two years at work" {
		t.Errorf("auditor answer not applied: %+v", p)
	}
	view := m.View()
	for _, want := range []string{"LV 4", "by AI audit", "RUSTY"} {
		if !strings.Contains(view, want) {
			t.Errorf("the skill card should show %q", want)
		}
	}
}

func TestSetupAuditorFailureKeepsHandPickedLevel(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = typeText(t, m, "Chess")
	m = tabTo(t, m, fieldLevel)
	m = send(t, m, key("right")) // hand-picked level 4
	m = tabTo(t, m, fieldExperience)
	m = typeText(t, m, "club player")
	m = send(t, m, key("enter"))

	m = send(t, m, auditDoneMsg{name: "Chess", err: errors.New("ollama down")})
	p := m.onboard.pending[0]
	if p.Auditing || p.Level != 4 || p.Rusty {
		t.Errorf("a failed audit should keep the hand-picked level 4: %+v", p)
	}
	if !m.onboard.noteError {
		t.Error("the failure should be shown")
	}
}

func TestSetupWithoutExperienceSkipsTheAuditor(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = typeText(t, m, "Running")
	m = send(t, m, key("enter"))
	if p := m.onboard.pending[0]; p.Auditing || p.Level != 3 {
		t.Errorf("without experience the picked level is used directly: %+v", p)
	}
}

func TestSetupDropsAuditForRemovedSkill(t *testing.T) {
	m := newFlowModel(t)
	m = send(t, m, roadLoadedMsg{})
	m = typeText(t, m, "Go")
	m = tabTo(t, m, fieldExperience)
	m = typeText(t, m, "years of it")
	m = send(t, m, key("enter"))
	m = send(t, m, key("ctrl+d"))
	m = send(t, m, auditDoneMsg{name: "Go", audit: rpg.Audit{StartLevel: 5}})
	if len(m.onboard.pending) != 0 {
		t.Errorf("an answer for a removed skill should be dropped, pending=%+v", m.onboard.pending)
	}
}

func TestApplyRustMarksStaleActiveSkills(t *testing.T) {
	ctx := context.Background()
	m := newFlowModel(t)
	store := m.store

	mk := func(name string) {
		if _, err := store.CreateSkillTree(ctx, db.NewSkillTree{Name: name, Category: "Mind", StartLevel: 2,
			Nodes: []db.RoadmapNode{
				{Parent: -1, Name: name + " basics", RequiredLevel: 1},
				{Parent: 0, Name: name + " core", RequiredLevel: 2},
			}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("Go")
	mk("Chess")
	mk("Piano")
	skills, _ := store.ListSkills(ctx)
	ids := map[string]int64{}
	for _, s := range skills {
		ids[s.Name] = s.ID
	}
	if err := store.SetSkillActive(ctx, ids["Piano"], false); err != nil {
		t.Fatal(err)
	}

	rustyNodes := func() map[string]bool {
		nodes, err := store.ListNodes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, n := range nodes {
			if n.IsRusty {
				out[n.Name] = true
			}
		}
		return out
	}

	// Ten days on, nothing has gone 14 days without practice.
	if err := game.ApplyRust(ctx, store, time.Now().Add(10*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if r := rustyNodes(); len(r) != 0 {
		t.Errorf("nothing should rust within 14 days, got %v", r)
	}

	// Three weeks on, the active skills rust at their frontier; the
	// garrisoned one does not.
	later := time.Now().Add(21 * 24 * time.Hour)
	if err := game.ApplyRust(ctx, store, later); err != nil {
		t.Fatal(err)
	}
	rusty := rustyNodes()
	if !rusty["Go core"] || !rusty["Chess core"] {
		t.Errorf("active skills idle three weeks should rust at their frontier, got %v", rusty)
	}
	if rusty["Go basics"] {
		t.Error("only the frontier rusts, not the done basics")
	}
	if rusty["Piano core"] {
		t.Error("a garrisoned skill should not rust")
	}

	// Running it again changes nothing.
	if err := game.ApplyRust(ctx, store, later); err != nil {
		t.Fatal(err)
	}
	if len(rustyNodes()) != len(rusty) {
		t.Error("applying rust twice should not mark more nodes")
	}
}

func TestTreeShowsRustyNode(t *testing.T) {
	m := newFlowModel(t)
	m.width, m.height = 160, 40
	m.skills = []db.Skill{{ID: 1, Name: "Go", Category: "Mind", TotalXP: 100}}
	m.nodes = []db.Node{
		{ID: 1, SkillID: 1, Name: "Basics", RequiredLevel: 1, XPNeeded: 40, Progress: 40},
		{ID: 2, SkillID: 1, ParentID: 1, Name: "Interfaces", RequiredLevel: 2, XPNeeded: 80, IsRusty: true},
	}
	m.openTree()
	m = send(t, m, key("l")) // select Interfaces

	view := m.View()
	for _, want := range []string{"⚠ Rusty ▸ Interfaces", "RUSTY", "comeback"} {
		if !strings.Contains(view, want) {
			t.Errorf("tree should show %q for a rusty node", want)
		}
	}
	done, open, locked := countStatuses(m.treeRows())
	if done != 1 || open != 1 || locked != 0 {
		t.Errorf("a rusty node counts as open: done %d open %d locked %d", done, open, locked)
	}
}

func TestComebackStatusNote(t *testing.T) {
	msg := planSavedMsg{xp: &db.XPEvent{XP: 10, SkillName: "Go", Category: "Mind", Comeback: true}}
	if s := planSavedStatus(msg); !strings.Contains(s, "comeback") {
		t.Errorf("status should mention the comeback: %q", s)
	}
	msg.xp.Comeback = false
	if s := planSavedStatus(msg); strings.Contains(s, "comeback") {
		t.Errorf("no comeback, no note: %q", s)
	}
}
