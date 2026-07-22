// Package tasksubmission coordinates the user-facing task creation use case.
// Domain state changes remain owned by Kernel and planning remains owned by Planner.
package tasksubmission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/skillcatalog"
)

const defaultMaxSkillContextBytes = 64 * 1024

var (
	ErrConflict = errors.New("task submission conflicts with an existing task")
	ErrPlanning = errors.New("task submission planning did not complete")
)

type Repository interface {
	CreateSession(context.Context, kernel.SessionID, time.Time) (kernel.Session, error)
	GetSession(context.Context, kernel.SessionID) (kernel.Session, error)
	CreateTaskWithSkillSnapshot(context.Context, kernel.TaskID, kernel.SessionID, string, string, []string, string, time.Time) (kernel.Task, error)
	GetTask(context.Context, kernel.TaskID) (kernel.Task, error)
	GetTaskSkillSnapshot(context.Context, kernel.TaskID) (kernel.TaskSkillSnapshot, error)
}

type Planner interface {
	PlanTask(context.Context, kernel.Task, kernel.PlanID, uint64) (kernel.Task, approval.PlanDocument, error)
}

type Config struct {
	Repository           Repository
	Planner              Planner
	Skills               *skillcatalog.Catalog
	MaxSkillContextBytes int
	Now                  func() time.Time
	NewID                func(string) (string, error)
}

type Service struct {
	repository           Repository
	planner              Planner
	skills               *skillcatalog.Catalog
	maxSkillContextBytes int
	now                  func() time.Time
	newID                func(string) (string, error)
}

type Request struct {
	TaskID        kernel.TaskID
	SessionID     kernel.SessionID
	PlanID        kernel.PlanID
	Title         string
	Objective     string
	SkillIDs      []string
	EnsureSession bool
	AllowExisting bool
}

type Result struct {
	Task kernel.Task
	Plan *approval.PlanDocument
}

func New(config Config) (*Service, error) {
	if config.Repository == nil {
		return nil, errors.New("task submission repository is required")
	}
	if config.MaxSkillContextBytes < 0 {
		return nil, errors.New("task submission skill context byte budget cannot be negative")
	}
	if config.MaxSkillContextBytes == 0 {
		config.MaxSkillContextBytes = defaultMaxSkillContextBytes
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.NewID == nil {
		config.NewID = randomID
	}
	return &Service{
		repository:           config.Repository,
		planner:              config.Planner,
		skills:               config.Skills,
		maxSkillContextBytes: config.MaxSkillContextBytes,
		now:                  config.Now,
		newID:                config.NewID,
	}, nil
}

func (s *Service) Submit(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("task submission context is required")
	}
	request.Title = strings.TrimSpace(request.Title)
	request.Objective = strings.TrimSpace(request.Objective)
	if request.Title == "" || request.Objective == "" {
		return Result{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: task title and objective are required", kernel.ErrInvalidAggregate))
	}

	var err error
	if request.SessionID == "" {
		if !request.EnsureSession {
			return Result{}, applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, fmt.Errorf("%w: session ID is required", kernel.ErrInvalidAggregate))
		}
		request.SessionID, err = s.nextSessionID()
		if err != nil {
			return Result{}, err
		}
	}
	if request.TaskID == "" {
		request.TaskID, err = s.nextTaskID()
		if err != nil {
			return Result{}, err
		}
	}
	if request.EnsureSession {
		if err := s.ensureSession(ctx, request.SessionID); err != nil {
			return Result{}, classifyError(err)
		}
	}

	var task kernel.Task
	if request.AllowExisting {
		task, err = s.repository.GetTask(ctx, request.TaskID)
		if err == nil {
			return s.continueSubmission(ctx, task, request)
		}
		if !errors.Is(err, kernel.ErrNotFound) {
			return Result{}, classifyError(err)
		}
	}

	skillIDs, skillContext, err := s.resolveSkills(request.SkillIDs)
	if err != nil {
		return Result{}, classifyError(err)
	}
	request.SkillIDs = skillIDs
	task, err = s.repository.CreateTaskWithSkillSnapshot(
		ctx, request.TaskID, request.SessionID, request.Title, request.Objective,
		skillIDs, skillContext, s.now(),
	)
	if errors.Is(err, kernel.ErrAlreadyExists) && request.AllowExisting {
		task, err = s.repository.GetTask(ctx, request.TaskID)
		if err == nil {
			return s.continueSubmission(ctx, task, request)
		}
	}
	if err != nil {
		return Result{}, classifyError(err)
	}
	return s.planTask(ctx, task, request)
}

