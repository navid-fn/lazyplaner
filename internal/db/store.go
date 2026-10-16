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

	db, err := sql.Open("sqlite", dsn(path))
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

// connPragmas run on every new connection. WAL lets the TUI and the CLI use
// the database at the same time: readers never block the writer. A writer
// that finds another writer waits up to busy_timeout instead of failing
// with SQLITE_BUSY, and _txlock=immediate takes the write lock when a
// transaction begins, so two transactions cannot deadlock upgrading from
// read to write.
var connPragmas = []string{"journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"}

// dsn adds the connection pragmas to a database path.
func dsn(path string) string {
	params := "_txlock=immediate"
	for _, p := range connPragmas {
		params += "&_pragma=" + p
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + params
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
		// Curated tasks an external agent designed for a node, done in
		// position order. A directive pulled from one points back at it
		// through challenges.node_task_id.
		`CREATE TABLE IF NOT EXISTS node_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_id INTEGER NOT NULL REFERENCES skills(id),
			node_id INTEGER NOT NULL REFERENCES skill_nodes(id),
			position INTEGER NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			difficulty INTEGER NOT NULL,
			minutes INTEGER NOT NULL,
			is_completed INTEGER NOT NULL DEFAULT 0
		);`,
		// The title of each era after the first, when one was given.
		`CREATE TABLE IF NOT EXISTS skill_eras (
			skill_id INTEGER NOT NULL REFERENCES skills(id),
			era INTEGER NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			PRIMARY KEY (skill_id, era)
		);`,
		// Daily notes. created_at is the note's date and never changes.
		`CREATE TABLE IF NOT EXISTS notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			body TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
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
		// What the note analyzer read from a note. status is '' until the
		// note is analyzed; xp_event_id is the XP claimed from it, 0 if none.
		{"notes", "status", "TEXT NOT NULL DEFAULT ''"},
		{"notes", "shield", "INTEGER NOT NULL DEFAULT 0"},
		{"notes", "skill_name", "TEXT NOT NULL DEFAULT ''"},
		{"notes", "minutes", "INTEGER NOT NULL DEFAULT 0"},
		{"notes", "follow_up", "TEXT NOT NULL DEFAULT ''"},
		{"notes", "xp_event_id", "INTEGER NOT NULL DEFAULT 0"},
		{"notes", "claimed_xp", "INTEGER NOT NULL DEFAULT 0"},
		// The curated task a directive was pulled from, 0 for none.
		{"challenges", "node_task_id", "INTEGER NOT NULL DEFAULT 0"},
		// Multi-era trees. A tree grows in eras of rpg.RoadmapSize nodes;
		// era 1 is the first tree. extra_parent_id is a node's second
		// parent (0 for none), which closes each era's diamond. is_capstone
		// marks the node an era ends on and the next era grows from.
		{"skill_nodes", "era", "INTEGER NOT NULL DEFAULT 1"},
		{"skill_nodes", "extra_parent_id", "INTEGER NOT NULL DEFAULT 0"},
		{"skill_nodes", "is_capstone", "INTEGER NOT NULL DEFAULT 0"},
		// A directive's and a curated task's execution guide (markdown).
		{"challenges", "details", "TEXT NOT NULL DEFAULT ''"},
		{"node_tasks", "details", "TEXT NOT NULL DEFAULT ''"},
		// A directive is started before it is finished: started_at is when
		// (RFC 3339, NULL until started), completion_note is the proof the
		// person wrote when finishing it. xp_events.note keeps that proof
		// with the award.
		{"challenges", "started_at", "TEXT"},
		{"challenges", "completion_note", "TEXT NOT NULL DEFAULT ''"},
		{"xp_events", "note", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, mg := range migrations {
		if err := s.addColumnIfMissing(ctx, mg.table, mg.column, mg.definition); err != nil {
			return err
		}
	}
	if err := s.backfillLastPracticed(ctx); err != nil {
		return err
	}
	return s.backfillDiamonds(ctx)
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
