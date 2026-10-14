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

// buildSkillTreesCmd asks the model for a tree for each new start skill and
// saves them. Trees already saved stay if a later one fails.
func buildSkillTreesCmd(client *llm.Client, store *db.Store, pending []pendingSkill) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return onboardingDoneMsg{err: errors.New("planner is not configured")}
		}
		for i, p := range pending {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			category, nodes, err := rpg.BuildRoadmap(ctx, client, p.Name, p.Level, p.Goal)
			cancel()
			if err != nil {
				return onboardingDoneMsg{err: err}
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

			saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
			// The auditor's category was shown on the setup card, so it wins
			// over the tree's; without an audit, the tree's choice is used.
			if p.Category != "" {
				category = p.Category
			}
			_, err = store.CreateSkillTree(saveCtx, db.NewSkillTree{
				Name:       p.Name,
				Category:   category,
				StartLevel: p.Level,
				Rusty:      p.Rusty,
				Garrisoned: garrisoned(i),
				Nodes:      roadmap,
			})
			saveCancel()
			if err != nil {
				return onboardingDoneMsg{err: fmt.Errorf("save %s: %w", p.Name, err)}
			}
		}
		return onboardingDoneMsg{count: len(pending)}
	}
}

// dailyMode says when loadDailyCmd asks the model for a new set.
type dailyMode int

const (
	dailyLoad   dailyMode = iota // only read today's challenges
	dailyCreate                  // create a set when today has none
	dailyRenew                   // create a set when today has none or all are done
)

// canRenew reports whether a new set may be made from this list: there is
// no set yet, or every challenge in the current set is done.
func canRenew(list []db.Challenge) bool {
	for _, c := range list {
		if !c.Done {
			return false
		}
	}
	return true
}

// loadDailyCmd returns the challenges for today, creating a new set first
// when mode allows it. size is the number of challenges for a new set; it is
// saved as the default for later sets. It is ignored for dailyLoad.
func loadDailyCmd(client *llm.Client, store *db.Store, mode dailyMode, size int) tea.Cmd {
	return func() tea.Msg {
		day := db.Today()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		list, err := store.ListChallenges(ctx, day)
		if err != nil {
			return challengesLoadedMsg{err: err}
		}

		count, err := store.DailyChallengeCount(ctx)
		if err != nil {
			return challengesLoadedMsg{err: err}
		}

		create := (mode == dailyCreate && len(list) == 0) ||
			(mode == dailyRenew && canRenew(list))
		if create {
			if size > 0 {
				count = size
				if err := store.SetDailyChallengeCount(ctx, size); err != nil {
					return challengesLoadedMsg{err: err}
				}
			}
			list, err = generateDaily(client, store, day, list, count)
			if err != nil {
				return challengesLoadedMsg{err: err}
			}
		}

		// The model call above can take longer than ctx allows, so the stats
		// query gets a fresh deadline.
		statsCtx, cancelStats := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelStats()
		stats, err := store.ChallengeStats(statsCtx, day)
		return challengesLoadedMsg{challenges: list, stats: stats, count: count, err: err}
	}
}

// generateDaily asks the model for a new set for the day. The titles in
// previous are passed along so the new set does not repeat them.
func generateDaily(client *llm.Client, store *db.Store, day string, previous []db.Challenge, count int) ([]db.Challenge, error) {
	if client == nil {
		return nil, errors.New("planner is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	trees, err := store.SkillTrees(ctx)
	if err != nil {
		return nil, err
	}
	if len(trees) == 0 {
		return nil, errors.New("no skills yet, create your character first")
	}

	avoid := make([]string, 0, len(previous))
	for _, c := range previous {
		avoid = append(avoid, c.Title)
	}
	drafts, err := rpg.BuildChallenges(ctx, client, outlinesFrom(trees), count, avoid)
	if err != nil {
		return nil, err
	}

	items := make([]db.NewChallenge, 0, len(drafts))
	for _, d := range drafts {
		items = append(items, db.NewChallenge{
			SkillName:   d.Skill,
			NodeName:    d.Node,
			Title:       d.Title,
			Description: d.Description,
			Difficulty:  d.Difficulty,
			Minutes:     d.Minutes,
			XP:          d.XP,
		})
	}
	if err := store.SaveChallenges(ctx, day, items); err != nil {
		return nil, err
	}
	return store.ListChallenges(ctx, day)
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