func (s *Service) continueSubmission(ctx context.Context, task kernel.Task, request Request) (Result, error) {
	snapshot, err := s.repository.GetTaskSkillSnapshot(ctx, task.ID)
	if err != nil {
		return Result{}, classifyError(err)
	}
	if !sameTask(task, snapshot, request) {
		return Result{}, applicationerror.Wrap(applicationerror.CodeConflict, false, fmt.Errorf("%w: task %s", ErrConflict, request.TaskID))
	}
	return s.planTask(ctx, task, request)
}

func (s *Service) planTask(ctx context.Context, task kernel.Task, request Request) (Result, error) {
	result := Result{Task: task}
	switch task.State {
	case kernel.TaskCreated:
		if s.planner == nil {
			return result, nil
		}
	case kernel.TaskPlanning:
		if s.planner == nil {
			return result, planningError(task.ID, errors.New("planner is unavailable"))
		}
	case kernel.TaskWaitingApproval:
		// A retried idempotent submission may observe a plan committed earlier.
		return result, nil
	default:
		return result, applicationerror.Wrap(applicationerror.CodeConflict, false, fmt.Errorf("%w: task %s is already %s", ErrConflict, task.ID, task.State))
	}
	var err error
	if request.PlanID == "" {
		request.PlanID, err = s.nextPlanID()
		if err != nil {
			return result, planningError(task.ID, err)
		}
	}
	plannedTask, plan, planErr := s.planner.PlanTask(ctx, task, request.PlanID, 1)
	result.Task = plannedTask
	if planErr != nil {
		return result, planningError(task.ID, planErr)
	}
	result.Plan = &plan
	return result, nil
}

func (s *Service) resolveSkills(requested []string) ([]string, string, error) {
	if len(requested) == 0 {
		return nil, "", nil
	}
	if s.skills == nil {
		return nil, "", fmt.Errorf("%w: skill catalog is unavailable", kernel.ErrInvalidAggregate)
	}
	selected, err := s.skills.Select(requested)
	if err != nil {
		return nil, "", fmt.Errorf("%w: select skills: %w", kernel.ErrInvalidAggregate, err)
	}
	contextText, err := s.skills.Context(selected, s.maxSkillContextBytes)
	if err != nil {
		return nil, "", fmt.Errorf("%w: render skill context: %w", kernel.ErrInvalidAggregate, err)
	}
	ids := make([]string, len(selected))
	for index, skill := range selected {
		ids[index] = skill.ID
	}
	return ids, contextText, nil
}

func classifyError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, kernel.ErrInvalidAggregate):
		return applicationerror.Wrap(applicationerror.CodeInvalidRequest, false, err)
	case errors.Is(err, ErrConflict), errors.Is(err, kernel.ErrAlreadyExists), errors.Is(err, kernel.ErrSessionClosed):
		return applicationerror.Wrap(applicationerror.CodeConflict, false, err)
	case errors.Is(err, kernel.ErrNotFound):
		return applicationerror.Wrap(applicationerror.CodeNotFound, false, err)
	default:
		return err
	}
}

func planningError(taskID kernel.TaskID, err error) error {
	return applicationerror.Wrap(
		applicationerror.CodePlanningPending,
		true,
		fmt.Errorf("%w for task %s: %w", ErrPlanning, taskID, err),
	)
}

func (s *Service) ensureSession(ctx context.Context, id kernel.SessionID) error {
	session, err := s.repository.GetSession(ctx, id)
	if err == nil {
		if session.State != kernel.SessionActive {
			return fmt.Errorf("%w: %s", kernel.ErrSessionClosed, id)
		}
		return nil
	}
	if !errors.Is(err, kernel.ErrNotFound) {
		return err
	}
	_, err = s.repository.CreateSession(ctx, id, s.now())
	if errors.Is(err, kernel.ErrAlreadyExists) {
		session, err = s.repository.GetSession(ctx, id)
		if err == nil && session.State != kernel.SessionActive {
			return fmt.Errorf("%w: %s", kernel.ErrSessionClosed, id)
		}
	}
	return err
}

func (s *Service) nextSessionID() (kernel.SessionID, error) {
	value, err := s.newID("session-")
	return kernel.SessionID(value), err
}

func (s *Service) nextTaskID() (kernel.TaskID, error) {
	value, err := s.newID("task-")
	return kernel.TaskID(value), err
}

func (s *Service) nextPlanID() (kernel.PlanID, error) {
	value, err := s.newID("plan-")
	return kernel.PlanID(value), err
}

func sameTask(task kernel.Task, snapshot kernel.TaskSkillSnapshot, request Request) bool {
	return task.SessionID == request.SessionID &&
		task.Title == request.Title &&
		task.Objective == request.Objective &&
		slices.Equal(snapshot.SkillIDs, request.SkillIDs)
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate %s ID: %w", strings.TrimSuffix(prefix, "-"), err)
	}
	return prefix + hex.EncodeToString(value), nil
}
