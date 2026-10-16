package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// NewChallenges turns directive drafts into rows to save.
func NewChallenges(drafts []rpg.ChallengeDraft) []db.NewChallenge {
	items := make([]db.NewChallenge, 0, len(drafts))
	for _, d := range drafts {
		items = append(items, db.NewChallenge{
			Tier:        d.Tier,
			SkillName:   d.Skill,
			NodeName:    d.Node,
			Title:       d.Title,
			Description: d.Description,
			Details:     d.Details,
			Difficulty:  d.Difficulty,
			Minutes:     d.Minutes,
			XP:          d.XP,
			NodeTaskID:  d.TaskID,
		})
	}
	return items
}

// TreeImport is a skill tree written by an external agent.
type TreeImport struct {
	Skill    string
	Level    int    // start level, 1-5, for a new skill; unused with Replace
	Category string // Mind, Body, Craft, Social or Life; "" guesses from the name
	Frozen   bool   // create the skill garrisoned instead of active
	// Replace swaps the tree of an existing skill. The skill keeps its XP;
	// the new tree is calibrated to its current level, not Level.
	Replace bool
	Nodes   []rpg.ExternalNode
}

// ImportTree saves an agent's tree, and any curated tasks on its nodes,
// with the same shape, levels and XP costs as a generated one. It returns
// the skill.
func ImportTree(ctx context.Context, store *db.Store, t TreeImport) (db.Skill, error) {
	tree, err := prepareTree(ctx, store, t)
	if err != nil {
		return db.Skill{}, err
	}
	if t.Replace {
		return store.ReplaceSkillNodes(ctx, tree.Name, tree.Nodes)
	}
	return store.CreateSkillTree(ctx, tree)
}

// RoadmapNodes turns shaped nodes into rows to save, with the XP cost of
// each node's required level and its curated tasks.
func RoadmapNodes(shaped []rpg.RoadNode) []db.RoadmapNode {
	nodes := make([]db.RoadmapNode, 0, len(shaped))
	for _, n := range shaped {
		var tasks []db.NewNodeTask
		for _, task := range n.Tasks {
			tasks = append(tasks, db.NewNodeTask{Title: task.Title, Description: task.Description,
				Details: task.Details, Difficulty: task.Difficulty, Minutes: task.Minutes})
		}
		nodes = append(nodes, db.RoadmapNode{
			Parent:        n.Parent,
			MoreParents:   n.MoreParents,
			Capstone:      n.Capstone,
			Name:          n.Name,
			Description:   n.Description,
			RequiredLevel: n.RequiredLevel,
			XPNeeded:      rpg.NodeXPNeeded(n.RequiredLevel),
			Tasks:         tasks,
		})
	}
	return nodes
}

// prepareTree checks an agent's tree against the skills there are and
// shapes it: a new tree starts at t.Level, a replaced one at the level the
// skill has reached.
func prepareTree(ctx context.Context, store *db.Store, t TreeImport) (db.NewSkillTree, error) {
	skills, err := store.ListSkills(ctx)
	if err != nil {
		return db.NewSkillTree{}, err
	}
	name := rpg.NormalizeSkillName(t.Skill)
	var existing *db.Skill
	var others []string
	for i, s := range skills {
		if strings.EqualFold(s.Name, name) {
			existing = &skills[i]
		} else {
			others = append(others, s.Name)
		}
	}
	switch {
	case existing != nil && !t.Replace:
		return db.NewSkillTree{}, fmt.Errorf("skill %q already exists; pass --replace to swap its tree", existing.Name)
	case existing == nil && t.Replace:
		return db.NewSkillTree{}, fmt.Errorf("skill %q does not exist, so there is no tree to replace", name)
	case existing == nil && (t.Level < 1 || t.Level > 5):
		return db.NewSkillTree{}, fmt.Errorf("level must be 1 to 5, got %d", t.Level)
	}

	level := t.Level
	if existing != nil {
		level = rpg.ProgressFor(existing.TotalXP).Level
	}
	shaped, err := rpg.ShapeExternalTree(t.Nodes, level)
	if err != nil {
		return db.NewSkillTree{}, err
	}
	nodes := RoadmapNodes(shaped)
	if existing != nil {
		return db.NewSkillTree{Name: existing.Name, Category: existing.Category, Nodes: nodes}, nil
	}

	if err := rpg.ValidateSkillName(name, others); err != nil {
		return db.NewSkillTree{}, err
	}
	category := strings.TrimSpace(t.Category)
	if category == "" {
		category = rpg.GuessCategory(name)
	}
	if !contains(rpg.Categories, category) {
		return db.NewSkillTree{}, fmt.Errorf("category must be one of %s", strings.Join(rpg.Categories, ", "))
	}
	return db.NewSkillTree{Name: name, Category: category, StartLevel: t.Level, Garrisoned: t.Frozen, Nodes: nodes}, nil
}

// Blueprint is an external agent's one-shot setup for a goal: a skill with
// its 8-node tree, curated tasks on the nodes, and a master plan.
type Blueprint struct {
	SkillName  string `json:"skill_name"`
	StartLevel int    `json:"start_level"`
	Category   string `json:"category"`
	Goal       string `json:"goal"`
	Frozen     bool   `json:"frozen,omitempty"`
	MasterPlan struct {
		Subject      string `json:"subject"`
		MarkdownBody string `json:"markdown_body"`
	} `json:"master_plan"`
	Nodes []rpg.ExternalNode `json:"nodes"`
}

