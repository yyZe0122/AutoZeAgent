package corequery

import (
	"context"
	"fmt"
	"strings"
)

// SkillEvent is a read projection of skill_events (ADR-050).
type SkillEvent struct {
	ID          string `json:"event_id"`
	SkillID     string `json:"skill_id"`
	Action      string `json:"action"`
	Actor       string `json:"actor,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// ListSkillEvents returns newest skill events first.
func (s *Store) ListSkillEvents(ctx context.Context, options SkillEventListOptions) ([]SkillEvent, error) {
	if err := validateList(ctx, options.Page, SortDescending); err != nil {
		return nil, err
	}
	skillID := strings.TrimSpace(options.SkillID)
	query := `
		SELECT event_id, skill_id, action, actor, path, content_hash, created_at
		FROM skill_events`
	args := []any{}
	if skillID != "" {
		query += ` WHERE skill_id = ?`
		args = append(args, skillID)
	}
	query += ` ORDER BY created_at DESC, event_id DESC LIMIT ? OFFSET ?`
	args = append(args, options.Page.Limit, options.Page.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list skill events: %w", err)
	}
	defer rows.Close()
	var out []SkillEvent
	for rows.Next() {
		var e SkillEvent
		if err := rows.Scan(&e.ID, &e.SkillID, &e.Action, &e.Actor, &e.Path, &e.ContentHash, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := normalizeTimeFields(&e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
