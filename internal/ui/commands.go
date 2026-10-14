package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/llm"
	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// roadEventLimit caps how many events are loaded for the road.
const roadEventLimit = 200

func loadPlansCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		plans, err := store.ListPlans(ctx)
		return plansLoadedMsg{plans: plans, err: err}
	}
}

func loadModelsCmd(client *llm.Client) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return modelsLoadedMsg{err: errors.New("planner is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		models, err := client.ListModels(ctx)
		return modelsLoadedMsg{models: models, err: err}
	}
}

func generatePlanCmd(client *llm.Client, store *db.Store, subject, detail string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		description, err := client.GenerateDescription(ctx, subject, detail)
		if err != nil {
			return planSavedMsg{err: err}
		}
		plan, err := store.CreatePlan(ctx, subject, detail, description)
		if err != nil {
			return planSavedMsg{err: err}
		}

		// XP is a bonus on top of the plan. If the model can't classify it,
		// the plan is still saved and the status line says why.
		xp, xpErr := awardPlanXP(client, store, plan)
		return planSavedMsg{plan: plan, xp: xp, xpErr: xpErr}
	}
}

// awardPlanXP asks the model which skill and node the plan belongs to and
// records the XP.
func awardPlanXP(client *llm.Client, store *db.Store, plan db.Plan) (*db.XPEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return nil, err
	}
	activity, err := rpg.Classify(ctx, client, plan.Subject, plan.Detail, outlinesFrom(trees))
	if err != nil {
		return nil, err
	}

	// A node created from this plan starts at the skill's current level, so
	// it is open straight away.
	level := 1
	for _, t := range trees {
		if t.Skill.Name == activity.Skill {
			level = rpg.ProgressFor(t.Skill.TotalXP).Level
		}
	}

	event, err := store.RecordXP(ctx, db.XPAward{
		PlanID:       plan.ID,
		Activity:     plan.Subject,
		SkillName:    activity.Skill,
		Category:     activity.Category,
		Difficulty:   activity.Difficulty,
		Minutes:      activity.Minutes,
		XP:           rpg.Award(activity),
		NodeName:     activity.Node,
		ParentNode:   activity.Parent,
		NodeRequired: level,
		NodeXPNeeded: rpg.NodeXPNeeded(level),
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// outlinesFrom converts stored trees into the form the model sees.
func outlinesFrom(trees []db.SkillTree) []rpg.SkillOutline {
	out := make([]rpg.SkillOutline, 0, len(trees))
	for _, t := range trees {
		o := rpg.SkillOutline{
			Name:  t.Skill.Name,
			Level: rpg.ProgressFor(t.Skill.TotalXP).Level,
		}
		for _, n := range t.Nodes {
			o.Nodes = append(o.Nodes, n.Name)
		}
		out = append(out, o)
	}
	return out
}

func loadRoadCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Rust is checked whenever character data loads, which includes
		// start-up, so a skill left alone for two weeks shows it next launch.
		if err := applyRust(ctx, store, time.Now()); err != nil {
			return roadLoadedMsg{err: err}
		}
		skills, err := store.ListSkills(ctx)
		if err != nil {
			return roadLoadedMsg{err: err}
		}
		nodes, err := store.ListNodes(ctx)
		if err != nil {
			return roadLoadedMsg{err: err}
		}
		events, err := store.ListXPEvents(ctx, roadEventLimit)
		if err != nil {
			return roadLoadedMsg{err: err}
		}
		stats, err := store.ChallengeStats(ctx, db.Today())
		return roadLoadedMsg{skills: skills, nodes: nodes, events: events, stats: stats, err: err}
	}
}

