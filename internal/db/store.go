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
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS plans (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject TEXT NOT NULL,
			detail TEXT NOT NULL,
			description TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
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
