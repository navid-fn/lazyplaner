package game

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// EraState is where a skill's tree stands in its eras.
type EraState struct {
	Tree     db.SkillTree
	Level    int      // the skill's level
	Latest   int      // the latest era, 0 for a skill with no nodes
	Capstone *db.Node // the latest era's capstone, if it has one
	Done     int      // nodes done across all eras
	Total    int      // nodes across all eras
	Statuses map[int64]rpg.Status
	// CanExpand is true when the latest era's capstone is done, or at most
	// one of its nodes is left (see rpg.CanExpand).
	CanExpand bool
}

// Eras reads the era state of one skill's tree.
func Eras(t db.SkillTree) EraState {
	st := EraState{Tree: t, Level: rpg.ProgressFor(t.Skill.TotalXP).Level, Total: len(t.Nodes)}
	st.Statuses = rpg.NodeStatuses(TreeNodes(t.Nodes), st.Level)
	for _, n := range t.Nodes {
		st.Latest = max(st.Latest, n.Era)
		if st.Statuses[n.ID] == rpg.Done {
			st.Done++
		}
	}
	var latest []int64
	for i, n := range t.Nodes {
		if n.Era != st.Latest {
			continue
		}
		latest = append(latest, n.ID)
		if n.IsCapstone {
			st.Capstone = &t.Nodes[i]
		}
	}
	var capID int64
	if st.Capstone != nil {
		capID = st.Capstone.ID
	}
	st.CanExpand = st.Capstone != nil && rpg.CanExpand(st.Statuses, latest, capID)
	return st
}

// NextBase is the level the next era's foundations require.
func (s EraState) NextBase() int {
	if s.Capstone == nil {
		return s.Level
	}
	return rpg.EraBaseLevel(s.Level, s.Capstone.RequiredLevel)
}

// NodeNames are the names of every node in the tree, oldest first.
func (s EraState) NodeNames() []string {
	names := make([]string, 0, len(s.Tree.Nodes))
	for _, n := range s.Tree.Nodes {
		names = append(names, n.Name)
	}
	return names
}

// findTree returns the tree of the skill named name, ignoring case.
func findTree(ctx context.Context, store *db.Store, name string) (db.SkillTree, error) {
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return db.SkillTree{}, err
	}
	for _, t := range trees {
		if strings.EqualFold(t.Skill.Name, strings.TrimSpace(name)) {
			return t, nil
		}
	}
	return db.SkillTree{}, fmt.Errorf("no skill named %q; lazyplaner status lists them", name)
}

// readyToExpand reads a skill's era state and checks it can take its next
// era.
func readyToExpand(ctx context.Context, store *db.Store, skill string) (EraState, error) {
	t, err := findTree(ctx, store, skill)
	if err != nil {
		return EraState{}, err
	}
	st := Eras(t)
	switch {
	case st.Latest == 0:
		return st, fmt.Errorf("%s has no tree yet; build one with apply-blueprint first", t.Skill.Name)
	case st.Capstone == nil:
		return st, fmt.Errorf("era %d of %s has no capstone to grow from", st.Latest, t.Skill.Name)
	case !st.CanExpand:
		left := 0
		for _, n := range t.Nodes {
			if n.Era == st.Latest && st.Statuses[n.ID] != rpg.Done {
				left++
			}
		}
		return st, fmt.Errorf("era %d of %s still has %d nodes to finish; expand once its capstone %q is done or one node is left",
			st.Latest, t.Skill.Name, left, st.Capstone.Name)
	}
	return st, nil
}

// Expansion is an external agent's next era for a skill: eight new nodes
// with their curated tasks, an optional title, and an optional master plan
// for the era.
type Expansion struct {
	EraTitle   string             `json:"era_title,omitempty"`
	MasterPlan *ExpansionPlan     `json:"master_plan,omitempty"`
	Nodes      []rpg.ExternalNode `json:"nodes"`
}

// ExpansionPlan is the master plan of an era, saved as a new plan.
type ExpansionPlan struct {
	Subject      string `json:"subject"`
	MarkdownBody string `json:"markdown_body"`
}

// maxEraTitle caps an era's title.
const maxEraTitle = 60

// ExpandResult is what an expansion saved.
type ExpandResult struct {
	Skill db.Skill
	Era   int
	Title string
	Plan  db.Plan // zero when no master plan was given
}

// ExpandTree appends the next era to a skill's tree: eight new nodes
// wired like a fresh tree, whose two foundations hang off the capstone of
// the latest era. Nothing already in the tree changes. The skill must be
// ready (see EraState.CanExpand).
func ExpandTree(ctx context.Context, store *db.Store, skill string, e Expansion) (ExpandResult, error) {
	title := strings.TrimSpace(e.EraTitle)
	if len([]rune(title)) > maxEraTitle {
		return ExpandResult{}, fmt.Errorf("era_title is over %d characters", maxEraTitle)
	}
	var subject, body string
	if e.MasterPlan != nil {
		subject = strings.TrimSpace(e.MasterPlan.Subject)
		body = strings.TrimSpace(e.MasterPlan.MarkdownBody)
		switch {
		case subject == "" || body == "":
			return ExpandResult{}, errors.New("master_plan needs a subject and a markdown_body, or leave it out")
		case len([]rune(subject)) > maxPlanSubject:
			return ExpandResult{}, fmt.Errorf("master_plan.subject is over %d characters", maxPlanSubject)
		case len([]rune(body)) > maxPlanBody:
			return ExpandResult{}, fmt.Errorf("master_plan.markdown_body is over %d characters", maxPlanBody)
		}
	}
	st, err := readyToExpand(ctx, store, skill)
	if err != nil {
		return ExpandResult{}, err
	}
	shaped, err := rpg.ShapeExternalEra(e.Nodes, st.NextBase(), st.NodeNames())
	if err != nil {
		return ExpandResult{}, err
	}
	return saveEra(ctx, store, st, shaped, title, subject, body)
}

// GenerateEra asks the local model for a skill's next era and saves it,
// for a person who expands from the TUI instead of through an agent. goal
// steers the new nodes, and may be empty.
func GenerateEra(ctx context.Context, g rpg.Generator, store *db.Store, skill, goal string) (ExpandResult, error) {
	if g == nil {
		return ExpandResult{}, errors.New("planner is not configured")
	}
	st, err := readyToExpand(ctx, store, skill)
	if err != nil {
		return ExpandResult{}, err
	}
	shaped, err := rpg.BuildEraRoadmap(ctx, g, st.Tree.Skill.Name, st.Latest+1, st.NextBase(), goal, st.NodeNames())
	if err != nil {
		return ExpandResult{}, err
	}
	return saveEra(ctx, store, st, shaped, "", "", "")
}

func saveEra(ctx context.Context, store *db.Store, st EraState, shaped []rpg.RoadNode, title, subject, body string) (ExpandResult, error) {
	era := st.Latest + 1
	plan, err := store.AppendEra(ctx, db.NewEra{
		SkillID:     st.Tree.Skill.ID,
		Era:         era,
		CapstoneID:  st.Capstone.ID,
		Title:       title,
		Nodes:       RoadmapNodes(shaped),
		Level:       st.Level,
		PlanSubject: subject,
		PlanBody:    body,
	})
	if err != nil {
		return ExpandResult{}, err
	}
	return ExpandResult{Skill: st.Tree.Skill, Era: era, Title: title, Plan: plan}, nil
}

// RomanEra writes an era number as a Roman numeral, as the TUI shows it.
func RomanEra(n int) string {
	if n <= 0 {
		return "0"
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}
