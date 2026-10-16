package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/navid-fn/lazyplaner/internal/rpg"
)

// Note is a free-form daily note. CreatedAt is the note's date; UpdatedAt
// moves on each edit.
type Note struct {
	ID        int64
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Analysis is what the note analyzer read from the note. Its Status is
	// "" until the note has been analyzed.
	Analysis rpg.NoteAnalysis
	// ClaimedXP is the XP claimed from the note's work, 0 if none yet.
	ClaimedXP int
}

// Day is the note's local date in dayLayout.
func (n Note) Day() string {
	return n.CreatedAt.Local().Format(dayLayout)
}

// Claimed reports whether the note's XP has been claimed.
func (n Note) Claimed() bool {
	return n.ClaimedXP > 0
}

var (
	// ErrNoteClaimed is returned when a note's XP is claimed a second time.
	ErrNoteClaimed = errors.New("this note's XP is already claimed")
	// ErrNothingToClaim is returned for a note that names no finished work.
	ErrNothingToClaim = errors.New("this note has no work to claim XP for")
)

const noteColumns = `id, body, created_at, updated_at, status, shield, skill_name, minutes, follow_up, claimed_xp`

func scanNote(row interface{ Scan(...any) error }) (Note, error) {
	var n Note
	var shield int
	err := row.Scan(&n.ID, &n.Body, &n.CreatedAt, &n.UpdatedAt, &n.Analysis.Status, &shield,
		&n.Analysis.Skill, &n.Analysis.Minutes, &n.Analysis.FollowUp, &n.ClaimedXP)
	n.Analysis.Shield = shield != 0
	return n, err
}

