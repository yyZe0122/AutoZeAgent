// Package taskcontrol applies task lifecycle controls (pause/resume/cancel).
// Chat execution lives in chatsession; this package only mutates task state and
// interrupts in-flight chat via ChatInterrupter.
package taskcontrol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/applicationerror"
	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/runlog"
)

var ErrInvalidRequest = errors.New("invalid task control request")

// ChatInterrupter cancels in-flight chatsession agent.Run for a task.
// Optional; when nil, ControlTask only updates durable task state.
type ChatInterrupter interface {
	Interrupt(kernel.TaskID)
}

type Config struct {
	DB         *sql.DB
	Approvals  *approval.Repository
	Repository *kernel.Repository
	Chat       ChatInterrupter
	Now        func() time.Time
}

type Service struct {
	db         *sql.DB
	approvals  *approval.Repository
	repository *kernel.Repository
	chat       ChatInterrupter
	now        func() time.Time
}

type TaskAction string

const (
	TaskActionPause  TaskAction = "pause"
	TaskActionResume TaskAction = "resume"
	TaskActionCancel TaskAction = "cancel"
)

type TaskActionRequest struct {
	TaskID          kernel.TaskID `json:"-"`
	ExpectedVersion uint64        `json:"expected_version"`
	Action          TaskAction    `json:"action"`
	Reason          string        `json:"reason,omitempty"`
}

func New(config Config) (*Service, error) {
	if config.DB == nil || config.Approvals == nil || config.Repository == nil {
		return nil, errors.New("task control requires db, approval repository, and kernel repository")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		db: config.DB, approvals: config.Approvals, repository: config.Repository,
		chat: config.Chat, now: config.Now,
	}, nil
}

// ControlTask applies pause/resume/cancel. Pause and cancel interrupt in-flight
// chat via ChatInterrupter; cancel also revokes task grants.
func (s *Service) ControlTask(ctx context.Context, request TaskActionRequest) (kernel.Task, error) {
	if ctx == nil {
		return kernel.Task{}, errors.New("task action context is required")
	}
	request.TaskID = kernel.TaskID(strings.TrimSpace(string(request.TaskID)))
	request.Reason = strings.TrimSpace(request.Reason)
	if request.TaskID == "" || request.ExpectedVersion == 0 {
		return kernel.Task{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: task and expected version are required", ErrInvalidRequest))
	}

	var (
		task kernel.Task
		err  error
	)
	switch request.Action {
	case TaskActionPause:
		task, err = s.repository.TransitionTask(ctx, request.TaskID, request.ExpectedVersion, kernel.TaskPaused, request.Reason, s.now())
	case TaskActionResume:
		task, err = s.repository.TransitionTask(ctx, request.TaskID, request.ExpectedVersion, kernel.TaskRunning, request.Reason, s.now())
	case TaskActionCancel:
		task, err = s.repository.CancelTask(ctx, request.TaskID, request.ExpectedVersion, request.Reason, s.now())
	default:
		return kernel.Task{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: unsupported task action %q", ErrInvalidRequest, request.Action))
	}
	if err != nil {
		slog.Warn("task control failed", runlog.Attrs("taskcontrol", string(request.Action), "failed", runlog.IDs{
			TaskID: string(request.TaskID),
		}, "expected_version", request.ExpectedVersion, "error", err)...)
		return kernel.Task{}, classifyTaskActionError(err)
	}
	if request.Action == TaskActionPause || request.Action == TaskActionCancel {
		if s.chat != nil {
			s.chat.Interrupt(request.TaskID)
		}
	}
	if request.Action == TaskActionCancel {
		if err := s.approvals.RevokeTaskGrants(context.WithoutCancel(ctx), request.TaskID, s.now()); err != nil {
			slog.Error("task control revoke grants failed", runlog.Attrs("taskcontrol", "cancel", "failed", runlog.IDs{
				SessionID: string(task.SessionID), TaskID: string(task.ID),
			}, "error", err)...)
			return task, err
		}
	}
	slog.Info("task control applied", runlog.Attrs("taskcontrol", string(request.Action), "succeeded", runlog.IDs{
		SessionID: string(task.SessionID), TaskID: string(task.ID),
	}, "task_state", string(task.State), "reason", request.Reason)...)
	return task, nil
}

func classifyTaskActionError(err error) error {
	switch {
	case errors.Is(err, kernel.ErrNotFound):
		return applicationerror.Wrap(applicationerror.CodeNotFound, false, err)
	case errors.Is(err, kernel.ErrVersionConflict), errors.Is(err, kernel.ErrInvalidTransition):
		return applicationerror.Wrap(applicationerror.CodeConflict, false, err)
	case errors.Is(err, kernel.ErrInvalidAggregate):
		return applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, err)
	default:
		return err
	}
}
