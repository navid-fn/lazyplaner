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

const roadmapSystem = `You design skill trees for a personal RPG planner.
A skill tree starts from basic habits and goes to advanced mastery. Each node is one
concrete sub-skill a person can practise and track. Nodes must be specific to the skill,
not generic (bad: "Practice", good: "Loops and slices").
The tree must branch. Use the parent field so that at least two different nodes
attach to different parents, rather than attaching everything to the first node.
`

const roadmapSchemaJSON = `{
  "type": "object",
  "properties": {
    "category": {"type": "string", "enum": ["Mind", "Body", "Craft", "Social", "Life"]},
    "nodes": {
      "type": "array",
      "minItems": 4,
      "maxItems": 12,
      "items": {
        "type": "object",
        "properties": {
          "parent":         {"type": "integer", "minimum": -1, "maximum": 11},
          "name":           {"type": "string"},
          "description":    {"type": "string"},
          "required_level": {"type": "integer", "minimum": 1, "maximum": 10}
        },
        "required": ["parent", "name", "description", "required_level"]
      }
    }
  },
  "required": ["category", "nodes"]
}`

type roadmapReply struct {
	Category string `json:"category"`
	Nodes    []struct {
		Parent        int    `json:"parent"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		RequiredLevel int    `json:"required_level"`
	} `json:"nodes"`
}

// BuildRoadmap asks the model for a skill tree for one skill, and for the
// category the skill belongs to. level is the character's starting level in
// it, so the model knows where to begin. goal, when given, is what the person
// wants to master or build; the advanced nodes are shaped toward it.
func BuildRoadmap(ctx context.Context, g Generator, skill string, level int, goal string) (string, []RoadNode, error) {
	prompt := fmt.Sprintf(
		"Skill: %s\nThe person already has level %d in this skill.\n%s"+
			"Pick the category that fits the skill: Mind (learning, study, reading), Body (exercise, "+
			"health, sleep), Craft (making things, technical skills), Social (people, communication), "+
			"Life (chores, admin, money). "+
			"Create a skill tree of 6 to 10 nodes. Node 'parent' is the index of the node it grows "+
			"from (earlier in this list), or -1 to attach it to the skill. Use required_level 1 for "+
			"basics and up to 10 for advanced nodes. Keep names to 1-4 words.",
		strings.TrimSpace(skill), level, goalClause(goal),
	)

	raw, err := g.GenerateJSON(ctx, roadmapSystem, prompt, json.RawMessage(roadmapSchemaJSON))
	if err != nil {
		return "", nil, fmt.Errorf("build roadmap for %s: %w", skill, err)
	}

	var reply roadmapReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return "", nil, fmt.Errorf("parse roadmap for %s: %w", skill, err)
	}
	if !contains(Categories, reply.Category) {
		return "", nil, fmt.Errorf("roadmap for %s has unknown category %q", skill, reply.Category)
	}
	nodes, err := cleanRoadmap(reply, skill)
	if err != nil {
		return "", nil, err
	}
	return reply.Category, nodes, nil
}

// cleanRoadmap drops nameless and duplicate nodes, keeps parents pointing at
// nodes that survived and come earlier, and clamps levels. A node whose
// parent was dropped attaches to the skill instead.
func cleanRoadmap(reply roadmapReply, skill string) ([]RoadNode, error) {
	seen := map[string]bool{}
	newIndex := map[int]int{} // original index -> index in out
	var out []RoadNode

	for i, n := range reply.Nodes {
		name := strings.TrimSpace(n.Name)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true

		parent := -1
		if p, ok := newIndex[n.Parent]; ok {
			parent = p
		}
		newIndex[i] = len(out)
		out = append(out, RoadNode{
			Parent:        parent,
			Name:          name,
			Description:   strings.TrimSpace(n.Description),
			RequiredLevel: clamp(n.RequiredLevel, 1, 10),
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("model returned no usable nodes for %s", skill)
	}
	return out, nil
}

// goalClause tells the model what the person is aiming for, if anything.
func goalClause(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	return fmt.Sprintf("Their goal: %s. Shape the advanced nodes toward this goal.\n",
		truncateRunes(goal, MaxGoalLength))
}

// MaxGoalLength caps the goal sentence sent to the model.
const MaxGoalLength = 160

// ChallengeDraft is one generated daily challenge, before it is saved.
type ChallengeDraft struct {
	Skill       string
	Node        string
	Title       string
	Description string
	Difficulty  int
	Minutes     int
	XP          int
}

const challengeSystem = `You design small daily challenges for a personal RPG planner.
Each challenge is concrete and can be done in one sitting today. Vary the skills.
Every challenge needs a description of one or two short sentences, under 160 characters,
that says exactly what to do and how the person will know it is finished.`

const challengeSchemaJSON = `{
  "type": "object",
  "properties": {
    "challenges": {
      "type": "array",
      "minItems": 1,
      "maxItems": 5,
      "items": {
        "type": "object",
        "properties": {
          "skill":      {"type": "string"},
          "node":       {"type": "string"},
          "title":       {"type": "string"},
          "description": {"type": "string"},
          "difficulty":  {"type": "integer", "minimum": 1, "maximum": 5},
          "minutes":     {"type": "integer", "minimum": 5, "maximum": 120}
        },
        "required": ["skill", "node", "title", "description", "difficulty", "minutes"]
      }
    }
  },
  "required": ["challenges"]
}`

// avoidClause asks the model not to repeat titles already used today.
func avoidClause(avoid []string) string {
	if len(avoid) == 0 {
		return ""
	}
	return "\nDo not repeat these challenges from earlier today: " + strings.Join(avoid, "; ") + "."
}

type challengeReply struct {
	Challenges []struct {
		Skill       string `json:"skill"`
		Node        string `json:"node"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Difficulty  int    `json:"difficulty"`
		Minutes     int    `json:"minutes"`
	} `json:"challenges"`
}

