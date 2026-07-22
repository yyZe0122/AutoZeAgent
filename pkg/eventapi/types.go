// Package eventapi defines stable event envelope types used across process
// boundaries. Concrete domain payloads remain versioned by schema.
package eventapi

import (
	"encoding/json"
	"time"
)

type Envelope struct {
	ID               string          `json:"event_id"`
	Type             string          `json:"event_type"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion uint64          `json:"aggregate_version"`
	Sequence         uint64          `json:"sequence"`
	OccurredAt       time.Time       `json:"occurred_at"`
	Producer         string          `json:"producer"`
	SchemaVersion    int             `json:"schema_version"`
	TraceID          string          `json:"trace_id,omitempty"`
	CausationID      string          `json:"causation_id,omitempty"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}
