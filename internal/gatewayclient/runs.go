package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type RunStartRequest struct {
	TaskID       TaskID `json:"task_id"`
	PlanID       PlanID `json:"plan_id"`
	PlanRevision uint64 `json:"plan_revision"`
	PlanHash     string `json:"plan_hash"`
}

type runListResponse struct {
	Runs []Run `json:"runs"`
}

func (c *Client) StartRuns(ctx context.Context, request RunStartRequest) (StartResult, error) {
	var started StartResult
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/runs", request, &started); err != nil {
		return StartResult{}, fmt.Errorf("start run: %w", err)
	}
	return started, nil
}

func (c *Client) GetRun(ctx context.Context, runID RunID) (Run, error) {
	var run Run
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(string(runID)), nil, &run); err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

func (c *Client) ListRuns(ctx context.Context, taskID TaskID, limit int) ([]Run, error) {
	// Gateway list does not filter by task_id; fetch a page and filter client-side.
	fetchLimit := limit
	if taskID != "" && fetchLimit > 0 && fetchLimit < 100 {
		fetchLimit = 100
	}
	path := "/v1/runs"
	if fetchLimit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", fetchLimit)
	}
	var response runListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	if taskID == "" {
		return response.Runs, nil
	}
	filtered := make([]Run, 0, len(response.Runs))
	for _, run := range response.Runs {
		if string(run.TaskID) == string(taskID) {
			filtered = append(filtered, run)
			if limit > 0 && len(filtered) >= limit {
				break
			}
		}
	}
	return filtered, nil
}
