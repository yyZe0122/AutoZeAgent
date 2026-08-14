// Package sessiontodo is the session-scoped todo list (Phase Q / QE).
// Not kernel Task / Planner.
package sessiontodo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
)

// Item is one session todo row.
type Item struct {
	SessionID string
	ID        string
	Content   string
	Status    string
	Position  int
	UpdatedAt string
}

// Store persists session_todos on core.db.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("sessiontodo store requires database")
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Store) List(ctx context.Context, sessionID string) ([]Item, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, item_id, content, status, position, updated_at
		FROM session_todos WHERE session_id = ?
		ORDER BY position ASC, item_id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session todos: %w", err)
	}
	defer rows.Close()
	out := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.SessionID, &item.ID, &item.Content, &item.Status, &item.Position, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) Replace(ctx context.Context, sessionID string, items []Item) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session id is required")
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_todos WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	for i, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("todo-%d", i+1)
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		status := normalizeStatus(item.Status)
		pos := item.Position
		if pos == 0 {
			pos = i
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO session_todos (session_id, item_id, content, status, position, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, sessionID, id, content, status, pos, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case StatusInProgress, "in-progress", "doing":
		return StatusInProgress
	case StatusCompleted, "done":
		return StatusCompleted
	case StatusCancelled, "canceled":
		return StatusCancelled
	default:
		return StatusPending
	}
}

// CompactBlock is a short Ephemeral inject (not Prefix).
func CompactBlock(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Session todos:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- [%s] %s\n", item.Status, item.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