// BuildChallenges asks the model for count daily challenges spread over the
// given skills. avoid lists challenge titles already set today, so a new set
// does not repeat them. Challenges that name an unknown skill are dropped. An
// unknown node is cleared, so the challenge still counts toward the skill.
func BuildChallenges(ctx context.Context, g Generator, skills []SkillOutline, count int, avoid []string) ([]ChallengeDraft, error) {
	if len(skills) == 0 {
		return nil, errors.New("no skills to build challenges for")
	}
	count = clamp(count, 1, 5)

	prompt := fmt.Sprintf(
		"EXISTING SKILLS:\n%s\nCreate %d daily challenges of 5 to 45 minutes each. "+
			"Pick skills from the list above by exact name. Set node to one of that skill's "+
			"nodes when it fits, otherwise leave it empty.%s",
		formatOutlines(skills), count, avoidClause(avoid),
	)

	raw, err := g.GenerateJSON(ctx, challengeSystem, prompt, json.RawMessage(challengeSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("build challenges: %w", err)
	}

	var reply challengeReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return nil, fmt.Errorf("parse challenges: %w", err)
	}

	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}

	var out []ChallengeDraft
	for _, c := range reply.Challenges {
		title := strings.TrimSpace(c.Title)
		skill := normalize(c.Skill, names)
		// With one skill there is only one place a challenge can go, so an
		// unmatched name is not worth dropping.
		if !contains(names, skill) && len(names) == 1 {
			skill = names[0]
		}
		if title == "" || !contains(names, skill) {
			continue
		}

		var nodes []string
		for _, s := range skills {
			if s.Name == skill {
				nodes = s.Nodes
			}
		}
		node := normalize(c.Node, nodes)
		if !contains(nodes, node) {
			node = ""
		}

		diff := clamp(c.Difficulty, minDiff, maxDiff)
		mins := clamp(c.Minutes, minMinutes, maxMinutes)
		out = append(out, ChallengeDraft{
			Skill:       skill,
			Node:        node,
			Title:       title,
			Description: strings.TrimSpace(c.Description),
			Difficulty:  diff,
			Minutes:     mins,
			XP:          Award(Activity{Difficulty: diff, Minutes: mins}),
		})
	}

	if len(out) == 0 {
		return nil, errors.New("model returned no usable challenges")
	}
	return out, nil
}

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
	category := reply.Category
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
