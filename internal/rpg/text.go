package rpg

import (
	"fmt"
	"strings"
)

// A directive or curated task has three pieces of text, each with a job:
//
//   - title: a short name for the drill or deliverable, 2 to 5 words, so a
//     row in the day's list shows it whole ("Ghosted Line Drills");
//   - description: the one-sentence definition of done;
//   - details: the execution guide, 3 or 4 markdown bullets on the steps,
//     the key concepts or commands, and how to check it is finished.
const (
	MinTitleWords   = 2
	MaxTitleWords   = 5
	MaxTitleRunes   = 32
	MaxSummaryRunes = 140
	MaxDetailsRunes = 900
)

// CheckTitle reports what is wrong with a title an agent wrote, or nil. The
// title should already be cleaned (see CleanDirectiveTitle).
func CheckTitle(title string) error {
	words := len(strings.Fields(title))
	switch {
	case title == "":
		return fmt.Errorf("needs a concrete title")
	case len([]rune(title)) > MaxTitleRunes:
		return fmt.Errorf("title %q is %d characters; keep it to %d: a 2-5 word name like \"Ghosted Line Drills\", not a sentence",
			title, len([]rune(title)), MaxTitleRunes)
	case words < MinTitleWords || words > MaxTitleWords:
		return fmt.Errorf("title %q is %d words; use %d to %d words naming the drill or deliverable",
			title, words, MinTitleWords, MaxTitleWords)
	}
	return nil
}

// ShortenTitle holds a model's title to the limits: at most MaxTitleWords
// words and MaxTitleRunes characters, cut between words. A model cannot be
// refused like an agent, so its title is trimmed instead.
func ShortenTitle(title string) string {
	words := strings.Fields(title)
	if len(words) > MaxTitleWords {
		words = words[:MaxTitleWords]
	}
	for len(words) > 1 && len([]rune(strings.Join(words, " "))) > MaxTitleRunes {
		words = words[:len(words)-1]
	}
	out := strings.Trim(strings.Join(words, " "), ".,!:;- ")
	if r := []rune(out); len(r) > MaxTitleRunes {
		out = string(r[:MaxTitleRunes])
	}
	return out
}

// ShortenSummary holds a model's description to MaxSummaryRunes, ending
// with … when it is cut.
func ShortenSummary(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= MaxSummaryRunes {
		return s
	}
	return strings.TrimSpace(string(r[:MaxSummaryRunes-1])) + "…"
}

// CleanDetails trims an execution guide and holds it to MaxDetailsRunes.
func CleanDetails(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > MaxDetailsRunes {
		s = strings.TrimSpace(string(r[:MaxDetailsRunes-1])) + "…"
	}
	return s
}

// checkTexts checks an agent's summary and details against their limits.
func checkTexts(desc, details string) error {
	if n := len([]rune(desc)); n > MaxSummaryRunes {
		return fmt.Errorf("description is %d characters; keep it to one sentence of at most %d", n, MaxSummaryRunes)
	}
	if n := len([]rune(details)); n > MaxDetailsRunes {
		return fmt.Errorf("details are %d characters; keep the guide to 3-4 bullets, at most %d", n, MaxDetailsRunes)
	}
	return nil
}
