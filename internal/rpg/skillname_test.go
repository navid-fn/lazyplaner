package rpg

import (
	"strings"
	"testing"
)

func TestValidateSkillName(t *testing.T) {
	existing := []string{"Go", "Running"}
	valid := []string{"Software engineering", "Go", "C++", "CI/CD", "R&D", "Node.js", "Public speaking", "Ünïcödé", "3D modelling"}
	for _, name := range valid {
		if err := ValidateSkillName(name, nil); err != nil {
			t.Errorf("%q should be valid, got %v", name, err)
		}
	}

	cases := map[string]struct {
		name string
		want string // substring of the error
	}{
		"empty":       {"   ", "Type a skill name"},
		"too long":    {strings.Repeat("a", 41), "under 40"},
		"too many":    {"speak better in meetings now", "3 words at most"},
		"bad char":    {"Go!", `'!' is not allowed`},
		"emoji":       {"Drawing 🎨", "not allowed"},
		"no letters":  {"123", "at least one letter"},
		"duplicate":   {"go", "Go is already"},
		"duplicate 2": {"  RUNNING  ", "Running is already"},
	}
	for label, c := range cases {
		err := ValidateSkillName(c.name, existing)
		if err == nil {
			t.Errorf("%s: %q should be rejected", label, c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not contain %q", label, err, c.want)
		}
	}
}