// Limits on a blueprint's master plan.
const (
	maxPlanSubject = 120
	maxPlanBody    = 20000
)

// ApplyBlueprint checks a blueprint and saves all of it in one
// transaction: the skill and tree (replacing the tree of an existing skill
// when replace is set), every curated task, and the master plan. If any part
// is wrong, nothing is saved.
func ApplyBlueprint(ctx context.Context, store *db.Store, b Blueprint, replace bool) (db.Skill, db.Plan, error) {
	subject := strings.TrimSpace(b.MasterPlan.Subject)
	body := strings.TrimSpace(b.MasterPlan.MarkdownBody)
	switch {
	case subject == "" || body == "":
		return db.Skill{}, db.Plan{}, errors.New("master_plan needs a subject and a markdown_body")
	case len([]rune(subject)) > maxPlanSubject:
		return db.Skill{}, db.Plan{}, fmt.Errorf("master_plan.subject is over %d characters", maxPlanSubject)
	case len([]rune(body)) > maxPlanBody:
		return db.Skill{}, db.Plan{}, fmt.Errorf("master_plan.markdown_body is over %d characters", maxPlanBody)
	}
	tree, err := prepareTree(ctx, store, TreeImport{Skill: b.SkillName, Level: b.StartLevel,
		Category: b.Category, Frozen: b.Frozen, Replace: replace, Nodes: b.Nodes})
	if err != nil {
		return db.Skill{}, db.Plan{}, err
	}
	return store.ApplyBlueprint(ctx, db.Blueprint{Tree: tree, Replace: replace, PlanSubject: subject, PlanBody: body})
}

// DirectiveImport is one directive written by an external agent.
type DirectiveImport struct {
	Tier        string `json:"tier"` // recon, core_op, deep_push or recovery
	NodeID      int64  `json:"node_id"`
	Title       string `json:"title"`                // 2-5 words, at most 32 characters
	Description string `json:"description"`          // one-sentence definition of done
	Details     string `json:"details,omitempty"`    // markdown execution guide
	Difficulty  int    `json:"difficulty,omitempty"` // held to the tier's range; 0 takes its lowest
	Minutes     int    `json:"minutes,omitempty"`    // held to the tier's range; 0 takes its lowest
}

// ImportDirectives saves an agent's set for day. Every directive must
// target an open or rusty node of an active skill, the tiers must make a
// valid set (see rpg.CheckDaySet), and the XP comes from the tier and
// rpg.Award. With replace, an unstarted set for the day is swapped out.
func ImportDirectives(ctx context.Context, store *db.Store, day string, items []DirectiveImport, replace bool) ([]db.Challenge, error) {
	if len(items) == 0 {
		return nil, errors.New("no directives given")
	}
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return nil, err
	}
	type target struct {
		skill  db.Skill
		node   db.Node
		status rpg.Status
	}
	targets := map[int64]target{}
	for _, t := range trees {
		statuses := rpg.NodeStatuses(TreeNodes(t.Nodes), rpg.ProgressFor(t.Skill.TotalXP).Level)
		for _, n := range t.Nodes {
			targets[n.ID] = target{t.Skill, n, statuses[n.ID]}
		}
	}

	tiers := make([]rpg.Tier, 0, len(items))
	drafts := make([]rpg.ChallengeDraft, 0, len(items))
	for i, it := range items {
		tier, err := rpg.ParseTier(it.Tier)
		if err != nil {
			return nil, fmt.Errorf("directive %d: %w", i+1, err)
		}
		tg, ok := targets[it.NodeID]
		switch {
		case !ok:
			return nil, fmt.Errorf("directive %d: node_id %d does not exist", i+1, it.NodeID)
		case !tg.skill.IsActive:
			return nil, fmt.Errorf("directive %d: %s is frozen; only active skills get directives", i+1, tg.skill.Name)
		case tg.status != rpg.Unlocked && tg.status != rpg.Rusty:
			return nil, fmt.Errorf("directive %d: node %d (%s) is %s; pick an OPEN or RUSTY node", i+1, it.NodeID, tg.node.Name, tg.status)
		}
		d, err := rpg.ExternalDirective(tier, tg.skill.Name, tg.node.Name, it.Title, it.Description, it.Details, it.Difficulty, it.Minutes)
		if err != nil {
			return nil, fmt.Errorf("directive %d: %w", i+1, err)
		}
		tiers = append(tiers, tier)
		drafts = append(drafts, d)
	}
	if err := rpg.CheckDaySet(tiers); err != nil {
		return nil, err
	}

	save := store.SaveChallenges
	if replace {
		save = store.ReplaceDaySet
	}
	if err := save(ctx, day, NewChallenges(drafts)); err != nil {
		if errors.Is(err, db.ErrDaySet) {
			return nil, fmt.Errorf("%w; pass --replace to swap an unstarted set", err)
		}
		return nil, err
	}
	return store.ListChallenges(ctx, day)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
