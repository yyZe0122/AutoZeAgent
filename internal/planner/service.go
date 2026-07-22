package planner

import (
	"context"
	"errors"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/kernel"
)

type TaskRepository interface {
	GetTaskSkillSnapshot(context.Context, kernel.TaskID) (kernel.TaskSkillSnapshot, error)
	TransitionTask(context.Context, kernel.TaskID, uint64, kernel.TaskState, string, time.Time) (kernel.Task, error)
	CreatePlanForApproval(context.Context, kernel.PlanID, kernel.TaskID, uint64, uint64, string, []byte, []kernel.PlanStepDraft, string, time.Time) (kernel.Plan, kernel.Task, error)
}

type Service struct {
	repository TaskRepository
	planner    *Planner
	now        func() time.Time
}

func NewService(repository TaskRepository, planner *Planner) (*Service, error) {
	if repository == nil || planner == nil {
		return nil, errors.New("planning service repository and planner are required")
	}
	return &Service{repository: repository, planner: planner, now: func() time.Time { return time.Now().UTC() }}, nil
}

// PlanTask leaves provider failures in TaskPlanning. That state is deliberately
// recoverable after a restart; no plan, grant, or tool execution is created.
func (s *Service) PlanTask(ctx context.Context, task kernel.Task, planID kernel.PlanID, revision uint64) (kernel.Task, approval.PlanDocument, error) {
	snapshot, err := s.repository.GetTaskSkillSnapshot(ctx, task.ID)
	if err != nil {
		return task, approval.PlanDocument{}, err
	}
	switch task.State {
	case kernel.TaskCreated:
		task, err = s.repository.TransitionTask(ctx, task.ID, task.Version, kernel.TaskPlanning, "planning started", s.now())
		if err != nil {
			return task, approval.PlanDocument{}, err
		}
	case kernel.TaskPlanning:
	default:
		return task, approval.PlanDocument{}, &kernel.TransitionError{Aggregate: "task", From: string(task.State), To: string(kernel.TaskPlanning)}
	}
	plan, err := s.planner.Generate(ctx, GenerateRequest{
		TaskID: task.ID, PlanID: planID, Revision: revision, Objective: task.Objective, SkillContext: snapshot.Instructions,
	})
	if err != nil {
		return task, approval.PlanDocument{}, err
	}
	planDocument, err := plan.CanonicalJSON()
	if err != nil {
		return task, approval.PlanDocument{}, err
	}
	planHash, err := plan.Hash()
	if err != nil {
		return task, approval.PlanDocument{}, err
	}
	drafts := make([]kernel.PlanStepDraft, len(plan.Steps))
	for i, step := range plan.Steps {
		drafts[i] = kernel.PlanStepDraft{ID: step.StepID, Position: step.Position, Title: step.Title, EffectLevel: string(step.Risk)}
	}
	_, updatedTask, err := s.repository.CreatePlanForApproval(
		ctx, plan.PlanID, plan.TaskID, task.Version, plan.Revision, planHash, planDocument, drafts,
		"plan awaits explicit approval", s.now(),
	)
	if err != nil {
		return task, approval.PlanDocument{}, err
	}
	return updatedTask, plan, nil
}