// CreateNote saves a new note dated now.
func (s *Store) CreateNote(ctx context.Context, body string) (Note, error) {
	if strings.TrimSpace(body) == "" {
		return Note{}, fmt.Errorf("note is empty")
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO notes (body, created_at, updated_at) VALUES (?, ?, ?)`, body, now, now)
	if err != nil {
		return Note{}, fmt.Errorf("insert note: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Note{}, fmt.Errorf("get insert id: %w", err)
	}
	return Note{ID: id, Body: body, CreatedAt: now, UpdatedAt: now}, nil
}

// ListNotes returns every note, newest first.
func (s *Store) ListNotes(ctx context.Context) ([]Note, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+noteColumns+` FROM notes ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return notes, nil
}

func (s *Store) getNote(ctx context.Context, q querier, id int64) (Note, error) {
	n, err := scanNote(q.QueryRowContext(ctx, `SELECT `+noteColumns+` FROM notes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, fmt.Errorf("note %d not found", id)
	}
	if err != nil {
		return Note{}, fmt.Errorf("load note: %w", err)
	}
	return n, nil
}

// UpdateNote replaces a note's text. Its date stays; UpdatedAt becomes now.
// The old analysis no longer fits the new text, so it is cleared; XP
// already claimed stays claimed.
func (s *Store) UpdateNote(ctx context.Context, id int64, body string) (Note, error) {
	if strings.TrimSpace(body) == "" {
		return Note{}, fmt.Errorf("note is empty")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE notes SET body = ?, updated_at = ?, status = '', shield = 0, skill_name = '',
		 minutes = 0, follow_up = '' WHERE id = ?`, body, time.Now().UTC(), id)
	if err != nil {
		return Note{}, fmt.Errorf("update note: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return Note{}, fmt.Errorf("check update: %w", err)
	} else if affected == 0 {
		return Note{}, fmt.Errorf("note %d not found", id)
	}
	return s.getNote(ctx, s.db, id)
}

// SetNoteAnalysis stores what the analyzer read from a note.
func (s *Store) SetNoteAnalysis(ctx context.Context, id int64, a rpg.NoteAnalysis) (Note, error) {
	shield := 0
	if a.Shield {
		shield = 1
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notes SET status = ?, shield = ?, skill_name = ?, minutes = ?, follow_up = ? WHERE id = ?`,
		a.Status, shield, a.Skill, a.Minutes, a.FollowUp, id,
	); err != nil {
		return Note{}, fmt.Errorf("save note analysis: %w", err)
	}
	return s.getNote(ctx, s.db, id)
}

// ClaimNoteXP turns the work a note describes into XP on its skill. A note
// can be claimed once; the award and the claim commit together.
func (s *Store) ClaimNoteXP(ctx context.Context, id int64) (XPEvent, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return XPEvent{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	n, err := s.getNote(ctx, tx, id)
	if err != nil {
		return XPEvent{}, err
	}
	if n.Claimed() {
		return XPEvent{}, ErrNoteClaimed
	}
	if !n.Analysis.Claimable() {
		return XPEvent{}, ErrNothingToClaim
	}
	// Claims go to existing skills only; findOrCreateSkill would make a new
	// one for a skill deleted since the analysis.
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE name = ?`, n.Analysis.Skill).Scan(&exists); err != nil {
		return XPEvent{}, fmt.Errorf("find skill: %w", err)
	}
	if exists == 0 {
		return XPEvent{}, fmt.Errorf("skill %s no longer exists", n.Analysis.Skill)
	}

	ev, err := recordXPTx(ctx, tx, XPAward{
		Activity:   "📝 " + firstLine(n.Body),
		SkillName:  n.Analysis.Skill,
		Difficulty: rpg.ClaimDifficulty,
		Minutes:    n.Analysis.Minutes,
		XP:         n.Analysis.ClaimXP(),
	})
	if err != nil {
		return XPEvent{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE notes SET xp_event_id = ?, claimed_xp = ? WHERE id = ?`, ev.ID, ev.XP, id,
	); err != nil {
		return XPEvent{}, fmt.Errorf("mark note claimed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return XPEvent{}, fmt.Errorf("commit claim: %w", err)
	}
	return ev, nil
}

// firstLine is a note's first non-empty line, used as its title.
func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// DeleteNote removes a note by ID. It returns an error if no note has that ID.
// XP claimed from it stays: the work was still done.
func (s *Store) DeleteNote(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("note %d not found", id)
	}
	return nil
}

// ShieldedDays returns the local days a note shielded, each with the
// status that shielded it.
func (s *Store) ShieldedDays(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT created_at, status FROM notes WHERE shield = 1`)
	if err != nil {
		return nil, fmt.Errorf("list shielded days: %w", err)
	}
	defer rows.Close()

	days := map[string]string{}
	for rows.Next() {
		var at time.Time
		var status string
		if err := rows.Scan(&at, &status); err != nil {
			return nil, fmt.Errorf("scan shielded day: %w", err)
		}
		days[at.Local().Format(dayLayout)] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return days, nil
}

// DayLog is what happened on one local day: the directives set and done,
// the XP earned, and the skills that earned it.
type DayLog struct {
	DirectivesDone  int
	DirectivesTotal int
	XP              int
	Skills          []string // in the order they first earned XP that day
}

// DayLogs returns the log of every day with directives or XP, keyed by
// local date in dayLayout.
func (s *Store) DayLogs(ctx context.Context) (map[string]DayLog, error) {
	logs := map[string]DayLog{}

	rows, err := s.db.QueryContext(ctx,
		`SELECT day, COUNT(*), COALESCE(SUM(done_at IS NOT NULL), 0) FROM challenges GROUP BY day`)
	if err != nil {
		return nil, fmt.Errorf("count directives by day: %w", err)
	}
	for rows.Next() {
		var day string
		var l DayLog
		if err := rows.Scan(&day, &l.DirectivesTotal, &l.DirectivesDone); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan directive day: %w", err)
		}
		logs[day] = l
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// XP is grouped in Go: created_at is stored in UTC and the day must be
	// the local one.
	rows, err = s.db.QueryContext(ctx,
		`SELECT e.created_at, e.xp, s.name FROM xp_events e JOIN skills s ON s.id = e.skill_id
		 ORDER BY e.created_at, e.id`)
	if err != nil {
		return nil, fmt.Errorf("list xp by day: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var xp int
		var skill string
		if err := rows.Scan(&at, &xp, &skill); err != nil {
			return nil, fmt.Errorf("scan xp day: %w", err)
		}
		day := at.Local().Format(dayLayout)
		l := logs[day]
		l.XP += xp
		if !containsFold(l.Skills, skill) {
			l.Skills = append(l.Skills, skill)
		}
		logs[day] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return logs, nil
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
