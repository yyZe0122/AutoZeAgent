package gatewayclient

import (
	"context"
	"fmt"
	"net/url"

	"autozeagent.local/autozeagent/internal/modelstream"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

// StreamModelEvents consumes GET /v1/model-stream until ctx is cancelled.
// Optional sessionID / runID filter the hub subscription on the daemon.
func (c *Client) StreamModelEvents(ctx context.Context, sessionID SessionID, runID RunID, emit func(modelstream.Envelope) error) error {
	path := "/v1/model-stream"
	q := url.Values{}
	if sessionID != "" {
		q.Set("session_id", string(sessionID))
	}
	if runID != "" {
		q.Set("run_id", string(runID))
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	return c.inner.StreamSSE(ctx, path, 0, func(event sseEvent) error {
		if len(event.Data) == 0 {
			return nil
		}
		var envelope modelstream.Envelope
		if err := jsonUnmarshal(event.Data, &envelope); err != nil {
			return fmt.Errorf("decode model stream event: %w", err)
		}
		// Ensure event type is set for older partial payloads.
		if envelope.Event.Type == "" && envelope.Event.ContentDelta != "" {
			envelope.Event.Type = providerapi.StreamDelta
		}
		if emit == nil {
			return nil
		}
		return emit(envelope)
	})
}
