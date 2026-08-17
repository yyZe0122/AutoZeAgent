// Package toolapi defines the stable tool request metadata required for policy
// enforcement and auditing.
package toolapi

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// ErrDenied is returned when a tool call is rejected before or instead of
// producing side effects (policy, args, path, grant). Agent runners treat this
// as a recoverable tool result that can be fed back to the model.
var ErrDenied = errors.New("tool call denied")

// ErrPermissionRequired is returned when a tool call is waiting for interactive
// permission (ADR-043). Broker may wait in-process; if wait fails, runners treat
// this like a denial recoverable to the model.
var ErrPermissionRequired = errors.New("tool call requires permission")

var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidName reports whether name is safe for outbound provider tool protocols
// (OpenAI-compatible function names and similar).
func ValidName(name string) bool {
	return name != "" && validNamePattern.MatchString(name)
}

type Definition struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	InputSchema          json.RawMessage `json:"input_schema"`
	Risk                 string          `json:"risk"`
	DefaultTimeoutMillis int64           `json:"default_timeout_ms"`
}

type Request struct {
	CallID             string   `json:"call_id"`
	RunID              string   `json:"run_id"`
	TaskID             string   `json:"task_id"`
	PlanID             string   `json:"plan_id"`
	PlanHash           string   `json:"plan_hash"`
	StepID             string   `json:"step_id"`
	CapabilityGrantID  string   `json:"capability_grant_id,omitempty"`
	CapabilityGrantIDs []string `json:"capability_grant_ids,omitempty"`
	Actor              string   `json:"actor"`
	TraceID            string   `json:"trace_id,omitempty"`
	// Interactive is true when a TUI can answer /perm for this call (ADR-043).
	// Omit / false = fail-closed. Not an authn bit — local UDS capability flag.
	Interactive   bool            `json:"interactive,omitempty"`
	Tool          string          `json:"tool"`
	Arguments     json.RawMessage `json:"arguments"`
	TimeoutMillis int64           `json:"timeout_ms,omitempty"`
}

type ArtifactRef struct {
	ID          string `json:"artifact_id"`
	ContentHash string `json:"content_hash"`
	MediaType   string `json:"media_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type Response struct {
	CallID     string          `json:"call_id"`
	Tool       string          `json:"tool"`
	Output     json.RawMessage `json:"output,omitempty"`
	Artifact   *ArtifactRef    `json:"artifact,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
}
