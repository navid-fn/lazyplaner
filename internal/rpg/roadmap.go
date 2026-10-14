package rpg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// RoadNode is one node of a generated skill tree. Parent is the index of the
// parent node in the same slice, or -1 for a node that hangs off the skill.
type RoadNode struct {
	Parent        int
	Name          string
	Description   string
	RequiredLevel int
}

// RoadmapSize is how many sub-skills the model is asked for.
const RoadmapSize = 8

// The tree's shape is fixed in code, because small models chain every node
// into one line when asked for parents. treeParents gives each of the eight
// nodes, in learning order, the index of its parent (-1 for the skill):
//
//	column 1: 0 1        two roots
//	column 2: 2 3 ← 0,   4 ← 1
//	column 3: 5 ← 3,     6 ← 4
//	column 4: 7 ← 5      the capstone
//
// Every parent comes before its children, so a shorter reply keeps a valid
// tree by taking the first nodes.
var (
	treeParents = [RoadmapSize]int{-1, -1, 0, 0, 1, 3, 4, 5}
	treeColumn  = [RoadmapSize]int{1, 1, 2, 2, 2, 3, 3, 4}
)

// roadmapSystem names no other domain on purpose: small models copy any
// example they are given into the tree.
const roadmapSystem = `You list the sub-skills of ONE skill for a personal RPG planner, in learning
order: from the foundations to advanced mastery. Each sub-skill is something a person can
practise and track, named in 2 to 4 words, with a one-sentence practical milestone that says
what the person can do once they have it.
Every single node MUST be 100% strictly about the given skill. Never combine it with or
mention other hobbies, skills or languages.
Never use generic names such as "Practice" or "Advanced Topics".`

var roadmapSchemaJSON = fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "category": {"type": "string", "enum": ["Mind", "Body", "Craft", "Social", "Life"]},
    "nodes": {
      "type": "array",
      "minItems": %[1]d,
      "maxItems": %[1]d,
      "items": {
        "type": "object",
        "properties": {
          "name":        {"type": "string"},
          "description": {"type": "string"}
        },
        "required": ["name", "description"]
      }
    }
  },
  "required": ["category", "nodes"]
}`, RoadmapSize)

type roadmapReply struct {
	Category string `json:"category"`
	Nodes    []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"nodes"`
}

// BuildRoadmap asks the model for a skill tree for one skill, and for the
// category the skill belongs to. The model only lists the sub-skills in
// learning order; the parents and required levels are set in code (see
// shapeTree), from level, the person's starting level in the skill. goal,
// when given, is what the person wants to master or build; the advanced
// nodes are shaped toward it. Nothing about the person's other skills is
// sent, so they cannot leak into this tree.
func BuildRoadmap(ctx context.Context, g Generator, skill string, level int, goal string) (string, []RoadNode, error) {
	skill = strings.TrimSpace(skill)
	prompt := fmt.Sprintf(
		"Skill: %[1]s\nStarting level: %[2]d of 10.\n%[3]s"+
			"Pick the category that fits %[1]s: Mind (learning, study), Body (exercise, health), "+
			"Craft (making things, technical skills, programming), Social (people, communication), "+
			"Life (chores, admin, money).\n"+
			"List exactly %[4]d sub-skills of %[1]s, from foundational to advanced mastery. "+
			"Every node must be strictly about %[1]s.%[5]s",
		skill, max(level, 1), goalClause(goal), RoadmapSize, goalReminder(goal),
	)

	raw, err := g.GenerateJSON(ctx, roadmapSystem, prompt, json.RawMessage(roadmapSchemaJSON))
	if err != nil {
		return "", nil, fmt.Errorf("build roadmap for %s: %w", skill, err)
	}

	var reply roadmapReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return "", nil, fmt.Errorf("parse roadmap for %s: %w", skill, err)
	}
	category := settleCategory(skill, reply.Category)
	if !contains(Categories, category) {
		return "", nil, fmt.Errorf("roadmap for %s has unknown category %q", skill, reply.Category)
	}
	nodes, err := cleanRoadmap(reply, skill)
	if err != nil {
		return "", nil, err
	}
	return category, shapeTree(nodes, level), nil
}

// cleanRoadmap keeps the named, distinct nodes in order, at most
// RoadmapSize of them.
func cleanRoadmap(reply roadmapReply, skill string) ([]RoadNode, error) {
	seen := map[string]bool{}
	var out []RoadNode
	for _, n := range reply.Nodes {
		name := strings.TrimSpace(n.Name)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, RoadNode{Name: name, Description: strings.TrimSpace(n.Description)})
		if len(out) == RoadmapSize {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("model returned no usable nodes for %s", skill)
	}
	return out, nil
}

// shapeTree wires nodes, in learning order, into the fixed tree shape and
// sets each node's required level from the skill's start level L, so that
// there is always work open at the start:
//
//   - L = 1: both roots are open; column 2 needs Lv 2, column 3 Lv 4, the
//     capstone Lv 6.
//   - L > 1: the roots (Lv L-2, at least 1) and node 2 (Lv L-1) are below
//     the start level, so they start done; nodes 3 and 4 need exactly L, so
//     two nodes are open; column 3 needs L+1, and the capstone L+3, which
//     keeps it in fog.
//
// Nodes below the start level start done (see StartProgress).
func shapeTree(nodes []RoadNode, startLevel int) []RoadNode {
	l := max(startLevel, 1)
	out := make([]RoadNode, len(nodes))
	for i, n := range nodes {
		n.Parent = treeParents[i]
		n.RequiredLevel = nodeLevel(i, l)
		out[i] = n
	}
	return out
}

