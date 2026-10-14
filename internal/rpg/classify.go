package rpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Generator is the part of the LLM client the rpg package needs. Keeping it
// as an interface lets the tests run without Ollama.
type Generator interface {
	GenerateJSON(ctx context.Context, system string, prompt string, schema json.RawMessage) (string, error)
}

// SkillOutline is a skill as the model sees it: its name, the character's
// level in it, and the names of the nodes already in its tree.
type SkillOutline struct {
	Name  string
	Level int
	Nodes []string
}

// Activity is what a finished plan contributed to the character.
type Activity struct {
	Skill      string
	Category   string
	Difficulty int
	Minutes    int
	// Node is the sub-skill inside the skill's tree that the plan trains.
	// Parent is the node it grows from; empty means the skill root. Both are
	// empty when the plan fits no node.
	Node   string
	Parent string
}

const classifySystem = `You turn a personal plan into RPG progress data.
Categories: Mind (learning, study, reading), Body (exercise, health, sleep),
Craft (making or building things, technical skills), Social (people, communication),
Life (chores, errands, admin, finance).
Skill: the single general skill the plan trains, 1-3 words. If it matches one of the
EXISTING SKILLS, reuse that exact name.
Node: the specific sub-skill inside that skill's tree the plan trains. Reuse an existing
node name when one fits. Otherwise invent a short new node name (1-3 words) and set
parent to the existing node it builds on, or "" to attach it to the skill itself.
Leave node and parent empty when the plan does not fit the skill's tree.
Difficulty: 1 trivial ... 5 very hard for an average person.
Minutes: total realistic effort.

Examples:
Plan: Read two chapters of a Rust book -> {"skill":"Rust","category":"Mind","difficulty":2,"minutes":60,"node":"Ownership","parent":""}
Plan: 5 km run before breakfast -> {"skill":"Running","category":"Body","difficulty":3,"minutes":35,"node":"","parent":""}
Plan: Pick up milk and bread -> {"skill":"Errands","category":"Life","difficulty":1,"minutes":20,"node":"","parent":""}`

// classifySchema constrains the model's output. Ollama enforces it while
// decoding, so a reply always has these fields with valid enum values.
var classifySchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "skill":      {"type": "string"},
    "category":   {"type": "string", "enum": ["Mind", "Body", "Craft", "Social", "Life"]},
    "difficulty": {"type": "integer", "minimum": 1, "maximum": 5},
    "minutes":    {"type": "integer", "minimum": 5, "maximum": 600},
    "node":       {"type": "string"},
    "parent":     {"type": "string"}
  },
  "required": ["skill", "category", "difficulty", "minutes", "node", "parent"]
}`)

type classifyReply struct {
	Skill      string `json:"skill"`
	Category   string `json:"category"`
	Difficulty int    `json:"difficulty"`
	Minutes    int    `json:"minutes"`
	Node       string `json:"node"`
	Parent     string `json:"parent"`
}

// Classify asks the model which skill a plan trains, how hard it is, and
// which node of that skill's tree it belongs to. skills lists the existing
// skills, so the model can reuse their names instead of inventing duplicates.
func Classify(ctx context.Context, g Generator, subject, detail string, skills []SkillOutline) (Activity, error) {
	prompt := fmt.Sprintf(
		"EXISTING SKILLS:\n%s\nPlan: %s\nDetail: %s",
		formatOutlines(skills),
		strings.TrimSpace(subject),
		strings.TrimSpace(detail),
	)

	raw, err := g.GenerateJSON(ctx, classifySystem, prompt, classifySchema)
	if err != nil {
		return Activity{}, fmt.Errorf("classify plan: %w", err)
	}

	var reply classifyReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return Activity{}, fmt.Errorf("parse classification: %w", err)
	}

	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	skill := normalize(reply.Skill, names)
	if skill == "" {
		return Activity{}, errors.New("classification has no skill name")
	}
	if !slices.Contains(Categories, reply.Category) {
		return Activity{}, fmt.Errorf("unknown category %q", reply.Category)
	}

	// Node names are checked against the tree of the chosen skill only.
	var nodes []string
	for _, s := range skills {
		if strings.EqualFold(s.Name, skill) {
			nodes = s.Nodes
		}
	}
	node := normalize(reply.Node, nodes)
	parent := normalize(reply.Parent, nodes)
	// A parent only makes sense for a node, and a node cannot be its own parent.
	if node == "" || strings.EqualFold(node, parent) {
		parent = ""
	}

	return Activity{
		Skill:      skill,
		Category:   reply.Category,
		Difficulty: clamp(reply.Difficulty, minDiff, maxDiff),
		Minutes:    clamp(reply.Minutes, minMinutes, maxMinutes),
		Node:       node,
		Parent:     parent,
	}, nil
}

// normalize trims name and, when it matches a known name ignoring case,
// returns the known spelling. This keeps "go" and "Go" from becoming two
// skills, and keeps node names consistent.
func normalize(name string, known []string) string {
	name = strings.TrimSpace(name)
	for _, k := range known {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return name
}

func formatOutlines(skills []SkillOutline) string {
	if len(skills) == 0 {
		return "(none yet)"
	}
	var b strings.Builder
	for _, s := range skills {
		nodes := "(no nodes)"
		if len(s.Nodes) > 0 {
			nodes = strings.Join(s.Nodes, ", ")
		}
		fmt.Fprintf(&b, "- %s (level %d): %s\n", s.Name, s.Level, nodes)
	}
	return b.String()
}
