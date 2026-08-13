package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// MemoryEntry is a local memory fact (ADR-044).
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

type memoryListResponse struct {
	Entries []MemoryEntry `json:"entries"`
}

// ListMemory returns memory entries (optional session/query/kind filters).
func (c *Client) ListMemory(ctx context.Context, sessionID, query, kind string, limit int) ([]MemoryEntry, error) {
	return c.ListMemoryFilter(ctx, sessionID, query, kind, limit, false)
}

// ListMemoryFilter is ListMemory with optional archived-only rows.
func (c *Client) ListMemoryFilter(ctx context.Context, sessionID, query, kind string, limit int, includeArchived bool) ([]MemoryEntry, error) {
	path := "/v1/memory"
	q := url.Values{}
	if s := strings.TrimSpace(sessionID); s != "" {
		q.Set("session_id", s)
	}
	if s := strings.TrimSpace(query); s != "" {
		q.Set("q", s)
	}
	if s := strings.TrimSpace(kind); s != "" {
		q.Set("kind", s)
	}
	if includeArchived {
		q.Set("include_archived", "true")
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var response memoryListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list memory: %w", err)
	}
	return response.Entries, nil
}

// RefreshMemory invalidates the frozen inject snapshot for a session (empty = all).
func (c *Client) RefreshMemory(ctx context.Context, sessionID string) error {
	var out map[string]any
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/memory/actions", map[string]string{
		"action": "refresh", "session_id": strings.TrimSpace(sessionID),
	}, &out); err != nil {
		return fmt.Errorf("refresh memory: %w", err)
	}
	return nil
}

// ForgetMemory deletes one entry by id.
func (c *Client) ForgetMemory(ctx context.Context, entryID string) error {
	var out map[string]any
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/memory/actions", map[string]string{
		"action": "forget", "entry_id": strings.TrimSpace(entryID),
	}, &out); err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	return nil
}

// PromoteMemory promotes a session entry to user-global curated.
func (c *Client) PromoteMemory(ctx context.Context, entryID string) (MemoryEntry, error) {
	var out struct {
		OK    bool        `json:"ok"`
		Entry MemoryEntry `json:"entry"`
	}
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/memory/actions", map[string]string{
		"action": "promote", "entry_id": strings.TrimSpace(entryID),
	}, &out); err != nil {
		return MemoryEntry{}, fmt.Errorf("promote memory: %w", err)
	}
	return out.Entry, nil
}
