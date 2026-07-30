package gatewayclient

import (
	"autozeagent.local/autozeagent/internal/approvalsubmission"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/runexecution"
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
	Plan               = corequery.Plan
	Step               = corequery.Step
	Run                = corequery.Run
	Approval           = corequery.Approval
	Session            = corequery.Session
	TranscriptMessage  = corequery.TranscriptMessage
	TranscriptToolCall = corequery.TranscriptToolCall
)

// Approval interaction types.
type (
	Action           = approvalsubmission.Action
	Prompt           = approvalsubmission.Prompt
	PromptBudget     = approvalsubmission.PromptBudget
	PromptStep       = approvalsubmission.PromptStep
	PromptCapability = approvalsubmission.PromptCapability
	ActionOption     = approvalsubmission.ActionOption
)

// Run / task control types.
type (
	TaskAction  = runexecution.TaskAction
	StartResult = runexecution.StartResult
)

// Approval action constants.
const (
	ActionAllowOnce      = approvalsubmission.ActionAllowOnce
	ActionAllowLimited   = approvalsubmission.ActionAllowLimited
	ActionAllowPlan      = approvalsubmission.ActionAllowPlan
	ActionReject         = approvalsubmission.ActionReject
	ActionRequestChanges = approvalsubmission.ActionRequestChanges
)

// Task control action constants.
const (
	TaskActionPause  = runexecution.TaskActionPause
	TaskActionResume = runexecution.TaskActionResume
	TaskActionCancel = runexecution.TaskActionCancel
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

// Task execution mode (permission posture for the task).
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
