package gatewayclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/runexecution"
)

type TaskSubmissionRequest struct {
	TaskID        TaskID    `json:"task_id,omitempty"`
	SessionID     SessionID `json:"session_id,omitempty"`
	PlanID        PlanID    `json:"plan_id,omitempty"`
	Title         string    `json:"title"`
	Objective     string    `json:"objective"`
	SkillIDs      []string  `json:"skill_ids,omitempty"`
	ExecutionMode string    `json:"execution_mode,omitempty"`
}

type TaskSubmissionResponse struct {
	Task            Task                   `json:"task"`
	Plan            *approval.PlanDocument `json:"plan,omitempty"`
	RunID           RunID                  `json:"run_id,omitempty"`
	PlanningPending bool                   `json:"planning_pending,omitempty"`
	// PlanID from client (plan mode) or daemon chat synthetic plan (agent mode).
	PlanID PlanID `json:"plan_id,omitempty"`
}

type taskListResponse struct {
	Tasks []Task `json:"tasks"`
}

func (c *Client) SubmitTask(ctx context.Context, request TaskSubmissionRequest) (TaskSubmissionResponse, error) {
	if strings.TrimSpace(request.Title) == "" {
		request.Title = TaskTitle(request.Objective)
	}
	mode := strings.TrimSpace(request.ExecutionMode)
	if mode == "" {
		mode = ExecutionModeAgent
	}
	request.ExecutionMode = mode
	// Client-generated plan IDs are only for plan-mode async planning.
	// Agent chat owns its synthetic plan inside the daemon.
	if request.PlanID == "" && mode == ExecutionModePlan {
		planID, err := RandomID("plan-")
		if err != nil {
			return TaskSubmissionResponse{}, err
		}
		request.PlanID = PlanID(planID)
	}
	var response TaskSubmissionResponse
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/tasks", request, &response); err != nil {
		return TaskSubmissionResponse{}, fmt.Errorf("create task: %w", err)
	}
	if response.PlanID == "" {
		response.PlanID = request.PlanID
	}
	return response, nil
}

func (c *Client) GetTask(ctx context.Context, taskID TaskID) (Task, error) {
	var task Task
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(string(taskID)), nil, &task); err != nil {
		return Task{}, fmt.Errorf("get task: %w", err)
	}
	return task, nil
}

func (c *Client) TaskUsage(ctx context.Context, taskID TaskID) (TaskUsage, error) {
	var usage TaskUsage
	path := "/v1/tasks/" + url.PathEscape(string(taskID)) + "/usage"
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &usage); err != nil {
		return TaskUsage{}, fmt.Errorf("task usage: %w", err)
	}
	return usage, nil
}

func (c *Client) ListTasks(ctx context.Context, limit int) ([]Task, error) {
	path := "/v1/tasks"
	if limit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", limit)
	}
	var response taskListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return response.Tasks, nil
}

func (c *Client) ControlTask(ctx context.Context, taskID TaskID, action TaskAction, expectedVersion uint64, reason string) (Task, error) {
	var updated Task
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(string(taskID))+"/actions", runexecution.TaskActionRequest{
		ExpectedVersion: expectedVersion,
		Action:          action,
		Reason:          strings.TrimSpace(reason),
	}, &updated); err != nil {
		return Task{}, fmt.Errorf("%s task: %w", action, err)
	}
	return updated, nil
}

func (c *Client) GetPlan(ctx context.Context, planID PlanID) (Plan, error) {
	var plan Plan
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/plans/"+url.PathEscape(string(planID)), nil, &plan); err != nil {
		return Plan{}, fmt.Errorf("get plan: %w", err)
	}
	return plan, nil
}

type planListResponse struct {
	Plans []Plan `json:"plans"`
}

func (c *Client) ListPlans(ctx context.Context, limit int) ([]Plan, error) {
	path := "/v1/plans"
	if limit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", limit)
	}
	var response planListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	return response.Plans, nil
}

func (c *Client) FindPlanForTask(ctx context.Context, taskID TaskID) (Plan, error) {
	plans, err := c.ListPlans(ctx, 100)
	if err != nil {
		return Plan{}, err
	}
	for _, plan := range plans {
		if string(plan.TaskID) == string(taskID) {
			return plan, nil
		}
	}
	return Plan{}, fmt.Errorf("no plan found for task %s", taskID)
}

func TaskTitle(objective string) string {
	runes := []rune(strings.TrimSpace(objective))
	if len(runes) <= 80 {
		return string(runes)
	}
	return string(runes[:80]) + "…"
}

func RandomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return prefix + hex.EncodeToString(value), nil
}
