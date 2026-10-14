// Package db stores plans in a local SQLite database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Plan struct {
	ID          int64
	Subject     string
	Detail      string
	Description string
	CreatedAt   time.Time
}

type Store struct {
	db *sql.DB
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	store := &Store{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS plans (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject TEXT NOT NULL,
			detail TEXT NOT NULL,
			description TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		// Skills are the character sheet. Names are unique ignoring case.
		`CREATE TABLE IF NOT EXISTS skills (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE COLLATE NOCASE,
			category TEXT NOT NULL,
			total_xp INTEGER NOT NULL DEFAULT 0
		);`,
		// Each row is one award. plan_id has no foreign key on purpose: the
		// XP history stays even if the plan is deleted.
		`CREATE TABLE IF NOT EXISTS xp_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_id INTEGER NOT NULL REFERENCES skills(id),
			plan_id INTEGER NOT NULL DEFAULT 0,
			activity TEXT NOT NULL,
			difficulty INTEGER NOT NULL,
			minutes INTEGER NOT NULL,
			xp INTEGER NOT NULL,
			created_at DATETIME NOT NULL
		);`,
	}
	statements = append(statements,
		// Nodes form each skill's tree. parent_id 0 attaches the node to the
		// skill itself. Names are unique per skill, ignoring case.
		`CREATE TABLE IF NOT EXISTS skill_nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_id INTEGER NOT NULL REFERENCES skills(id),
			parent_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL COLLATE NOCASE,
			description TEXT NOT NULL DEFAULT '',
			required_level INTEGER NOT NULL,
			xp_needed INTEGER NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0,
			UNIQUE(skill_id, name COLLATE NOCASE)
		);`,
		// Daily challenges. day is the local date as YYYY-MM-DD. done_at is
		// NULL until the challenge is ticked off.
		`CREATE TABLE IF NOT EXISTS challenges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			day TEXT NOT NULL,
			skill_id INTEGER NOT NULL REFERENCES skills(id),
			node_id INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL,
			difficulty INTEGER NOT NULL,
			minutes INTEGER NOT NULL,
			xp INTEGER NOT NULL,
			done_at DATETIME
		);`,
	)

	statements = append(statements,
		// Small key/value store for preferences.
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
	)

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}

	// Databases created before skill trees existed lack this column.
	if err := s.addColumnIfMissing(ctx, "xp_events", "node_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// Each set of daily challenges for a day gets a batch number. Older
	// databases predate batches, so their rows are batch 1.
	if err := s.addColumnIfMissing(ctx, "challenges", "batch", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	// Challenges made before descriptions existed have an empty one.
	if err := s.addColumnIfMissing(ctx, "challenges", "description", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// A directive's tier (rpg.Tier). Older rows are 0, no tier.
	if err := s.addColumnIfMissing(ctx, "challenges", "tier", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// Skill stance and rust. is_active 1 is the active stance (the skill can
	// rust); 0 is garrisoned. last_practiced_at is the last time the skill
	// earned XP, as RFC 3339 text.
	migrations := []struct{ table, column, definition string }{
		{"skills", "is_active", "INTEGER NOT NULL DEFAULT 1"},
		{"skills", "last_practiced_at", "TEXT"},
		{"skill_nodes", "is_rusty", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, mg := range migrations {
		if err := s.addColumnIfMissing(ctx, mg.table, mg.column, mg.definition); err != nil {
			return err
		}
	}
	return s.backfillLastPracticed(ctx)
}

// addColumnIfMissing adds a column to a table that was created by an older
// version of the schema. SQLite has no ADD COLUMN IF NOT EXISTS, so the
// table is checked first.
func (s *Store) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}
	if found {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) CreatePlan(ctx context.Context, subject string, detail string, description string) (Plan, error) {
	createdAt := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO plans (subject, detail, description, created_at) VALUES (?, ?, ?, ?)`,
		subject,
		detail,
		description,
		createdAt,
	)
	if err != nil {
		return Plan{}, fmt.Errorf("insert plan: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Plan{}, fmt.Errorf("get insert id: %w", err)
	}

	return Plan{
		ID:          id,
		Subject:     subject,
		Detail:      detail,
		Description: description,
		CreatedAt:   createdAt,
	}, nil
}

func (s *Store) ListPlans(ctx context.Context) ([]Plan, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, subject, detail, description, created_at FROM plans ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []Plan
	for rows.Next() {
		var plan Plan
		if err := rows.Scan(&plan.ID, &plan.Subject, &plan.Detail, &plan.Description, &plan.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return plans, nil
}

// DeletePlan removes a plan by ID. It returns an error if no plan has that ID.
func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("plan %d not found", id)
	}
	return nil
}

func (s *Store) UpdatePlanDescription(ctx context.Context, id int64, description string) (Plan, error) {
	if id == 0 {
		return Plan{}, fmt.Errorf("invalid plan id")
	}
	if strings.TrimSpace(description) == "" {
		return Plan{}, fmt.Errorf("description is required")
	}

	_, err := s.db.ExecContext(
		ctx,
		`UPDATE plans SET description = ? WHERE id = ?`,
		description,
		id,
	)
	if err != nil {
		return Plan{}, fmt.Errorf("update plan: %w", err)
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, subject, detail, description, created_at FROM plans WHERE id = ?`,
		id,
	)
	var plan Plan
	if err := row.Scan(&plan.ID, &plan.Subject, &plan.Detail, &plan.Description, &plan.CreatedAt); err != nil {
		return Plan{}, fmt.Errorf("reload plan: %w", err)
	}

	return plan, nil
}
