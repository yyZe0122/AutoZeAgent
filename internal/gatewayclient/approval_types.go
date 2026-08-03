package gatewayclient

// Approval interaction types kept for API compatibility after interactive
// plan approval was removed. Prompt/decide endpoints return HTTP 410.

type Action string

const (
	ActionAllowOnce      Action = "allow_once"
	ActionAllowLimited   Action = "allow_limited"
	ActionAllowPlan      Action = "allow_plan"
	ActionReject         Action = "reject"
	ActionRequestChanges Action = "request_changes"
)

type PromptBudget struct {
	MaxTokens         int64 `json:"max_tokens"`
	MaxCostMicros     int64 `json:"max_cost_micros"`
	MaxDurationMillis int64 `json:"max_duration_millis"`
}

type PromptCapability struct {
	Capability string   `json:"capability"`
	Paths      []string `json:"paths,omitempty"`
}

type PromptStep struct {
	StepID       string             `json:"step_id"`
	Title        string             `json:"title"`
	Risk         string             `json:"risk"`
	Capabilities []PromptCapability `json:"capabilities,omitempty"`
}

type ActionOption struct {
	Action      Action `json:"action"`
	Description string `json:"description,omitempty"`
	StepID      StepID `json:"step_id,omitempty"`
}

// Prompt matches the former approvalsubmission shape used by clients/TUI.
type Prompt struct {
	TaskID    TaskID         `json:"task_id"`
	PlanID    PlanID         `json:"plan_id"`
	Revision  uint64         `json:"revision"`
	PlanHash  string         `json:"plan_hash"`
	Objective string         `json:"objective"`
	Budget    PromptBudget   `json:"budget"`
	Steps     []PromptStep   `json:"steps"`
	Actions   []ActionOption `json:"actions"`
	Options   []ActionOption `json:"options,omitempty"`
}
