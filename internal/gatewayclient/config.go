package gatewayclient

import (
	"context"
	"fmt"
	"net/http"

	"autozeagent.local/autozeagent/internal/app"
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

// Skill is metadata from GET /v1/skills (no body or file paths).
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// ListSkills returns discovered skill metadata (explicit selection only; no auto-match).
func (c *Client) ListSkills(ctx context.Context) ([]Skill, error) {
	var response struct {
		Skills []Skill `json:"skills"`
	}
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/skills", nil, &response); err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	if response.Skills == nil {
		return []Skill{}, nil
	}
	return response.Skills, nil
}
