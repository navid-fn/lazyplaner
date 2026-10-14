package rpg

import (
	"math"
	"time"
)

// Status is where a skill-tree node stands for the character right now.
type Status int

const (
	Locked   Status = iota // requirements not met yet
	Unlocked               // open to work on
	Done                   // progress reached the node's XP cost
	Rusty                  // open, but neglected; practising it earns a comeback bonus
)

// nodeXPPerLevel sets the least XP a node costs, per required level.
const nodeXPPerLevel = 40

// nodeLevelShare is the least share of a level's climb (the XP from its
// start to the next level) that one node at that level costs. A fresh tree
// opens two nodes at a level, so together they cover 120% of the climb and
// finishing them always reaches the next level.
const nodeLevelShare = 0.6

// RustAfter is how long an active skill can go without any XP before its
// frontier node turns rusty.
const RustAfter = 14 * 24 * time.Hour

// NodeXPNeeded returns the XP needed to finish a node that requires level
// required: 40 per level, but never less than nodeLevelShare of the climb
// from that level to the next, rounded up. A level-2 node costs 110 XP, as
// the climb from Lv 2 to Lv 3 is 183 XP.
func NodeXPNeeded(required int) int {
	r := max(required, 1)
	climb := TotalXPForLevel(r+1) - TotalXPForLevel(r)
	share := int(math.Ceil(float64(climb) * nodeLevelShare))
	return max(nodeXPPerLevel*r, share)
}

// StartProgress is the progress a node starts with when its skill begins at
// startLevel. Nodes below the start level are skills the person already has,
// so they start done. The rest start empty: those at the start level are
// open, and those above it are locked until the skill levels up.
func StartProgress(required, startLevel, xpNeeded int) int {
	if required < startLevel {
		return xpNeeded
	}
	return 0
}

// TreeNode is the part of a skill-tree node that decides its status.
type TreeNode struct {
	ID       int64
	ParentID int64 // 0 means the node hangs directly off the skill
	Required int
	Progress int
	XPNeeded int
	Rusty    bool // marked rusty and not practised since
}

// NodeStatuses works out the status of every node for a skill at skillLevel.
//
// A node is Locked when the skill level is below its requirement or, for a
// node with a parent, when that parent is not Done yet: a branch opens one
// node at a time, so a child waits for its parent to be finished. Otherwise it is Done when its progress reached its cost,
// Rusty when it has been marked rusty, and Unlocked if neither. nodes must
// be ordered so that a parent comes before its children, which is the order
// rows are inserted in.
//
// So the person can never be stuck, there is one exception: when no node is
// open, the next tier (the locked nodes with the lowest requirement whose
// parent is done) opens even though the skill is short of its level. XP
// also comes from plans and directives that hit no node, so a skill can
// finish every open node before it reaches the next level.
func NodeStatuses(nodes []TreeNode, skillLevel int) map[int64]Status {
	statuses := strictStatuses(nodes, skillLevel)
	for _, s := range statuses {
		if s == Unlocked || s == Rusty {
			return statuses
		}
	}

	next := 0
	for _, n := range nodes {
		if statuses[n.ID] == Locked && parentDone(n, statuses) && (next == 0 || n.Required < next) {
			next = n.Required
		}
	}
	if next == 0 {
		return statuses // everything is done, or nothing can open
	}
	for _, n := range nodes {
		if statuses[n.ID] == Locked && parentDone(n, statuses) && n.Required == next {
			switch {
			case n.Progress >= n.XPNeeded:
				statuses[n.ID] = Done
			case n.Rusty:
				statuses[n.ID] = Rusty
			default:
				statuses[n.ID] = Unlocked
			}
		}
	}
	return statuses
}

// parentDone reports whether n hangs off the skill or off a done node.
func parentDone(n TreeNode, statuses map[int64]Status) bool {
	return n.ParentID == 0 || statuses[n.ParentID] == Done
}

// strictStatuses applies the level and parent rules with no exception.
func strictStatuses(nodes []TreeNode, skillLevel int) map[int64]Status {
	statuses := make(map[int64]Status, len(nodes))
	for _, n := range nodes {
		parentDone := true
		if n.ParentID != 0 {
			// A missing parent reads as Locked, the zero value.
			parentDone = statuses[n.ParentID] == Done
		}

		switch {
		case !parentDone, skillLevel < n.Required:
			statuses[n.ID] = Locked
		case n.Progress >= n.XPNeeded:
			statuses[n.ID] = Done
		case n.Rusty:
			statuses[n.ID] = Rusty
		default:
			statuses[n.ID] = Unlocked
		}
	}
	return statuses
}

// FrontierNode is the open node with the highest requirement: the furthest
// point the person has reached in the skill. Ties go to the newest node. ok
// is false when no node is open.
func FrontierNode(nodes []TreeNode, skillLevel int) (id int64, ok bool) {
	statuses := NodeStatuses(nodes, skillLevel)
	best := -1
	for i, n := range nodes {
		st := statuses[n.ID]
		if st != Unlocked && st != Rusty {
			continue
		}
		if best < 0 || n.Required > nodes[best].Required ||
			(n.Required == nodes[best].Required && n.ID > nodes[best].ID) {
			best = i
		}
	}
	if best < 0 {
		return 0, false
	}
	return nodes[best].ID, true
}

// RustTarget returns the node that should turn rusty for a skill, if any.
// Only an active skill rusts, and only once it has gone RustAfter without
// practice. A skill never practised (lastPracticed is zero) does not rust,
// since there is nothing to measure from. The node is the skill's frontier.
func RustTarget(nodes []TreeNode, skillLevel int, active bool, lastPracticed, now time.Time) (int64, bool) {
	if !active || lastPracticed.IsZero() || now.Sub(lastPracticed) < RustAfter {
		return 0, false
	}
	return FrontierNode(nodes, skillLevel)
}
