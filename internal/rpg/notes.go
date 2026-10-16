package rpg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Note statuses the analyzer may report. "" means a note was not analyzed.
const (
	StatusNormal    = "Normal"
	StatusEnergized = "Energized"
	StatusSick      = "Sick"
	StatusExhausted = "Exhausted"
	StatusBlocked   = "Blocked"
)

// NoteStatuses lists every status the analyzer may report.
var NoteStatuses = []string{StatusNormal, StatusEnergized, StatusSick, StatusExhausted, StatusBlocked}

// ClaimDifficulty is the difficulty used for XP claimed from a note. A note
// says what was done and for how long, not how hard it was, so every claim
// counts as moderate work.
const ClaimDifficulty = 2

// NoteAnalysis is what the analyzer read from a daily note, after the code
// has checked it.
type NoteAnalysis struct {
	Status string
	// Shield protects the note's day from breaking the streak. The code
	// sets it for illness and burnout only, whatever the model says.
	Shield bool
	// Skill is an existing skill the note describes finished work on, and
	// Minutes how long it took. Skill is "" when the note names no
	// existing skill.
	Skill    string
	Minutes  int
	FollowUp string // one actionable task the note asks for, or ""
}

// Claimable reports whether the note describes work that can be claimed as
// XP.
func (a NoteAnalysis) Claimable() bool {
	return a.Skill != "" && a.Minutes > 0
}

// ClaimXP is the XP the note's work is worth.
func (a NoteAnalysis) ClaimXP() int {
	return Award(Activity{Difficulty: ClaimDifficulty, Minutes: a.Minutes})
}

// Shields reports whether a status protects the day's streak: illness and
// burnout do, being blocked or tired of a task does not.
func Shields(status string) bool {
	return status == StatusSick || status == StatusExhausted
}

// Signifier is a note's bullet-journal mark.
type Signifier int

const (
	SignNone    Signifier = iota // not analyzed yet
	SignNote                     // • a reflection or idea
	SignWin                      // ★ finished work
	SignBlocker                  // ! illness, burnout or an obstacle
)

// SignifierFor picks a note's mark. A blocker wins over a win, so a sick
// day that still logged some work is marked as the sick day it was.
func SignifierFor(a NoteAnalysis) Signifier {
	switch {
	case a.Status == "":
		return SignNone
	case a.Status == StatusSick || a.Status == StatusExhausted || a.Status == StatusBlocked:
		return SignBlocker
	case a.Claimable():
		return SignWin
	}
	return SignNote
}

const noteSystem = `You read one entry from a personal daily log and fill a small form.
mood_or_status: Sick if the person was ill; Exhausted if burnt out or drained;
Blocked if something stopped their work; Energized if they felt great; else Normal.
protect_streak: true only when illness or burnout kept them from working.
detected_skill_name: the one EXISTING SKILL the entry says they practised, spelled
exactly as listed, or "" if none of them fits. Never invent a skill.
estimated_minutes: how long that practice took, or 0 if no skill was practised.
follow_up_action: one short actionable task if the entry says something needs
fixing or doing, starting with a verb; else "".

Examples:
"I was so sick today, I could not do anything" -> {"mood_or_status":"Sick","protect_streak":true,"detected_skill_name":"","estimated_minutes":0,"follow_up_action":""}
"Ran 5k this morning, felt amazing" (skills: Running) -> {"mood_or_status":"Energized","protect_streak":false,"detected_skill_name":"Running","estimated_minutes":30,"follow_up_action":""}
"Spent an hour on Go but the build is broken, need to fix the CI" (skills: Go) -> {"mood_or_status":"Blocked","protect_streak":false,"detected_skill_name":"Go","estimated_minutes":60,"follow_up_action":"Fix the broken CI build"}`

const noteSchemaJSON = `{
  "type": "object",
  "properties": {
    "mood_or_status":      {"type": "string", "enum": ["Normal", "Energized", "Sick", "Exhausted", "Blocked"]},
    "protect_streak":      {"type": "boolean"},
    "detected_skill_name": {"type": "string"},
    "estimated_minutes":   {"type": "integer", "minimum": 0, "maximum": 600},
    "follow_up_action":    {"type": "string"}
  },
  "required": ["mood_or_status", "protect_streak", "detected_skill_name", "estimated_minutes", "follow_up_action"]
}`

type noteReply struct {
	Status        string `json:"mood_or_status"`
	ProtectStreak bool   `json:"protect_streak"`
	Skill         string `json:"detected_skill_name"`
	Minutes       int    `json:"estimated_minutes"`
	FollowUp      string `json:"follow_up_action"`
}

// maxFollowUp is the longest follow-up kept; it becomes a plan subject.
const maxFollowUp = 120

// AnalyzeNote asks the model to read a daily note. skills are the names of
// the existing skills; a skill the model names that is not one of them is
// dropped, so a note can never create a skill or claim XP on a made-up one.
// The streak shield follows the status: illness and burnout shield the day.
func AnalyzeNote(ctx context.Context, g Generator, body string, skills []string) (NoteAnalysis, error) {
	prompt := "EXISTING SKILLS: (none)\n"
	if len(skills) > 0 {
		prompt = "EXISTING SKILLS: " + strings.Join(skills, ", ") + "\n"
	}
	prompt += "Entry: " + strings.TrimSpace(body)

	raw, err := g.GenerateJSON(ctx, noteSystem, prompt, json.RawMessage(noteSchemaJSON))
	if err != nil {
		return NoteAnalysis{}, fmt.Errorf("analyze note: %w", err)
	}
	var reply noteReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return NoteAnalysis{}, fmt.Errorf("parse note analysis: %w", err)
	}

	a := NoteAnalysis{Status: normalize(reply.Status, NoteStatuses)}
	if !contains(NoteStatuses, a.Status) {
		a.Status = StatusNormal
	}
	a.Shield = Shields(a.Status)

	if skill := normalize(reply.Skill, skills); contains(skills, skill) && reply.Minutes > 0 {
		a.Skill = skill
		a.Minutes = clamp(reply.Minutes, minMinutes, maxMinutes)
	}

	a.FollowUp = strings.Trim(strings.TrimSpace(reply.FollowUp), `"`)
	if r := []rune(a.FollowUp); len(r) > maxFollowUp {
		a.FollowUp = strings.TrimSpace(string(r[:maxFollowUp]))
	}
	return a, nil
}