// nodeLevel is the required level of the node at index i of the tree shape
// for a skill starting at level l.
func nodeLevel(i, l int) int {
	if l == 1 {
		return [...]int{1, 1, 2, 2, 2, 4, 4, 6}[i]
	}
	switch {
	case treeColumn[i] == 1:
		return max(1, l-2)
	case i == 2:
		return l - 1
	case treeColumn[i] == 2:
		return l
	case treeColumn[i] == 3:
		return l + 1
	default:
		return l + 3
	}
}

// goalClause tells the model what the person is aiming for, if anything.
// With a goal, the tree is about that goal: most nodes must train what it
// takes, so a vague wellness or beginner tree does not come back.
func goalClause(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	return fmt.Sprintf("Target goal: %q.\n"+
		"CRITICAL: at least %d of the %d nodes MUST directly train the specific sub-skills "+
		"this exact goal needs. Do NOT output generic filler nodes such as hydration, sleep, "+
		"mental health or basic syntax when a goal is given.\n",
		truncateRunes(goal, MaxGoalLength), RoadmapSize-2, RoadmapSize)
}

// goalReminder repeats the goal at the end of the prompt, where small models
// pay the most attention.
func goalReminder(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	return fmt.Sprintf(" Build the nodes toward the goal: %q.", truncateRunes(goal, MaxGoalLength))
}

// MaxGoalLength caps the goal sentence sent to the model.
const MaxGoalLength = 160

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Audit is the skill auditor's reading of a person's past experience.
type Audit struct {
	Evidence   string // short summary of what they said, in the model's words
	StartLevel int    // 1 to 5, the level their tree starts at
	Rusty      bool   // they used to practise but stopped a while ago
	Category   string // one of Categories, so the setup card can show it
}

// MaxExperienceLength caps the experience sentence sent to the auditor.
const MaxExperienceLength = 240

// auditSystem is written for small local models (3B to 7B): a four-line
// rubric and one example, nothing else to get lost in.
const auditSystem = `You rate a person's real-world experience in one skill.
Be strict: when unsure between two levels, pick the lower one.
Rubric:
start_level 1-2: little or no hands-on practice, only reading or watching.
start_level 3: practises regularly or finished a few small projects, but not for years.
start_level 4-5: several years of steady practice or paid work; 5 only for professional mastery.
is_rusty: true only if they used to practise and then stopped for months or years; anything recent is false.
category: Mind (learning, study), Body (exercise, health), Craft (making, technical), Social (people), Life (chores, money).

Example:
Skill: Guitar
Experience: Played in a band for three years in college but have not touched it since 2019.
{"evidence_summary":"three years of band practice, stopped years ago","start_level":3,"is_rusty":true,"category":"Craft"}`

// auditSchema is deliberately flat, and evidence_summary comes first: Ollama
// fills fields in schema order, so the model states its evidence before it
// commits to a level.
const auditSchemaJSON = `{
  "type": "object",
  "properties": {
    "evidence_summary": {"type": "string"},
    "start_level":      {"type": "integer", "minimum": 1, "maximum": 5},
    "is_rusty":         {"type": "boolean"},
    "category":         {"type": "string", "enum": ["Mind", "Body", "Craft", "Social", "Life"]}
  },
  "required": ["evidence_summary", "start_level", "is_rusty", "category"]
}`

type auditReply struct {
	Evidence   string `json:"evidence_summary"`
	StartLevel int    `json:"start_level"`
	Rusty      bool   `json:"is_rusty"`
	Category   string `json:"category"`
}

// AuditSkill asks the model to turn a one-sentence description of past
// experience into a start level and a rusty flag.
func AuditSkill(ctx context.Context, g Generator, skill, experience string) (Audit, error) {
	experience = strings.TrimSpace(experience)
	if experience == "" {
		return Audit{}, errors.New("no experience to audit")
	}
	prompt := fmt.Sprintf("Skill: %s\nExperience: %s",
		strings.TrimSpace(skill), truncateRunes(experience, MaxExperienceLength))

	raw, err := g.GenerateJSON(ctx, auditSystem, prompt, json.RawMessage(auditSchemaJSON))
	if err != nil {
		return Audit{}, fmt.Errorf("audit %s: %w", skill, err)
	}
	var reply auditReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return Audit{}, fmt.Errorf("parse audit for %s: %w", skill, err)
	}

	// An unknown category is left empty; the tree build then picks one.
	category := settleCategory(skill, reply.Category)
	if !contains(Categories, category) {
		category = ""
	}
	return Audit{
		Evidence:   truncateRunes(strings.TrimSpace(reply.Evidence), 120),
		StartLevel: clamp(reply.StartLevel, 1, 5),
		Rusty:      reply.Rusty,
		Category:   category,
	}, nil
}

// truncateRunes shortens s to at most n runes.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
