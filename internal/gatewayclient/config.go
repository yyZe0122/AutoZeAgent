package gatewayclient

import (
	"context"
	"fmt"
	"net/http"

	"autozeagent.local/autozeagent/internal/app"
	"autozeagent.local/autozeagent/internal/gateway"
)

type Health struct {
	OK   bool       `json:"ok"`
	Core app.Status `json:"core"`
}

type ModelConfig = gateway.ModelConfig

// MCPStatus is the secret-free MCP snapshot from GET /v1/config/mcp.
type MCPStatus = gateway.MCPStatus

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
