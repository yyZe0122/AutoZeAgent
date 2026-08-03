package gatewayclient

import (
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/taskcontrol"
)

// Identity aliases — client-facing IDs without importing kernel.
type (
	SessionID = kernel.SessionID
	TaskID    = kernel.TaskID
	PlanID    = kernel.PlanID
	StepID    = kernel.StepID
	RunID     = kernel.RunID
)

// Query projections served by the gateway.
type (
	Task               = corequery.Task
	TaskUsage          = corequery.TaskUsage
	TaskContext        = corequery.TaskContext
	Plan               = corequery.Plan
	Step               = corequery.Step
	Run                = corequery.Run
	Approval           = corequery.Approval
	Session            = corequery.Session
	TranscriptMessage  = corequery.TranscriptMessage
	TranscriptToolCall = corequery.TranscriptToolCall
)

// Task control types (pause/resume/cancel).
type (
	TaskAction = taskcontrol.TaskAction
)

// StartResult is the legacy JSON body shape for POST /v1/runs (HTTP 410 Gone).
type StartResult struct {
	TaskID TaskID  `json:"task_id"`
	PlanID PlanID  `json:"plan_id"`
	RunIDs []RunID `json:"run_ids"`
}

// Task control action constants.
const (
	TaskActionPause  = taskcontrol.TaskActionPause
	TaskActionResume = taskcontrol.TaskActionResume
	TaskActionCancel = taskcontrol.TaskActionCancel
)

// Task state strings as returned by the gateway (corequery uses plain strings).
const (
	TaskStateCreated         = string(kernel.TaskCreated)
	TaskStatePlanning        = string(kernel.TaskPlanning)
	TaskStateWaitingApproval = string(kernel.TaskWaitingApproval)
	TaskStateApproved        = string(kernel.TaskApproved)
	TaskStateRunning         = string(kernel.TaskRunning)
	TaskStatePaused          = string(kernel.TaskPaused)
	TaskStateCompleted       = string(kernel.TaskCompleted)
	TaskStateFailed          = string(kernel.TaskFailed)
	TaskStateCancelled       = string(kernel.TaskCancelled)
)

// Task execution mode (permission posture: agent=build write, plan=read-only chat).
const (
	ExecutionModeAgent = string(kernel.ExecutionModeAgent)
	ExecutionModePlan  = string(kernel.ExecutionModePlan)
)

// Run state strings as returned by the gateway.
const (
	RunStateCreated   = string(kernel.RunCreated)
	RunStateRunning   = string(kernel.RunRunning)
	RunStateCompleted = string(kernel.RunCompleted)
	RunStateFailed    = string(kernel.RunFailed)
	RunStateCancelled = string(kernel.RunCancelled)
)
