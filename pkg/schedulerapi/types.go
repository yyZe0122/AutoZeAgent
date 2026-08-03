// Package schedulerapi defines the stable contract for the in-process Scheduler.
// Scheduler emits chat task requests; it cannot execute tools or create Core tasks directly.
package schedulerapi

const (
	CapabilityCreate      = "scheduler.create"
	CapabilityGet         = "scheduler.get"
	CapabilityList        = "scheduler.list"
	CapabilityPause       = "scheduler.pause"
	CapabilityResume      = "scheduler.resume"
	CapabilityArchive     = "scheduler.archive"
	CapabilityClaimDue    = "scheduler.claim_due"
	CapabilityAcknowledge = "scheduler.acknowledge"
)

var Capabilities = []string{
	CapabilityCreate, CapabilityGet, CapabilityList, CapabilityPause,
	CapabilityResume, CapabilityArchive, CapabilityClaimDue, CapabilityAcknowledge,
}

const (
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusArchived = "archived"
)

const (
	MisfireSkip    = "skip"
	MisfireCatchUp = "catch_up"
	MisfireRunOnce = "run_once"
)

const (
	ExecutionModeAgent = "agent"
	ExecutionModePlan  = "plan"
)

type Job struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	SessionID       string   `json:"session_id"`
	TaskTitle       string   `json:"task_title"`
	TaskObjective   string   `json:"task_objective"`
	ExecutionMode   string   `json:"execution_mode"`
	SkillIDs        []string `json:"skill_ids,omitempty"`
	IntervalSeconds int64    `json:"interval_seconds"`
	NextRunAt       string   `json:"next_run_at"`
	TimeoutSeconds  int64    `json:"timeout_seconds"`
	MaxRetries      int      `json:"max_retries"`
	BackoffSeconds  int64    `json:"backoff_seconds"`
	MisfirePolicy   string   `json:"misfire_policy"`
	IdempotencyKey  string   `json:"idempotency_key"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

// TaskRequest is a claimed due job fire handed to scheduledtasks for chat submit.
type TaskRequest struct {
	JobID          string   `json:"job_id"`
	RunID          string   `json:"run_id"`
	LeaseID        string   `json:"lease_id"`
	SessionID      string   `json:"session_id"`
	Title          string   `json:"title"`
	Objective      string   `json:"objective"`
	ExecutionMode  string   `json:"execution_mode"`
	SkillIDs       []string `json:"skill_ids,omitempty"`
	ScheduledAt    string   `json:"scheduled_at"`
	TimeoutSeconds int64    `json:"timeout_seconds"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type CreateRequest struct {
	Name            string   `json:"name"`
	SessionID       string   `json:"session_id"`
	TaskTitle       string   `json:"task_title"`
	TaskObjective   string   `json:"task_objective"`
	ExecutionMode   string   `json:"execution_mode,omitempty"`
	SkillIDs        []string `json:"skill_ids,omitempty"`
	IntervalSeconds int64    `json:"interval_seconds"`
	NextRunAt       string   `json:"next_run_at"`
	TimeoutSeconds  int64    `json:"timeout_seconds,omitempty"`
	MaxRetries      int      `json:"max_retries,omitempty"`
	BackoffSeconds  int64    `json:"backoff_seconds,omitempty"`
	MisfirePolicy   string   `json:"misfire_policy,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key"`
}
type CreateResponse struct {
	Job Job `json:"job"`
}
type GetRequest struct {
	JobID string `json:"job_id"`
}
type GetResponse struct {
	Job Job `json:"job"`
}
type ListRequest struct {
	IncludeArchived bool `json:"include_archived,omitempty"`
}
type ListResponse struct {
	Jobs []Job `json:"jobs"`
}
type StateRequest struct {
	JobID    string `json:"job_id"`
	Reviewer string `json:"reviewer"`
	Reason   string `json:"reason"`
}
type StateResponse struct {
	Job Job `json:"job"`
}
type ClaimDueRequest struct {
	Owner        string `json:"owner"`
	Now          string `json:"now,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	LeaseSeconds int64  `json:"lease_seconds,omitempty"`
}
type ClaimDueResponse struct {
	Tasks []TaskRequest `json:"tasks"`
}
type AcknowledgeRequest struct {
	RunID      string `json:"run_id"`
	LeaseID    string `json:"lease_id"`
	CoreTaskID string `json:"core_task_id,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}
type AcknowledgeResponse struct {
	Accepted bool `json:"accepted"`
}
