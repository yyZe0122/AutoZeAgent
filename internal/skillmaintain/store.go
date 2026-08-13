package skillmaintain

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	ActionDraft   = "draft"
	ActionApply   = "apply"
	ActionReject  = "reject"
	ActionUsed    = "used"
	ActionArchive = "archive"
)

// Usage is one skill_usage row.
type Usage struct {
	SkillID    string
	LastUsedAt string
	ArchivedAt string
	UpdatedAt  string
}

// Event is one skill_events row.
type Event struct {
	ID          string
	SkillID     string
	Action      string
	Actor       string
	Path        string
	ContentHash string
	CreatedAt   string
}

// Store persists skill usage and events on core.db (ADR-050).
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("skillmaintain store requires database")
	}
	return &Store{db: db}, nil
}

func (s *Store) GetUsage(ctx context.Context, skillID string) (Usage, error) {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return Usage{}, errors.New("skill_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT skill_id, last_used_at, archived_at, updated_at
		FROM skill_usage WHERE skill_id = ?`, skillID)
	var u Usage
	if err := row.Scan(&u.SkillID, &u.LastUsedAt, &u.ArchivedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Usage{SkillID: skillID}, nil
		}
		return Usage{}, err
	}
	return u, nil
}

func (s *Store) ListUsage(ctx context.Context) ([]Usage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT skill_id, last_used_at, archived_at, updated_at FROM skill_usage`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Usage
	for rows.Next() {
		var u Usage
		if err := rows.Scan(&u.SkillID, &u.LastUsedAt, &u.ArchivedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) RecordUsed(ctx context.Context, skillID, nowRFC string) error {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		return errors.New("skill_id is required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO skill_usage(skill_id, last_used_at, archived_at, updated_at)
		VALUES(?, ?, '', ?)
		ON CONFLICT(skill_id) DO UPDATE SET
			last_used_at = excluded.last_used_at,
			archived_at = '',
			updated_at = excluded.updated_at`,
		skillID, nowRFC, nowRFC)
	return err
}

func (s *Store) ArchiveExpired(ctx context.Context, cutoffRFC, nowRFC string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT skill_id FROM skill_usage
		WHERE archived_at = '' AND last_used_at != '' AND last_used_at <= ?`,
		cutoffRFC)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return nil, nil
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE skill_usage
		SET archived_at = ?, updated_at = ?
		WHERE archived_at = '' AND last_used_at != '' AND last_used_at <= ?`,
		nowRFC, nowRFC, cutoffRFC)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) InsertEvent(ctx context.Context, e Event) error {
	if strings.TrimSpace(e.ID) == "" {
		id, err := randomID("skillevent-")
		if err != nil {
			return err
		}
		e.ID = id
	}
	if strings.TrimSpace(e.SkillID) == "" || strings.TrimSpace(e.Action) == "" {
		return errors.New("skill_id and action are required")
	}
	if strings.TrimSpace(e.CreatedAt) == "" {
		return errors.New("created_at is required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO skill_events(event_id, skill_id, action, actor, path, content_hash, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.SkillID, e.Action, e.Actor, e.Path, e.ContentHash, e.CreatedAt)
	return err
}

func (s *Store) ListEvents(ctx context.Context, skillID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 24
	}
	if limit > 200 {
		limit = 200
	}
	skillID = strings.TrimSpace(skillID)
	var (
		rows *sql.Rows
		err  error
	)
	if skillID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT event_id, skill_id, action, actor, path, content_hash, created_at
			FROM skill_events ORDER BY created_at DESC, event_id DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT event_id, skill_id, action, actor, path, content_hash, created_at
			FROM skill_events WHERE skill_id = ?
			ORDER BY created_at DESC, event_id DESC LIMIT ?`, skillID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.SkillID, &e.Action, &e.Actor, &e.Path, &e.ContentHash, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func randomID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}
