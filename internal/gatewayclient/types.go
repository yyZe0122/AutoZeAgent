package gatewayclient

import (
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/taskcontrol"
)

// Identity and DTO aliases — monorepo client reuses domain/read types.
// If this package is ever split into a standalone SDK, replace with local DTOs.
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
	RunUsage           = corequery.RunUsage
	UsageTotals        = corequery.UsageTotals
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

// Task control action constants.
const (
	TaskActionPause  = taskcontrol.TaskActionPause
	TaskActionResume = taskcontrol.TaskActionResume
	TaskActionCancel = taskcontrol.TaskActionCancel
)

// Task state strings as returned by the gateway (corequery uses plain strings).
const (
	TaskStateCreated   = string(kernel.TaskCreated)
	TaskStateRunning   = string(kernel.TaskRunning)
	TaskStatePaused    = string(kernel.TaskPaused)
	TaskStateCompleted = string(kernel.TaskCompleted)
	TaskStateFailed    = string(kernel.TaskFailed)
	TaskStateCancelled = string(kernel.TaskCancelled)

	// Legacy strings for old core.db rows (display only).
	TaskStatePlanning        = string(kernel.TaskPlanning)
	TaskStateWaitingApproval = string(kernel.TaskWaitingApproval)
	TaskStateApproved        = string(kernel.TaskApproved)
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
