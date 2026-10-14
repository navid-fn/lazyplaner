package rpg

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on skill names and on how many start skills one setup may add.
const (
	MaxSkillWords  = 3
	MaxSkillLength = 40
	MaxStartSkills = 8
)

// skillNameSymbols are the non-letter, non-digit characters allowed in a
// skill name, such as C++, C#, CI/CD, R&D or Node.js.
const skillNameSymbols = "+#.-/&'"

// ValidateSkillName reports why a skill name cannot be added, or nil if it
// can. existing lists the names already on the character, compared without
// regard to case. The error text is shown to the person, so it says what to
// change.
func ValidateSkillName(name string, existing []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("Type a skill name first.")
	}
	if utf8.RuneCountInString(name) > MaxSkillLength {
		return fmt.Errorf("Keep it under %d characters.", MaxSkillLength)
	}

	words := strings.Fields(name)
	if len(words) > MaxSkillWords {
		return fmt.Errorf("Use %d words at most. Name the skill, e.g. \"Public speaking\".", MaxSkillWords)
	}

	hasLetter := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r), r == ' ', strings.ContainsRune(skillNameSymbols, r):
		default:
			return fmt.Errorf("%q is not allowed. Use letters, numbers, spaces or %s.", r, skillNameSymbols)
		}
	}
	if !hasLetter {
		return errors.New("A skill needs at least one letter.")
	}

	for _, e := range existing {
		if strings.EqualFold(e, name) {
			return fmt.Errorf("%s is already on your character.", e)
		}
	}
	return nil
}

// NormalizeSkillName trims the name, squeezes runs of spaces, and
// capitalizes the first letter of each word ("public speaking" becomes
// "Public Speaking"). A word that already has a capital letter is kept as
// typed, so acronyms and brand spellings survive: "SQL", "CI/CD", "iOS".
func NormalizeSkillName(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		if strings.IndexFunc(w, unicode.IsUpper) >= 0 {
			continue
		}
		r, size := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + w[size:]
	}
	return strings.Join(words, " ")
}
