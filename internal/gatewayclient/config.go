package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/app"
)

type Health struct {
	OK   bool       `json:"ok"`
	Core app.Status `json:"core"`
}

// ModelConfig is the secret-free model snapshot from GET/PUT /v1/config/model.
// JSON shape matches the gateway server response (not an import of server types).
type ModelConfig struct {
	Model  string   `json:"model"`
	Models []string `json:"models"`
	// ContextWindow is the selected model's context length in tokens; 0 = unknown.
	ContextWindow int64 `json:"context_window,omitempty"`
	// Ready is true when chat/switch can use the main model stack (ADR-048).
	Ready bool `json:"ready"`
	// Error explains why Ready is false (no secrets).
	Error string `json:"error,omitempty"`
}

// MCPStatus is the secret-free MCP snapshot from GET /v1/config/mcp.
type MCPStatus struct {
	Enabled bool `json:"enabled"`
	Total   int  `json:"total"`
	OK      int  `json:"ok"`
	Error   int  `json:"error"`
	Tools   int  `json:"tools"`
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/health", nil, &health); err != nil {
		return Health{}, fmt.Errorf("health: %w", err)
	}
	return health, nil
}

func (c *Client) ModelConfig(ctx context.Context) (ModelConfig, error) {
	var config ModelConfig
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/config/model", nil, &config); err != nil {
		return ModelConfig{}, fmt.Errorf("config model: %w", err)
	}
	if config.Models == nil {
		config.Models = []string{}
	}
	return config, nil
}

func (c *Client) SetModelConfig(ctx context.Context, model string) (ModelConfig, error) {
	var config ModelConfig
	if err := c.inner.DoJSON(ctx, http.MethodPut, "/v1/config/model", map[string]string{"model": model}, &config); err != nil {
		return ModelConfig{}, fmt.Errorf("set config model: %w", err)
	}
	if config.Models == nil {
		config.Models = []string{}
	}
	return config, nil
}

func (c *Client) MCPStatus(ctx context.Context) (MCPStatus, error) {
	var status MCPStatus
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/config/mcp", nil, &status); err != nil {
		return MCPStatus{}, fmt.Errorf("config mcp: %w", err)
	}
	return status, nil
}

// ChatCommand is one entry from GET /v1/config/commands (instruction template only).
type ChatCommand struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Template    string `json:"template"`
}

// ListChatCommands returns configured user slash templates (O3).
func (c *Client) ListChatCommands(ctx context.Context) ([]ChatCommand, error) {
	var response struct {
		Commands []ChatCommand `json:"commands"`
	}
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/config/commands", nil, &response); err != nil {
		return nil, fmt.Errorf("list chat commands: %w", err)
	}
	if response.Commands == nil {
		return []ChatCommand{}, nil
	}
	return response.Commands, nil
}

// Skill is metadata from GET /v1/skills (no body or file paths).
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Draft       bool   `json:"draft,omitempty"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
	ArchivedAt  string `json:"archived_at,omitempty"`
}

// SkillEvent is one skill_events row (ADR-050).
type SkillEvent struct {
	ID          string `json:"event_id"`
	SkillID     string `json:"skill_id"`
	Action      string `json:"action"`
	Actor       string `json:"actor,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// ListSkills returns discovered skill metadata (explicit selection only; no auto-match).
func (c *Client) ListSkills(ctx context.Context) ([]Skill, error) {
	return c.ListSkillsFilter(ctx, false)
}

// ListSkillsFilter is ListSkills with optional archived-only rows.
func (c *Client) ListSkillsFilter(ctx context.Context, includeArchived bool) ([]Skill, error) {
	path := "/v1/skills"
	if includeArchived {
		path += "?include_archived=true"
	}
	var response struct {
		Skills []Skill `json:"skills"`
	}
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	if response.Skills == nil {
		return []Skill{}, nil
	}
	return response.Skills, nil
}

// ListSkillEvents returns recent skill draft/apply/used events.
func (c *Client) ListSkillEvents(ctx context.Context, skillID string, limit int) ([]SkillEvent, error) {
	path := "/v1/skills/events"
	q := url.Values{}
	if s := strings.TrimSpace(skillID); s != "" {
		q.Set("skill_id", s)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var response struct {
		Events []SkillEvent `json:"events"`
	}
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list skill events: %w", err)
	}
	if response.Events == nil {
		return []SkillEvent{}, nil
	}
	return response.Events, nil
}

// ApplySkillDraft promotes SKILL.md.draft after user confirmation.
func (c *Client) ApplySkillDraft(ctx context.Context, skillID string) error {
	var out map[string]any
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/skills/actions", map[string]string{
		"action": "apply", "skill_id": strings.TrimSpace(skillID), "actor": "tui",
	}, &out); err != nil {
		return fmt.Errorf("apply skill draft: %w", err)
	}
	return nil
}

// RejectSkillDraft discards SKILL.md.draft.
func (c *Client) RejectSkillDraft(ctx context.Context, skillID string) error {
	var out map[string]any
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/skills/actions", map[string]string{
		"action": "reject", "skill_id": strings.TrimSpace(skillID), "actor": "tui",
	}, &out); err != nil {
		return fmt.Errorf("reject skill draft: %w", err)
	}
	return nil
}
