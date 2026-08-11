package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
)

type jobServiceStub struct {
	createRequest schedulerapi.CreateRequest
	listArchived  bool
	stateRequest  schedulerapi.StateRequest
	state         string
	job           schedulerapi.Job
	jobs          []schedulerapi.Job
	err           error
}

func (s *jobServiceStub) Create(_ context.Context, request schedulerapi.CreateRequest) (schedulerapi.Job, error) {
	s.createRequest = request
	return s.job, s.err
}

func (s *jobServiceStub) Get(context.Context, string) (schedulerapi.Job, error) {
	return s.job, s.err
}

func (s *jobServiceStub) List(_ context.Context, includeArchived bool) ([]schedulerapi.Job, error) {
	s.listArchived = includeArchived
	return s.jobs, s.err
}

func (s *jobServiceStub) ChangeState(_ context.Context, request schedulerapi.StateRequest, state string) (schedulerapi.Job, error) {
	s.stateRequest = request
	s.state = state
	return s.job, s.err
}

func TestJobCreateEndpointForwardsRequest(t *testing.T) {
	service := &jobServiceStub{job: schedulerapi.Job{ID: "job-1", Name: "heartbeat", Status: schedulerapi.StatusActive}}
	api := &API{jobs: service}
	request := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{
		"name":"heartbeat","session_id":"session-1","task_title":"Heartbeat",
		"task_objective":"Inspect progress","interval_seconds":60,"idempotency_key":"heartbeat/session-1"
	}`))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.createRequest.Name != "heartbeat" || service.createRequest.SessionID != "session-1" {
		t.Fatalf("create request = %+v", service.createRequest)
	}
	var job schedulerapi.Job
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ID != "job-1" {
		t.Fatalf("job = %+v", job)
	}
}

func TestJobCreateEndpointForwardsModeAndSkills(t *testing.T) {
	service := &jobServiceStub{job: schedulerapi.Job{ID: "job-2", ExecutionMode: "plan"}}
	api := &API{jobs: service}
	request := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{
		"name":"review","session_id":"session-2","task_title":"Review",
		"task_objective":"Read only","execution_mode":"plan","skill_ids":["demo"],
		"interval_seconds":120,"idempotency_key":"review/session-2"
	}`))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.createRequest.ExecutionMode != "plan" {
		t.Fatalf("mode = %q", service.createRequest.ExecutionMode)
	}
	if len(service.createRequest.SkillIDs) != 1 || service.createRequest.SkillIDs[0] != "demo" {
		t.Fatalf("skills = %v", service.createRequest.SkillIDs)
	}
}

func TestJobListEndpointForwardsArchiveFilter(t *testing.T) {
	service := &jobServiceStub{jobs: []schedulerapi.Job{{ID: "job-1"}}}
	api := &API{jobs: service}
	request := httptest.NewRequest(http.MethodGet, "/v1/jobs?include_archived=true", nil)
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !service.listArchived {
		t.Fatal("include_archived was not forwarded")
	}
	var result struct {
		Jobs []schedulerapi.Job `json:"jobs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Jobs) != 1 || result.Jobs[0].ID != "job-1" {
		t.Fatalf("jobs = %+v", result.Jobs)
	}
}

func TestJobActionEndpointMapsCancelToArchived(t *testing.T) {
	service := &jobServiceStub{job: schedulerapi.Job{ID: "job-1", Status: schedulerapi.StatusArchived}}
	api := &API{jobs: service}
	request := httptest.NewRequest(http.MethodPost, "/v1/jobs/job-1/actions", strings.NewReader(`{
		"action":"cancel","reviewer":"operator","reason":"no longer needed"
	}`))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.state != schedulerapi.StatusArchived {
		t.Fatalf("state = %q, want %q", service.state, schedulerapi.StatusArchived)
	}
	if service.stateRequest.JobID != "job-1" || service.stateRequest.Reviewer != "operator" || service.stateRequest.Reason != "no longer needed" {
		t.Fatalf("state request = %+v", service.stateRequest)
	}
}

func TestJobActionEndpointRejectsInvalidAction(t *testing.T) {
	api := &API{jobs: &jobServiceStub{}}
	request := httptest.NewRequest(http.MethodPost, "/v1/jobs/job-1/actions", strings.NewReader(`{"action":"restart","reason":"test"}`))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestJobEndpointsReportUnavailableService(t *testing.T) {
	api := &API{}
	for _, target := range []string{"/v1/jobs", "/v1/jobs/job-1", "/v1/jobs/job-1/actions"} {
		method := http.MethodGet
		if strings.HasSuffix(target, "/actions") {
			method = http.MethodPost
		}
		request := httptest.NewRequest(method, target, strings.NewReader(`{"action":"pause","reason":"test"}`))
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, body = %s", target, response.Code, response.Body.String())
		}
	}
}

func TestJobActionEndpointMapsMissingJob(t *testing.T) {
	api := &API{jobs: &jobServiceStub{err: schedulerapi.ErrNotFound}}
	request := httptest.NewRequest(http.MethodPost, "/v1/jobs/missing/actions", strings.NewReader(`{"action":"pause","reason":"test"}`))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
