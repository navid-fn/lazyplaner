package rpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Tier is the slot a daily directive fills. A day's set is one directive
// per tier, from a quick warm-up to a deep push; Overtime is the one bonus
// directive allowed after the set is finished.
type Tier int

const (
	TierNone     Tier = iota // made before tiers existed
	TierRecon                // fast warm-up or habit task
	TierCoreOp               // hands-on practice on an open node
	TierDeepPush             // deep work milestone
	TierOvertime             // the day's single bonus, once the set is done
	TierFocus                // asked for one node from the skill tree
)

// tierSpec is what a tier asks of the model, and the bounds its answer is
// held to.
type tierSpec struct {
	icon, name, time string
	minDiff, maxDiff int
	minMins, maxMins int
	brief            string
}

var tierSpecs = map[Tier]tierSpec{
	TierRecon:    {"⚡", "Recon", "15m", 1, 2, 10, 15, "a fast warm-up or habit task"},
	TierCoreOp:   {"🔧", "Core Op", "30-45m", 3, 3, 30, 45, "hands-on practice on one open node"},
	TierDeepPush: {"🔥", "Deep Push", "60m", 4, 5, 60, 60, "a deep work milestone that produces something finished"},
	TierOvertime: {"★", "Bonus Overtime", "30m", 3, 3, 30, 30, "one extra hands-on practice"},
	TierFocus:    {"🎯", "Focus", "30-45m", 3, 3, 30, 45, "hands-on practice on exactly this node"},
}

// Label is the tier as shown on a directive, e.g. "⚡ Recon · 15m". A
// directive without a tier has no label.
func (t Tier) Label() string {
	s, ok := tierSpecs[t]
	if !ok {
		return ""
	}
	return s.icon + " " + s.name + " · " + s.time
}

// Short is the tier's icon and name, for tight rows.
func (t Tier) Short() string {
	s, ok := tierSpecs[t]
	if !ok {
		return ""
	}
	return s.icon + " " + s.name
}

// DirectiveSlot is one directive to generate: its tier and the skill it
// trains. Nodes lists the nodes the directive may target; callers pass only
// nodes that are open or rusty, never locked ones.
type DirectiveSlot struct {
	Tier  Tier
	Skill string
	Level int
	Nodes []string
}

// dailyTiers is the order of a day's set. A fourth active skill gets a
// second Core Op.
var dailyTiers = []Tier{TierRecon, TierCoreOp, TierDeepPush, TierCoreOp}

// MaxDailyDirectives is the most directives a day's set can have.
const MaxDailyDirectives = 4

// DailySlots lays out a day's set over the given skills: at least three
// directives (Recon, Core Op, Deep Push), one more for a fourth skill, and
// never more than MaxDailyDirectives. Skills are dealt in order, so with
// fewer than three skills some get more than one directive.
func DailySlots(skills []SkillOutline) []DirectiveSlot {
	if len(skills) == 0 {
		return nil
	}
	n := clamp(len(skills), 3, MaxDailyDirectives)
	slots := make([]DirectiveSlot, 0, n)
	for i := 0; i < n; i++ {
		s := skills[i%len(skills)]
		slots = append(slots, DirectiveSlot{Tier: dailyTiers[i], Skill: s.Name, Level: s.Level, Nodes: s.Nodes})
	}
	return slots
}

// ChallengeDraft is one generated daily directive, before it is saved.
type ChallengeDraft struct {
	Tier        Tier
	Skill       string
	Node        string
	Title       string
	Description string
	Difficulty  int
	Minutes     int
	XP          int
}

const directiveSystem = `You write daily directives for a personal RPG planner.
A directive is one concrete action the person can finish today in one sitting.
Title rules:
- Start with a concrete action verb and name the specific scope, for example
  "Write a buffered channel worker pool", "Sketch 4 angry/happy eye studies",
  "Cook pan-seared chicken with pan sauce".
- NEVER end a title with the word "Challenge", "Task" or "Study", and never just
  repeat the node or skill name.
Every directive needs a description of one or two short sentences, under 160
characters, that says exactly what to do and how the person will know it is finished.
Fit each directive to its slot's difficulty and time.`

const directiveSchemaJSON = `{
  "type": "object",
  "properties": {
    "directives": {
      "type": "array",
      "minItems": 1,
      "maxItems": 4,
      "items": {
        "type": "object",
        "properties": {
          "slot":        {"type": "integer", "minimum": 1, "maximum": 4},
          "node":        {"type": "string"},
          "title":       {"type": "string"},
          "description": {"type": "string"},
          "difficulty":  {"type": "integer", "minimum": 1, "maximum": 5},
          "minutes":     {"type": "integer", "minimum": 5, "maximum": 120}
        },
        "required": ["slot", "node", "title", "description", "difficulty", "minutes"]
      }
    }
  },
  "required": ["directives"]
}`

