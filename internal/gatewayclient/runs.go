package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type runListResponse struct {
	Runs []Run `json:"runs"`
}

func (c *Client) GetRun(ctx context.Context, runID RunID) (Run, error) {
	var run Run
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(string(runID)), nil, &run); err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

// RunUsage returns self + one-level child token/cost rollup for runID (ADR-039).
func (c *Client) RunUsage(ctx context.Context, runID RunID) (RunUsage, error) {
	var usage RunUsage
	path := "/v1/runs/" + url.PathEscape(string(runID)) + "/usage"
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &usage); err != nil {
		return RunUsage{}, fmt.Errorf("run usage: %w", err)
	}
	return usage, nil
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
