package rpg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// categorySystem is short on purpose: it runs on small local models for
// every skill added without an experience sentence.
const categorySystem = `You sort one skill into a category.
Mind: learning, study, languages, reading, games of thought.
Body: exercise, sport, health, sleep, diet.
Craft: making things, art, music, cooking, programming and other technical skills.
Social: people, communication, speaking, leadership.
Life: chores, admin, money, home.`

const categorySchemaJSON = `{
  "type": "object",
  "properties": {
    "category": {"type": "string", "enum": ["Mind", "Body", "Craft", "Social", "Life"]}
  },
  "required": ["category"]
}`

// CategorizeSkill returns the category a skill belongs to. A skill the
// keyword list knows gets its fixed category without asking the model, so
// it never changes between calls; otherwise the model is asked.
func CategorizeSkill(ctx context.Context, g Generator, skill string) (string, error) {
	if guess := GuessCategory(skill); guess != "" {
		return guess, nil
	}
	raw, err := g.GenerateJSON(ctx, categorySystem, "Skill: "+strings.TrimSpace(skill),
		json.RawMessage(categorySchemaJSON))
	if err == nil {
		var reply struct {
			Category string `json:"category"`
		}
		if err = json.Unmarshal([]byte(raw), &reply); err == nil {
			if contains(Categories, reply.Category) {
				return reply.Category, nil
			}
			err = fmt.Errorf("unknown category %q", reply.Category)
		}
	}
	return "", fmt.Errorf("categorize %s: %w", skill, err)
}

// settleCategory is the category to keep for a skill: its fixed keyword
// category when it has one, else what the model said. Every place that asks
// a model for a category goes through it, so a skill such as Golang is
// always Craft, whichever prompt classified it.
func settleCategory(skill, fromModel string) string {
	if guess := GuessCategory(skill); guess != "" {
		return guess
	}
	return fromModel
}

// categoryWords maps words that often appear in skill names to a category.
var categoryWords = map[string][]string{
	"Body": {"running", "run", "yoga", "gym", "lifting", "fitness", "swimming", "cycling", "body",
		"workout", "boxing", "climbing", "stretching", "sleep", "diet", "health", "football", "tennis"},
	"Craft": {"go", "golang", "python", "java", "javascript", "typescript", "rust", "c", "c++", "c#",
		"kotlin", "swift", "ruby", "php", "haskell", "scala", "elixir", "sql", "html", "css", "react",
		"docker", "kubernetes", "programming", "coding", "concurrency", "backend", "frontend",
		"drawing", "painting", "cooking", "cook", "baking", "guitar", "piano", "music",
		"woodworking", "design", "photography", "writing", "sewing", "knitting", "devops", "linux"},
	"Mind": {"chess", "math", "maths", "reading", "spanish", "french", "german", "english", "japanese",
		"history", "physics", "philosophy", "study", "meditation", "memory", "language"},
	"Social": {"speaking", "public", "leadership", "networking", "negotiation", "dating", "parenting",
		"communication", "mentoring", "teaching"},
	"Life": {"budgeting", "budget", "finance", "investing", "cleaning", "gardening", "chores",
		"admin", "taxes", "saving", "household"},
}

// GuessCategory picks a category from the words in a skill name, or ""
// when none of them is known. A known word wins over any model's answer
// (see settleCategory).
func GuessCategory(skill string) string {
	for _, w := range strings.Fields(strings.ToLower(skill)) {
		for _, cat := range Categories {
			if contains(categoryWords[cat], w) {
				return cat
			}
		}
	}
	return ""
}
