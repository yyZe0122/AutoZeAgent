package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Permission is a pending or decided tool-call permission (ADR-043).
type Permission struct {
	ID         string `json:"permission_id"`
	SessionID  string `json:"session_id,omitempty"`
	TaskID     string `json:"task_id"`
	RunID      string `json:"run_id"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Capability string `json:"capability,omitempty"`
	Path       string `json:"path,omitempty"`
	Risk       string `json:"risk,omitempty"`
	State      string `json:"state"`
	GrantID    string `json:"grant_id,omitempty"`
	Decision   string `json:"decision,omitempty"`
	CreatedAt  string `json:"created_at"`
	DecidedAt  string `json:"decided_at,omitempty"`
}

type permissionListResponse struct {
	Permissions []Permission `json:"permissions"`
}

// ListPermissions returns pending tool permissions (optional session filter).
func (c *Client) ListPermissions(ctx context.Context, sessionID string, limit int) ([]Permission, error) {
	path := "/v1/permissions"
	q := url.Values{}
	if s := strings.TrimSpace(sessionID); s != "" {
		q.Set("session_id", s)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var response permissionListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	return response.Permissions, nil
}

// DecidePermission applies allow_once | allow_similar | allow_permanent | deny.
// allow_session is accepted as an alias of allow_similar.
func (c *Client) DecidePermission(ctx context.Context, permissionID, decision string) (Permission, error) {
	return c.DecidePermissionConfirm(ctx, permissionID, decision, false)
}

// DecidePermissionConfirm is DecidePermission with permanent second confirmation.
func (c *Client) DecidePermissionConfirm(ctx context.Context, permissionID, decision string, confirm bool) (Permission, error) {
	var item Permission
	path := "/v1/permissions/" + url.PathEscape(strings.TrimSpace(permissionID)) + "/decide"
	body := map[string]any{"decision": decision, "actor": "tui", "confirm": confirm}
	if err := c.inner.DoJSON(ctx, http.MethodPost, path, body, &item); err != nil {
		return Permission{}, fmt.Errorf("decide permission: %w", err)
	}
	return item, nil
}