// auditSkillCmd asks the skill auditor to read one experience sentence.
func auditSkillCmd(client *llm.Client, name, experience string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return auditDoneMsg{name: name, err: errors.New("planner is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		audit, err := rpg.AuditSkill(ctx, client, name, experience)
		return auditDoneMsg{name: name, audit: audit, err: err}
	}
}

// applyRust marks the frontier node of every active skill that has gone
// rpg.RustAfter without practice. A node that is already rusty is left as
// it is, so this is safe to run on every load.
func applyRust(ctx context.Context, store *db.Store, now time.Time) error {
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return err
	}
	for _, t := range trees {
		level := rpg.ProgressFor(t.Skill.TotalXP).Level
		id, ok := rpg.RustTarget(toTreeNodes(t.Nodes), level, t.Skill.IsActive, t.Skill.LastPracticed, now)
		if !ok {
			continue
		}
		for _, n := range t.Nodes {
			if n.ID == id && !n.IsRusty {
				if err := store.SetNodeRusty(ctx, id, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// buildSkillTreeCmd asks the model for the tree of the pending skill at
// index and saves it. The setup screen starts the next one when this one
// is done, so it can show which skill is being designed.
func buildSkillTreeCmd(client *llm.Client, store *db.Store, pending []pendingSkill, index int) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return treeBuiltMsg{index: index, err: errors.New("planner is not configured")}
		}
		p := pending[index]

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		category, nodes, err := rpg.BuildRoadmap(ctx, client, p.Name, p.Level, p.Goal)
		cancel()
		if err != nil {
			return treeBuiltMsg{index: index, err: err}
		}

		roadmap := make([]db.RoadmapNode, 0, len(nodes))
		for _, n := range nodes {
			roadmap = append(roadmap, db.RoadmapNode{
				Parent:        n.Parent,
				Name:          n.Name,
				Description:   n.Description,
				RequiredLevel: n.RequiredLevel,
				XPNeeded:      rpg.NodeXPNeeded(n.RequiredLevel),
			})
		}

		// The category on the setup card (from the auditor or the
		// classifier) wins over the tree's; without one, the tree's is used.
		if p.Category != "" {
			category = p.Category
		}
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer saveCancel()
		_, err = store.CreateSkillTree(saveCtx, db.NewSkillTree{
			Name:       p.Name,
			Category:   category,
			StartLevel: p.Level,
			Rusty:      p.Rusty,
			Garrisoned: p.Frozen,
			Nodes:      roadmap,
		})
		if err != nil {
			err = fmt.Errorf("save %s: %w", p.Name, err)
		}
		return treeBuiltMsg{index: index, err: err}
	}
}

// categorizeSkillCmd asks the classifier for a skill's category. Without a
// model it falls back to the keyword guess.
func categorizeSkillCmd(client *llm.Client, name string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			if cat := rpg.GuessCategory(name); cat != "" {
				return categoryDoneMsg{name: name, category: cat}
			}
			return categoryDoneMsg{name: name, err: errors.New("planner is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cat, err := rpg.CategorizeSkill(ctx, client, name)
		return categoryDoneMsg{name: name, category: cat, err: err}
	}
}

// loadDailyCmd reads today's directives and the challenge stats. It never
// asks the model for anything; dailyJobCmd does that.
func loadDailyCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, stats, err := readDaily(ctx, store)
		return challengesLoadedMsg{challenges: list, stats: stats, err: err}
	}
}

func readDaily(ctx context.Context, store *db.Store) ([]db.Challenge, db.ChallengeStats, error) {
	day := db.Today()
	list, err := store.ListChallenges(ctx, day)
	if err != nil {
		return nil, db.ChallengeStats{}, err
	}
	stats, err := store.ChallengeStats(ctx, day)
	return list, stats, err
}

// dailyJob is what dailyJobCmd asks the model for.
type dailyJob int

const (
	dailyGenerate dailyJob = iota // today's set, when today has none
	dailyReroll                   // a fresh directive in place of one open one
	dailyBonus                    // the day's one bonus, once the set is done
	dailyNode                     // a focus directive for one tree node
)

// hasSet reports whether today's set has been generated. Focus directives
// made from the skill tree are not a set.
func hasSet(list []db.Challenge) bool {
	for _, c := range list {
		if c.Tier != rpg.TierFocus {
			return true
		}
	}
	return false
}

// allChallengesDone reports whether every directive in the list is done. An empty list
// is not done: there is nothing to finish yet.
func allChallengesDone(list []db.Challenge) bool {
	for _, c := range list {
		if !c.Done {
			return false
		}
	}
	return len(list) > 0
}

// bonusUsed reports whether today's bonus directive has been handed out.
func bonusUsed(list []db.Challenge) bool {
	for _, c := range list {
		if c.Tier == rpg.TierOvertime {
			return true
		}
	}
	return false
}

// dailyJobCmd runs one model job on today's directives and returns the
// updated list. target is the directive to reroll, or for dailyNode names
// the skill and node; it is ignored otherwise.
func dailyJobCmd(client *llm.Client, store *db.Store, job dailyJob, target db.Challenge) tea.Cmd {
	return func() tea.Msg {
		note, err := runDailyJob(client, store, job, target)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, stats, loadErr := readDaily(ctx, store)
		if err == nil {
			err = loadErr
		}
		return challengesLoadedMsg{challenges: list, stats: stats, note: note, err: err}
	}
}

func runDailyJob(client *llm.Client, store *db.Store, job dailyJob, target db.Challenge) (string, error) {
	if client == nil {
		return "", errors.New("planner is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	day := db.Today()
	list, err := store.ListChallenges(ctx, day)
	if err != nil {
		return "", err
	}
	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return "", err
	}
	outlines := directiveOutlines(trees)
	avoid := make([]string, 0, len(list))
	for _, c := range list {
		avoid = append(avoid, c.Title)
	}

	switch job {
	case dailyGenerate:
		if hasSet(list) {
			return "", nil // already set, maybe from another window
		}
		if len(outlines) == 0 {
			return "", errors.New("no active skills; activate one on the character screen first")
		}
		drafts, err := rpg.BuildDirectives(ctx, client, rpg.DailySlots(outlines), nil)
		if err != nil {
			return "", err
		}
		if err := store.SaveChallenges(ctx, day, newChallenges(drafts)); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d directives for today.", len(drafts)), nil

	case dailyReroll:
		drafts, err := rpg.BuildDirectives(ctx, client, []rpg.DirectiveSlot{rerollSlot(target, trees)}, avoid)
		if err != nil {
			return "", err
		}
		if err := store.RerollChallenge(ctx, target.ID, newChallenges(drafts)[0]); err != nil {
			return "", err
		}
		return "Rerolled: " + drafts[0].Title, nil

	case dailyBonus:
		if !allChallengesDone(list) {
			return "", db.ErrSetUnfinished
		}
		if bonusUsed(list) {
			return "", db.ErrBonusUsed
		}
		if len(outlines) == 0 {
			return "", errors.New("no active skills for a bonus directive")
		}
		slot := bonusSlot(outlines, list)
		drafts, err := rpg.BuildDirectives(ctx, client, []rpg.DirectiveSlot{slot}, avoid)
		if err != nil {
			return "", err
		}
		if err := store.AddBonusChallenge(ctx, day, newChallenges(drafts)[0]); err != nil {
			return "", err
		}
		return "★ Bonus Overtime Directive: " + drafts[0].Title, nil

	case dailyNode:
		slot, err := nodeSlot(target, trees)
		if err != nil {
			return "", err
		}
		drafts, err := rpg.BuildDirectives(ctx, client, []rpg.DirectiveSlot{slot}, avoid)
		if err != nil {
			return "", err
		}
		if err := store.AddNodeChallenge(ctx, day, newChallenges(drafts)[0]); err != nil {
			return "", err
		}
		return "🎯 New directive for " + target.NodeName + ": " + drafts[0].Title, nil
	}
	return "", nil
}

func newChallenges(drafts []rpg.ChallengeDraft) []db.NewChallenge {
	items := make([]db.NewChallenge, 0, len(drafts))
	for _, d := range drafts {
		items = append(items, db.NewChallenge{
			Tier:        d.Tier,
			SkillName:   d.Skill,
			NodeName:    d.Node,
			Title:       d.Title,
			Description: d.Description,
			Difficulty:  d.Difficulty,
			Minutes:     d.Minutes,
			XP:          d.XP,
		})
	}
	return items
}

// directiveOutlines is what the model may build directives from: only
// active skills, and only their open or rusty nodes. Locked and done nodes
// are left out, so a directive can never target a node the person cannot
// work on yet.
func directiveOutlines(trees []db.SkillTree) []rpg.SkillOutline {
	var out []rpg.SkillOutline
	for _, t := range trees {
		if !t.Skill.IsActive {
			continue
		}
		out = append(out, rpg.SkillOutline{
			Name:  t.Skill.Name,
			Level: rpg.ProgressFor(t.Skill.TotalXP).Level,
			Nodes: openNodeNames(t),
		})
	}
	return out
}

// openNodeNames lists a tree's open and rusty nodes.
func openNodeNames(t db.SkillTree) []string {
	statuses := rpg.NodeStatuses(toTreeNodes(t.Nodes), rpg.ProgressFor(t.Skill.TotalXP).Level)
	var names []string
	for _, n := range t.Nodes {
		if st := statuses[n.ID]; st == rpg.Unlocked || st == rpg.Rusty {
			names = append(names, n.Name)
		}
	}
	return names
}

// rerollSlot keeps a directive's tier and skill. It keeps the node too while
// that node is still open; otherwise any open node of the skill will do.
func rerollSlot(c db.Challenge, trees []db.SkillTree) rpg.DirectiveSlot {
	tier := c.Tier
	if tier == rpg.TierNone {
		tier = rpg.TierCoreOp
	}
	slot := rpg.DirectiveSlot{Tier: tier, Skill: c.SkillName, Level: 1}
	for _, t := range trees {
		if t.Skill.Name != c.SkillName {
			continue
		}
		slot.Level = rpg.ProgressFor(t.Skill.TotalXP).Level
		slot.Nodes = openNodeNames(t)
		for _, n := range slot.Nodes {
			if n == c.NodeName {
				slot.Nodes = []string{n}
				break
			}
		}
	}
	return slot
}

// nodeSlot is a focus slot for exactly one node. The node must be open or
// rusty right now, so a directive never targets a locked node.
func nodeSlot(c db.Challenge, trees []db.SkillTree) (rpg.DirectiveSlot, error) {
	for _, t := range trees {
		if t.Skill.Name != c.SkillName {
			continue
		}
		for _, n := range openNodeNames(t) {
			if n == c.NodeName {
				return rpg.DirectiveSlot{Tier: rpg.TierFocus, Skill: t.Skill.Name,
					Level: rpg.ProgressFor(t.Skill.TotalXP).Level, Nodes: []string{n}}, nil
			}
		}
		return rpg.DirectiveSlot{}, fmt.Errorf("%s is not open, so it cannot get a directive", c.NodeName)
	}
	return rpg.DirectiveSlot{}, fmt.Errorf("skill %s not found", c.SkillName)
}

// bonusSlot gives the bonus to the active skill with the fewest directives
// today, the first such skill on a tie.
func bonusSlot(outlines []rpg.SkillOutline, list []db.Challenge) rpg.DirectiveSlot {
	count := map[string]int{}
	for _, c := range list {
		count[c.SkillName]++
	}
	best := outlines[0]
	for _, o := range outlines[1:] {
		if count[o.Name] < count[best.Name] {
			best = o
		}
	}
	return rpg.DirectiveSlot{Tier: rpg.TierOvertime, Skill: best.Name, Level: best.Level, Nodes: best.Nodes}
}

// completeChallengeCmd ticks a challenge off and awards its XP.
func completeChallengeCmd(store *db.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ev, err := store.CompleteChallenge(ctx, id)
		return challengeDoneMsg{event: ev, err: err}
	}
}

func deletePlanCmd(store *db.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := store.DeletePlan(ctx, id)
		return planDeletedMsg{id: id, err: err}
	}
}

func updateDescriptionCmd(store *db.Store, id int64, description string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		plan, err := store.UpdatePlanDescription(ctx, id, description)
		return planUpdatedMsg{plan: plan, err: err}
	}
}
