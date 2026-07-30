package chatsession

import (
	"context"

	"autozeagent.local/autozeagent/internal/tasksubmission"
)

// AsTaskChat adapts Service to tasksubmission.ChatStarter.
func AsTaskChat(s *Service) tasksubmission.ChatStarter {
	if s == nil {
		return nil
	}
	return chatAdapter{s: s}
}

type chatAdapter struct {
	s *Service
}

func (a chatAdapter) StartChat(ctx context.Context, req tasksubmission.ChatStartRequest) (tasksubmission.ChatStartResult, error) {
	result, err := a.s.StartChat(ctx, StartRequest{
		Task: req.Task, Actor: req.Actor, TraceID: req.TraceID, UserText: req.UserText,
	})
	if err != nil {
		return tasksubmission.ChatStartResult{}, err
	}
	return tasksubmission.ChatStartResult{
		Task: result.Task, RunID: result.RunID, PlanID: result.PlanID,
	}, nil
}
