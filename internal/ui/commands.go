package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/navid-fn/lazyplaner/internal/db"
	"github.com/navid-fn/lazyplaner/internal/game"
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
		xp, xpErr := game.AwardPlanXP(client, store, plan)
		return planSavedMsg{plan: plan, xp: xp, xpErr: xpErr}
	}
}

func loadRoadCmd(store *db.Store) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Rust is checked whenever character data loads, which includes
		// start-up, so a skill left alone for two weeks shows it next launch.
		if err := game.ApplyRust(ctx, store, time.Now()); err != nil {
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
		eras, err := store.ListEras(ctx)
		if err != nil {
			return roadLoadedMsg{err: err}
		}
		stats, err := store.ChallengeStats(ctx, db.Today())
		return roadLoadedMsg{skills: skills, nodes: nodes, events: events, eras: eras, stats: stats, err: err}
	}
}

// expandEraCmd asks the model for the next era of a skill's tree and saves
// it.
func expandEraCmd(client *llm.Client, store *db.Store, skill string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return eraExpandedMsg{skill: skill, err: errors.New("planner is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		res, err := game.GenerateEra(ctx, client, store, skill, "")
		return eraExpandedMsg{skill: skill, era: res.Era, err: err}
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

		roadmap := game.RoadmapNodes(nodes)

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
	// A curated queue can fill a set or a focus directive without a model;
	// rerolls and bonuses always need one.
	var gen rpg.Generator
	if client != nil {
		gen = client
	} else if job == dailyReroll || job == dailyBonus {
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
		recovery, err := recoveryDay(ctx, store, day)
		if err != nil {
			return "", err
		}
		slots := rpg.DailySlots(outlines)
		if recovery {
			slots = rpg.RecoverySlots(outlines)
		}
		queue, err := game.Queue(ctx, store, trees, list)
		if err != nil {
			return "", err
		}
		drafts, curated, err := game.BuildDay(ctx, gen, slots, queue, nil)
		if err != nil {
			return "", err
		}
		if err := store.SaveChallenges(ctx, day, game.NewChallenges(drafts)); err != nil {
			return "", err
		}
		note := fmt.Sprintf("%d directives for today.", len(drafts))
		if recovery {
			note = fmt.Sprintf("🌿 Recovery day: %d light directive%s.", len(drafts), plural(len(drafts)))
		}
		if curated > 0 {
			note += fmt.Sprintf(" 📜 %d from your curated queue.", curated)
		}
		if missing := len(slots) - len(drafts); missing > 0 {
			note += fmt.Sprintf(" %d slot%s left empty: the model was not available.", missing, plural(missing))
		}
		return note, nil

	case dailyReroll:
		drafts, err := rpg.BuildDirectives(ctx, client, []rpg.DirectiveSlot{rerollSlot(target, trees)}, avoid)
		if err != nil {
			return "", err
		}
		if err := store.RerollChallenge(ctx, target.ID, game.NewChallenges(drafts)[0]); err != nil {
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
		if err := store.AddBonusChallenge(ctx, day, game.NewChallenges(drafts)[0]); err != nil {
			return "", err
		}
		return "★ Bonus Overtime Directive: " + drafts[0].Title, nil

	case dailyNode:
		slot, err := nodeSlot(target, trees)
		if err != nil {
			return "", err
		}
		queue, err := game.Queue(ctx, store, trees, list)
		if err != nil {
			return "", err
		}
		drafts, curated, err := game.BuildDay(ctx, gen, []rpg.DirectiveSlot{slot}, queue, avoid)
		if err != nil {
			return "", err
		}
		if err := store.AddNodeChallenge(ctx, day, game.NewChallenges(drafts)[0]); err != nil {
			return "", err
		}
		if curated > 0 {
			return "📜 Next curated task for " + target.NodeName + ": " + drafts[0].Title, nil
		}
		return "🎯 New directive for " + target.NodeName + ": " + drafts[0].Title, nil
	}
	return "", nil
}

// recoveryDay reports whether day's set should be light: a note shielded
// the day before (illness or burnout), or the day itself before its set
// was made.
func recoveryDay(ctx context.Context, store *db.Store, day string) (bool, error) {
	shielded, err := store.ShieldedDays(ctx)
	if err != nil {
		return false, err
	}
	d, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return false, fmt.Errorf("parse day: %w", err)
	}
	_, yesterday := shielded[d.AddDate(0, 0, -1).Format("2006-01-02")]
	_, today := shielded[day]
	return yesterday || today, nil
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
	statuses := rpg.NodeStatuses(game.TreeNodes(t.Nodes), rpg.ProgressFor(t.Skill.TotalXP).Level)
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

// completeChallengeCmd ticks a challenge off and awards its XP, keeping
// note, the person's proof of the work.
func completeChallengeCmd(store *db.Store, id int64, note string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ev, err := store.CompleteChallenge(ctx, id, note)
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
