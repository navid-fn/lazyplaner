package rpg

import (
	"context"
	"strings"
	"testing"
)

func TestAnalyzeNoteShieldsASickDay(t *testing.T) {
	g := &fakeGen{reply: `{"mood_or_status":"Sick","protect_streak":true,"detected_skill_name":"","estimated_minutes":0,"follow_up_action":""}`}
	a, err := AnalyzeNote(context.Background(), g, "I was so sick today, I could not do anything", []string{"Go"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusSick || !a.Shield || a.Claimable() || SignifierFor(a) != SignBlocker {
		t.Errorf("analysis = %+v, want a shielded blocker with nothing to claim", a)
	}
	if !strings.Contains(g.gotPrompt, "EXISTING SKILLS: Go") {
		t.Errorf("prompt should list the skills, got %q", g.gotPrompt)
	}
}

func TestAnalyzeNoteClaimsOnlyExistingSkills(t *testing.T) {
	cases := []struct {
		name, reply string
		wantSkill   string
		wantMinutes int
		wantSign    Signifier
	}{
		{"matches ignoring case", `{"mood_or_status":"Energized","protect_streak":false,"detected_skill_name":"running","estimated_minutes":30,"follow_up_action":""}`, "Running", 30, SignWin},
		{"made-up skill is dropped", `{"mood_or_status":"Normal","protect_streak":false,"detected_skill_name":"Juggling","estimated_minutes":30,"follow_up_action":""}`, "", 0, SignNote},
		{"no minutes, no claim", `{"mood_or_status":"Normal","protect_streak":false,"detected_skill_name":"Go","estimated_minutes":0,"follow_up_action":""}`, "", 0, SignNote},
		{"minutes are capped", `{"mood_or_status":"Normal","protect_streak":false,"detected_skill_name":"Go","estimated_minutes":600,"follow_up_action":""}`, "Go", maxMinutes, SignWin},
	}
	for _, c := range cases {
		a, err := AnalyzeNote(context.Background(), &fakeGen{reply: c.reply}, "note", []string{"Go", "Running"})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if a.Skill != c.wantSkill || a.Minutes != c.wantMinutes || SignifierFor(a) != c.wantSign {
			t.Errorf("%s: got %+v sign %v", c.name, a, SignifierFor(a))
		}
	}
}

func TestAnalyzeNoteShieldFollowsStatusNotTheModelsFlag(t *testing.T) {
	// The model asks to protect a blocked day; only illness and burnout do.
	g := &fakeGen{reply: `{"mood_or_status":"Blocked","protect_streak":true,"detected_skill_name":"","estimated_minutes":0,"follow_up_action":"  Fix the CI build  "}`}
	a, err := AnalyzeNote(context.Background(), g, "CI is broken", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Shield {
		t.Error("a blocked day should not be shielded")
	}
	if a.FollowUp != "Fix the CI build" {
		t.Errorf("FollowUp = %q, want it trimmed", a.FollowUp)
	}
	if !Shields(StatusExhausted) || Shields(StatusEnergized) {
		t.Error("burnout shields, a good day does not")
	}
}

func TestRecoverySlotsAreLight(t *testing.T) {
	slots := RecoverySlots([]SkillOutline{{Name: "Go"}, {Name: "Running"}, {Name: "Chess"}})
	if len(slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(slots))
	}
	for _, s := range slots {
		if s.Tier != TierRecovery {
			t.Errorf("slot %+v is not a recovery slot", s)
		}
	}
	if RecoverySlots(nil) != nil {
		t.Error("no skills, no slots")
	}
	spec := tierSpecs[TierRecovery]
	if spec.maxDiff > 1 || spec.maxMins > 15 {
		t.Errorf("recovery should be easy and short, got %+v", spec)
	}
}
