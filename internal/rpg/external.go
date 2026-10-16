package rpg

import (
	"fmt"
	"strings"
)

// The functions here take content written by an external agent, such as a
// frontier model driving the CLI, in place of the local model's. The agent
// only writes names and text; the tree shape, levels and XP still come from
// the same code as everything else.

// ExternalNode is a sub-skill an agent wrote, in learning order.
type ExternalNode struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Tasks       []CuratedTask `json:"tasks,omitempty"` // none, or 2-4 in the order to do them
}

// Limits on what an agent may write.
const (
	maxNodeName        = 60
	maxNodeDescription = 300
)

// ShapeExternalTree checks an agent's sub-skills and wires them into the
// fixed tree shape for a skill at level, exactly as a generated tree is:
// two roots, then 3, 2 and the capstone. It needs exactly RoadmapSize
// distinct, named nodes.
func ShapeExternalTree(nodes []ExternalNode, level int) ([]RoadNode, error) {
	if level < 1 {
		return nil, fmt.Errorf("level must be at least 1, got %d", level)
	}
	out, err := checkExternalNodes(nodes, nil)
	if err != nil {
		return nil, err
	}
	return shapeTree(out, level), nil
}

// ShapeExternalEra checks an agent's sub-skills for the next era of a tree
// and wires them like a fresh tree, with levels climbing from base (see
// ShapeEra). taken holds the names the tree already uses, which the era
// may not repeat.
func ShapeExternalEra(nodes []ExternalNode, base int, taken []string) ([]RoadNode, error) {
	out, err := checkExternalNodes(nodes, taken)
	if err != nil {
		return nil, err
	}
	return ShapeEra(out, base), nil
}

// checkExternalNodes checks that there are exactly RoadmapSize distinct,
// named nodes, none of them named in taken, with valid tasks.
func checkExternalNodes(nodes []ExternalNode, taken []string) ([]RoadNode, error) {
	if len(nodes) != RoadmapSize {
		return nil, fmt.Errorf("a tree needs exactly %d nodes, got %d", RoadmapSize, len(nodes))
	}
	used := map[string]bool{}
	for _, name := range taken {
		used[strings.ToLower(strings.TrimSpace(name))] = true
	}
	seen := map[string]bool{}
	out := make([]RoadNode, 0, len(nodes))
	for i, n := range nodes {
		name := strings.TrimSpace(n.Name)
		desc := strings.TrimSpace(n.Description)
		switch {
		case name == "":
			return nil, fmt.Errorf("node %d has no name", i+1)
		case len([]rune(name)) > maxNodeName:
			return nil, fmt.Errorf("node %d name is over %d characters", i+1, maxNodeName)
		case len([]rune(desc)) > maxNodeDescription:
			return nil, fmt.Errorf("node %d description is over %d characters", i+1, maxNodeDescription)
		case seen[strings.ToLower(name)]:
			return nil, fmt.Errorf("node %q is listed twice", name)
		case used[strings.ToLower(name)]:
			return nil, fmt.Errorf("node %q is already in this tree; pick a new name", name)
		}
		seen[strings.ToLower(name)] = true
		tasks, err := cleanTasks(name, n.Tasks)
		if err != nil {
			return nil, err
		}
		out = append(out, RoadNode{Name: name, Description: desc, Tasks: tasks})
	}
	return out, nil
}

// tierNames are the tier names an agent may use for a directive.
var tierNames = map[string]Tier{
	"recon":     TierRecon,
	"core_op":   TierCoreOp,
	"deep_push": TierDeepPush,
	"recovery":  TierRecovery,
}

// ParseTier reads a tier name as an agent writes it: recon, core_op,
// deep_push or recovery.
func ParseTier(name string) (Tier, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.NewReplacer(" ", "_", "-", "_").Replace(key)
	if t, ok := tierNames[key]; ok {
		return t, nil
	}
	return TierNone, fmt.Errorf("unknown tier %q: use recon, core_op, deep_push or recovery", name)
}

// ExternalDirective turns an agent's directive into a draft: the title is
// cleaned like a generated one and must be short (see CheckTitle), the
// description and details are held to their limits, difficulty and minutes
// are held to the tier's bounds (0 takes the tier's lowest), and XP comes
// from Award.
func ExternalDirective(tier Tier, skill, node, title, description, details string, difficulty, minutes int) (ChallengeDraft, error) {
	spec, ok := tierSpecs[tier]
	if !ok || tier == TierOvertime || tier == TierFocus {
		return ChallengeDraft{}, fmt.Errorf("tier %v cannot be set directly", tier)
	}
	clean := CleanDirectiveTitle(title)
	if err := CheckTitle(clean); err != nil {
		return ChallengeDraft{}, err
	}
	description, details = strings.TrimSpace(description), strings.TrimSpace(details)
	if err := checkTexts(description, details); err != nil {
		return ChallengeDraft{}, err
	}
	diff := clamp(difficulty, spec.minDiff, spec.maxDiff)
	mins := clamp(minutes, spec.minMins, spec.maxMins)
	return ChallengeDraft{
		Tier:        tier,
		Skill:       skill,
		Node:        node,
		Title:       clean,
		Description: description,
		Details:     details,
		Difficulty:  diff,
		Minutes:     mins,
		XP:          Award(Activity{Difficulty: diff, Minutes: mins}),
	}, nil
}

// CheckDaySet reports whether tiers make a valid day's set: Recon, Core Op
// and Deep Push once each, with an optional second Core Op; or one or two
// Recovery directives for a light day.
func CheckDaySet(tiers []Tier) error {
	count := map[Tier]int{}
	for _, t := range tiers {
		count[t]++
	}
	if count[TierRecovery] > 0 {
		if count[TierRecovery] != len(tiers) || len(tiers) > recoveryDirectives {
			return fmt.Errorf("a recovery day is 1 or %d recovery directives and nothing else", recoveryDirectives)
		}
		return nil
	}
	if count[TierRecon] != 1 || count[TierDeepPush] != 1 || count[TierCoreOp] < 1 || count[TierCoreOp] > 2 ||
		len(tiers) != count[TierRecon]+count[TierCoreOp]+count[TierDeepPush] {
		return fmt.Errorf("a day's set is one recon, one or two core_op and one deep_push")
	}
	return nil
}
