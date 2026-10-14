// Package rpg turns finished plans into skill levels and experience.
//
// The model only classifies a plan (skill, category, difficulty, effort).
// The XP numbers come from the formulas in this package, so the same plan
// always earns the same XP and the rules stay in one place.
package rpg

import "math"

// xpScale sets how much XP the first levels need. Level 2 needs 100 XP.
const xpScale = 100.0

// TotalXPForLevel returns the cumulative XP needed to reach level n.
// Level 1 starts at 0. The curve grows as n^1.5, so early levels come fast
// and later ones take longer.
func TotalXPForLevel(n int) int {
	if n <= 1 {
		return 0
	}
	return int(math.Round(xpScale * math.Pow(float64(n-1), 1.5)))
}

// Progress describes where a total XP amount sits on the curve.
type Progress struct {
	Level    int // current level, starting at 1
	Into     int // XP earned inside the current level
	Span     int // XP needed to go from this level to the next
	NextXP   int // total XP at which the next level starts
	Fraction float64
}

// ProgressFor returns the level and the progress within it for a total XP.
func ProgressFor(total int) Progress {
	if total < 0 {
		total = 0
	}
	level := 1
	for TotalXPForLevel(level+1) <= total {
		level++
	}

	start := TotalXPForLevel(level)
	next := TotalXPForLevel(level + 1)
	span := next - start
	into := total - start

	return Progress{
		Level:    level,
		Into:     into,
		Span:     span,
		NextXP:   next,
		Fraction: float64(into) / float64(span),
	}
}

// rankTitles are the names shown for level ranges. Each entry applies from
// its level up to the next entry's level.
var rankTitles = []struct {
	from  int
	title string
}{
	{1, "Novice"},
	{5, "Apprentice"},
	{10, "Journeyman"},
	{20, "Expert"},
	{35, "Master"},
	{50, "Grandmaster"},
}

// RankTitle returns the title for a character level.
func RankTitle(level int) string {
	title := rankTitles[0].title
	for _, r := range rankTitles {
		if level >= r.from {
			title = r.title
		}
	}
	return title
}

// NextRank returns the rank after the one held at level, and the level it
// starts at. ok is false once the top rank is reached.
func NextRank(level int) (title string, from int, ok bool) {
	for _, r := range rankTitles {
		if r.from > level {
			return r.title, r.from, true
		}
	}
	return "", 0, false
}
