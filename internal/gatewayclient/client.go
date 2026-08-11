// Package gatewayclient is the shared local-gateway facade used by the CLI and TUI.
// It talks only to the gateway HTTP API — no DB, tools, providers, or grants.
package gatewayclient

import (
	"context"
	"fmt"

	"autozeagent.local/autozeagent/internal/platform/paths"
	"autozeagent.local/autozeagent/pkg/eventapi"
)

// Client is the typed local-gateway facade (HTTP/SSE + workflow helpers).
type Client struct {
	inner *transport
}

// New connects to the local gateway for the given runtime mode.
func New(mode paths.Mode) (*Client, error) {
	layout, err := paths.Resolve(mode)
	if err != nil {
		return nil, err
	}
	return NewFromRuntimeDir(layout.RuntimeDir)
}

// NewFromRuntimeDir connects using an already-resolved runtime directory.
func NewFromRuntimeDir(runtimeDir string) (*Client, error) {
	inner, err := newLocalTransport(runtimeDir)
	if err != nil {
		return nil, fmt.Errorf("connect to local gateway: %w", err)
	}
	return &Client{inner: inner}, nil
}

// StreamEvents consumes GET /v1/events/stream until ctx is cancelled.
func (c *Client) StreamEvents(ctx context.Context, after uint64, emit func(eventapi.Envelope) error) error {
	return c.inner.StreamSSE(ctx, "/v1/events/stream", after, func(event sseEvent) error {
		if len(event.Data) == 0 {
			return nil
		}
		var envelope eventapi.Envelope
		if err := jsonUnmarshal(event.Data, &envelope); err != nil {
			return fmt.Errorf("decode SSE event: %w", err)
		}
		if emit == nil {
			return nil
		}
		return emit(envelope)
	})
}
