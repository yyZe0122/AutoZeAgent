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
