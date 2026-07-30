package tui

import (
	"context"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/internal/modelstream"
	"autozeagent.local/autozeagent/pkg/eventapi"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

// Gateway is the narrow local-gateway surface used by the TUI.
// Production uses *gatewayclient.Client; tests may inject a fake.
type Gateway interface {
	StreamEvents(ctx context.Context, after uint64, emit func(eventapi.Envelope) error) error
	StreamModelEvents(ctx context.Context, sessionID gatewayclient.SessionID, runID gatewayclient.RunID, emit func(modelstream.Envelope) error) error

	Health(ctx context.Context) (gatewayclient.Health, error)
	ModelConfig(ctx context.Context) (gatewayclient.ModelConfig, error)
	SetModelConfig(ctx context.Context, model string) (gatewayclient.ModelConfig, error)

	ListSessions(ctx context.Context, limit int) ([]gatewayclient.Session, error)
	GetSession(ctx context.Context, id gatewayclient.SessionID) (gatewayclient.Session, error)
	SessionMessages(ctx context.Context, id gatewayclient.SessionID, limit int) ([]gatewayclient.TranscriptMessage, error)
	TaskMessages(ctx context.Context, id gatewayclient.TaskID, limit int) ([]gatewayclient.TranscriptMessage, error)

	ListTasks(ctx context.Context, limit int) ([]gatewayclient.Task, error)
	GetTask(ctx context.Context, id gatewayclient.TaskID) (gatewayclient.Task, error)
	TaskUsage(ctx context.Context, id gatewayclient.TaskID) (gatewayclient.TaskUsage, error)
	SubmitTask(ctx context.Context, req gatewayclient.TaskSubmissionRequest) (gatewayclient.TaskSubmissionResponse, error)
	ControlTask(ctx context.Context, id gatewayclient.TaskID, action gatewayclient.TaskAction, expectedVersion uint64, reason string) (gatewayclient.Task, error)

	GetPlan(ctx context.Context, id gatewayclient.PlanID) (gatewayclient.Plan, error)
	FindPlanForTask(ctx context.Context, taskID gatewayclient.TaskID) (gatewayclient.Plan, error)

	ListRuns(ctx context.Context, taskID gatewayclient.TaskID, limit int) ([]gatewayclient.Run, error)
	StartRuns(ctx context.Context, req gatewayclient.RunStartRequest) (gatewayclient.StartResult, error)

	ApprovalPrompt(ctx context.Context, planID gatewayclient.PlanID, stepID gatewayclient.StepID) (gatewayclient.Prompt, error)
	DecideApproval(ctx context.Context, prompt gatewayclient.Prompt, stepID gatewayclient.StepID, action gatewayclient.Action, decidedBy, reason string) (gatewayclient.Approval, error)

	ListJobs(ctx context.Context, includeArchived bool) ([]schedulerapi.Job, error)
}