type directiveReply struct {
	Directives []struct {
		Slot        int    `json:"slot"`
		Node        string `json:"node"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Difficulty  int    `json:"difficulty"`
		Minutes     int    `json:"minutes"`
	} `json:"directives"`
}

// BuildDirectives asks the model for one directive per slot. avoid lists
// titles already set today, so a reroll or bonus does not repeat them.
//
// The model only writes the title, description and node; the skill comes
// from the slot, the difficulty and minutes are held to the slot's tier, and
// a node outside the slot's list is cleared, so a directive can never point
// at a locked node. Slots the model leaves out or answers without a usable
// title are skipped.
func BuildDirectives(ctx context.Context, g Generator, slots []DirectiveSlot, avoid []string) ([]ChallengeDraft, error) {
	if len(slots) == 0 {
		return nil, errors.New("no active skills to build directives for")
	}
	if len(slots) > MaxDailyDirectives {
		slots = slots[:MaxDailyDirectives]
	}

	raw, err := g.GenerateJSON(ctx, directiveSystem, directivePrompt(slots, avoid), json.RawMessage(directiveSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("build directives: %w", err)
	}
	var reply directiveReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return nil, fmt.Errorf("parse directives: %w", err)
	}

	// Each slot takes the answer that names it; answers with a missing or
	// repeated slot number fill the remaining slots in order.
	answers := make([]int, len(slots))
	for i := range answers {
		answers[i] = -1
	}
	var loose []int
	for i, d := range reply.Directives {
		if s := d.Slot - 1; s >= 0 && s < len(slots) && answers[s] < 0 {
			answers[s] = i
		} else {
			loose = append(loose, i)
		}
	}
	for s := range answers {
		if answers[s] < 0 && len(loose) > 0 {
			answers[s], loose = loose[0], loose[1:]
		}
	}

	var out []ChallengeDraft
	for s, i := range answers {
		if i < 0 {
			continue
		}
		d := reply.Directives[i]
		slot := slots[s]
		title := CleanDirectiveTitle(d.Title)
		if title == "" {
			continue
		}

		node := normalize(d.Node, slot.Nodes)
		if !contains(slot.Nodes, node) {
			node = ""
			if len(slot.Nodes) == 1 {
				node = slot.Nodes[0]
			}
		}

		spec := tierSpecs[slot.Tier]
		diff := clamp(d.Difficulty, spec.minDiff, spec.maxDiff)
		mins := clamp(d.Minutes, spec.minMins, spec.maxMins)
		out = append(out, ChallengeDraft{
			Tier:        slot.Tier,
			Skill:       slot.Skill,
			Node:        node,
			Title:       title,
			Description: strings.TrimSpace(d.Description),
			Difficulty:  diff,
			Minutes:     mins,
			XP:          Award(Activity{Difficulty: diff, Minutes: mins}),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("model returned no usable directives")
	}
	return out, nil
}

// directivePrompt describes each slot: its tier, skill and the nodes it may
// target.
func directivePrompt(slots []DirectiveSlot, avoid []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Write exactly %d directives, one per slot, and set slot to its number.\n", len(slots))
	for i, s := range slots {
		spec := tierSpecs[s.Tier]
		nodes := "(none; leave node empty)"
		if len(s.Nodes) > 0 {
			nodes = strings.Join(s.Nodes, ", ")
		}
		fmt.Fprintf(&b, "Slot %d: %s, %s. Difficulty %s, %s minutes. Skill: %s (level %d). Node, pick one of: %s\n",
			i+1, spec.name, spec.brief, rangeText(spec.minDiff, spec.maxDiff), rangeText(spec.minMins, spec.maxMins),
			s.Skill, max(s.Level, 1), nodes)
	}
	if len(avoid) > 0 {
		b.WriteString("Do not repeat these directives from earlier today: " + strings.Join(avoid, "; ") + ".\n")
	}
	return b.String()
}

func rangeText(lo, hi int) string {
	if lo == hi {
		return fmt.Sprint(lo)
	}
	return fmt.Sprintf("%d-%d", lo, hi)
}

// bannedTitleEnds are filler words small models tack onto a node name to make
// a title. A title must say what to do instead.
var bannedTitleEnds = []string{"challenge", "task", "study"}

// CleanDirectiveTitle trims a generated title and strips a trailing
// "Challenge", "Task" or "Study". It returns "" when nothing useful is left.
func CleanDirectiveTitle(title string) string {
	title = strings.Trim(strings.TrimSpace(title), ".!:;- ")
	for changed := true; changed; {
		changed = false
		words := strings.Fields(title)
		if len(words) < 2 {
			break
		}
		last := strings.ToLower(strings.Trim(words[len(words)-1], ".!:;-"))
		if contains(bannedTitleEnds, last) {
			title = strings.Trim(strings.Join(words[:len(words)-1], " "), ".!:;- ")
			changed = true
		}
	}
	if contains(bannedTitleEnds, strings.ToLower(title)) {
		return ""
	}
	return title
}
