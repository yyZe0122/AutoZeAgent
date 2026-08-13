package corequery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MemoryEntry is a read projection of memory_entries (ADR-044).
type MemoryEntry struct {
	ID         string   `json:"entry_id"`
	SessionID  string   `json:"session_id,omitempty"`
	Content    string   `json:"content"`
	Source     string   `json:"source"`
	Tags       []string `json:"tags,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Priority   int      `json:"priority,omitempty"`
	ExpiresAt  string   `json:"expires_at,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
	ArchivedAt string   `json:"archived_at,omitempty"`
}

// ListMemory returns memory entries (newest/priority first). Optional query uses LIKE.
func (s *Store) ListMemory(ctx context.Context, options MemoryListOptions) ([]MemoryEntry, error) {
	if err := validateList(ctx, options.Page, SortDescending); err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(options.SessionID)
	query := strings.TrimSpace(options.Query)
	kind := strings.TrimSpace(options.Kind)

	var (
		rows *sql.Rows
		err  error
	)
	base := `
		SELECT entry_id, session_id, content, source, tags_json, created_at,
		       COALESCE(kind, 'session'), COALESCE(priority, 0),
		       COALESCE(expires_at, ''), COALESCE(updated_at, ''), COALESCE(archived_at, '')
		FROM memory_entries WHERE 1=1`
	var args []any
	if sessionID == "" && !options.IncludeGlobal {
		// No session filter: list global only when IncludeGlobal, else all.
		// Empty session_id + IncludeGlobal=false → all entries (admin/list UI).
	} else if sessionID == "" && options.IncludeGlobal {
		base += ` AND session_id = ''`
	} else if options.IncludeGlobal {
		base += ` AND (session_id = ? OR session_id = '')`
		args = append(args, sessionID)
	} else {
		base += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if options.IncludeArchived {
		base += ` AND COALESCE(archived_at, '') != ''`
	} else {
		base += ` AND COALESCE(archived_at, '') = '' AND (expires_at = '' OR expires_at > ?)`
		args = append(args, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if kind != "" {
		base += ` AND COALESCE(kind, 'session') = ?`
		args = append(args, kind)
	}
	if query != "" {
		base += ` AND content LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLikeMemory(query)+"%")
	}
	base += ` ORDER BY priority DESC, created_at DESC LIMIT ? OFFSET ?`
	args = append(args, options.Page.Limit, options.Page.Offset)

	rows, err = s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list memory: %w", err)
	}
	defer rows.Close()
	return scanMemoryEntries(rows)
}

func scanMemoryEntries(rows *sql.Rows) ([]MemoryEntry, error) {
	var out []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		var tagsJSON string
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.Content, &e.Source, &tagsJSON, &e.CreatedAt,
			&e.Kind, &e.Priority, &e.ExpiresAt, &e.UpdatedAt, &e.ArchivedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
		if e.Tags == nil {
			e.Tags = []string{}
		}
		if err := normalizeTimeFields(&e.CreatedAt); err != nil {
			return nil, err
		}
		if e.UpdatedAt != "" {
			if err := normalizeTimeFields(&e.UpdatedAt); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func escapeLikeMemory(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
