package rpg

import "math"

// Categories are the fixed life areas a skill belongs to. The model must pick
// one of these, so the character screen always groups skills the same way.
var Categories = []string{"Mind", "Body", "Craft", "Social", "Life"}

// Limits applied to the model's estimates before they are used.
const (
	minMinutes = 5
	maxMinutes = 240
	minDiff    = 1
	maxDiff    = 5
)

// difficultyMultiplier scales the base XP. Index 0 is unused so that the
// difficulty value can index the slice directly.
var difficultyMultiplier = [maxDiff + 1]float64{0, 1.0, 1.25, 1.5, 2.0, 2.5}

// Award returns the XP for an activity. Every 5 minutes of effort is worth
// 1 base XP, capped at maxMinutes, then multiplied by the difficulty. An
// activity always earns at least 1 XP.
func Award(a Activity) int {
	minutes := clamp(a.Minutes, minMinutes, maxMinutes)
	diff := clamp(a.Difficulty, minDiff, maxDiff)

	base := float64(minutes / 5)
	xp := int(math.Round(base * difficultyMultiplier[diff]))
	return max(xp, 1)
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// ComebackMultiplier is the bonus for practising a rusty node again.
const ComebackMultiplier = 1.25

// ComebackXP is the XP for an award on a rusty node: the normal XP times
// ComebackMultiplier, rounded, and always at least one more than normal.
func ComebackXP(xp int) int {
	return max(int(math.Round(float64(xp)*ComebackMultiplier)), xp+1)
}
