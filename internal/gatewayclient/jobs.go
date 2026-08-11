package gatewayclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

type jobListResponse struct {
	Jobs []schedulerapi.Job `json:"jobs"`
}

func (c *Client) CreateJob(ctx context.Context, request schedulerapi.CreateRequest) (schedulerapi.Job, error) {
	var job schedulerapi.Job
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/jobs", request, &job); err != nil {
		return schedulerapi.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (c *Client) GetJob(ctx context.Context, jobID string) (schedulerapi.Job, error) {
	var job schedulerapi.Job
	if err := c.inner.DoJSON(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(jobID), nil, &job); err != nil {
		return schedulerapi.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (c *Client) ListJobs(ctx context.Context, includeArchived bool) ([]schedulerapi.Job, error) {
	path := "/v1/jobs"
	if includeArchived {
		path += "?include_archived=true"
	}
	var response jobListResponse
	if err := c.inner.DoJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return response.Jobs, nil
}

func (c *Client) JobAction(ctx context.Context, jobID, action, reviewer, reason string) (schedulerapi.Job, error) {
	var job schedulerapi.Job
	if err := c.inner.DoJSON(ctx, http.MethodPost, "/v1/jobs/"+url.PathEscape(jobID)+"/actions", map[string]any{
		"action": action, "reviewer": strings.TrimSpace(reviewer), "reason": strings.TrimSpace(reason),
	}, &job); err != nil {
		return schedulerapi.Job{}, fmt.Errorf("%s job: %w", action, err)
	}
	return job, nil
}
