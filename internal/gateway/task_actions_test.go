package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/runexecution"
)

type taskControllerStub struct {
	request runexecution.TaskActionRequest
	task    kernel.Task
	err     error
}

func (s *taskControllerStub) ControlTask(_ context.Context, request runexecution.TaskActionRequest) (kernel.Task, error) {
	s.request = request
	return s.task, s.err
}

func TestTaskActionEndpointForwardsVersionedAction(t *testing.T) {
	controller := &taskControllerStub{task: kernel.Task{
		ID: "task-1", SessionID: "session-1", Title: "Long task", Objective: "Keep working",
		State: kernel.TaskPaused, Version: 4, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}}
	api := &API{taskControls: controller}
	request := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/actions", strings.NewReader(`{"expected_version":3,"action":"pause","reason":"operator pause"}`))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if controller.request.TaskID != "task-1" || controller.request.ExpectedVersion != 3 || controller.request.Action != runexecution.TaskActionPause {
		t.Fatalf("request = %+v", controller.request)
	}
	var task corequery.Task
	if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.ID != "task-1" || task.State != string(kernel.TaskPaused) || task.Version != 4 {
		t.Fatalf("task = %+v", task)
	}
}

func TestTaskActionEndpointMapsConflict(t *testing.T) {
	controller := &taskControllerStub{err: applicationerror.Wrap(applicationerror.CodeConflict, false, errors.New("version conflict"))}
	api := &API{taskControls: controller}
	request := httptest.NewRequest(http.MethodPost, "/v1/tasks/task-1/actions", strings.NewReader(`{"expected_version":2,"action":"resume"}`))
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
