package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/internal/modelstream"
	"autozeagent.local/autozeagent/internal/platform/paths"
	"autozeagent.local/autozeagent/pkg/eventapi"
	"autozeagent.local/autozeagent/pkg/schedulerapi"
)

// fakeGateway is a minimal Gateway for unit tests.
type fakeGateway struct {
	tasks  []gatewayclient.Task
	jobs   []schedulerapi.Job
	health gatewayclient.Health
	model  gatewayclient.ModelConfig
}

func (f *fakeGateway) StreamEvents(context.Context, uint64, func(eventapi.Envelope) error) error {
	return context.Canceled
}

func (f *fakeGateway) StreamModelEvents(context.Context, gatewayclient.SessionID, gatewayclient.RunID, func(modelstream.Envelope) error) error {
	return context.Canceled
}

func (f *fakeGateway) ListSessions(context.Context, int) ([]gatewayclient.Session, error) {
	return nil, nil
}

func (f *fakeGateway) GetSession(context.Context, gatewayclient.SessionID) (gatewayclient.Session, error) {
	return gatewayclient.Session{}, errors.New("not found")
}

func (f *fakeGateway) SessionMessages(context.Context, gatewayclient.SessionID, int) ([]gatewayclient.TranscriptMessage, error) {
	return nil, nil
}

func (f *fakeGateway) TaskMessages(context.Context, gatewayclient.TaskID, int) ([]gatewayclient.TranscriptMessage, error) {
	return nil, nil
}

func (f *fakeGateway) Health(context.Context) (gatewayclient.Health, error) {
	return f.health, nil
}

func (f *fakeGateway) ModelConfig(context.Context) (gatewayclient.ModelConfig, error) {
	return f.model, nil
}

func (f *fakeGateway) SetModelConfig(_ context.Context, model string) (gatewayclient.ModelConfig, error) {
	f.model.Model = model
	return f.model, nil
}

func (f *fakeGateway) ListTasks(context.Context, int) ([]gatewayclient.Task, error) {
	return f.tasks, nil
}

func (f *fakeGateway) GetTask(_ context.Context, id gatewayclient.TaskID) (gatewayclient.Task, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return gatewayclient.Task{}, errors.New("not found")
}

func (f *fakeGateway) TaskUsage(_ context.Context, id gatewayclient.TaskID) (gatewayclient.TaskUsage, error) {
	return gatewayclient.TaskUsage{TaskID: id}, nil
}

func (f *fakeGateway) SubmitTask(context.Context, gatewayclient.TaskSubmissionRequest) (gatewayclient.TaskSubmissionResponse, error) {
	return gatewayclient.TaskSubmissionResponse{}, errors.New("not implemented")
}

func (f *fakeGateway) ControlTask(context.Context, gatewayclient.TaskID, gatewayclient.TaskAction, uint64, string) (gatewayclient.Task, error) {
	return gatewayclient.Task{}, errors.New("not implemented")
}

func (f *fakeGateway) GetPlan(context.Context, gatewayclient.PlanID) (gatewayclient.Plan, error) {
	return gatewayclient.Plan{}, errors.New("not found")
}

func (f *fakeGateway) FindPlanForTask(context.Context, gatewayclient.TaskID) (gatewayclient.Plan, error) {
	return gatewayclient.Plan{}, errors.New("not found")
}

func (f *fakeGateway) ListRuns(context.Context, gatewayclient.TaskID, int) ([]gatewayclient.Run, error) {
	return nil, nil
}

func (f *fakeGateway) StartRuns(context.Context, gatewayclient.RunStartRequest) (gatewayclient.StartResult, error) {
	return gatewayclient.StartResult{}, errors.New("not implemented")
}

func (f *fakeGateway) ApprovalPrompt(context.Context, gatewayclient.PlanID, gatewayclient.StepID) (gatewayclient.Prompt, error) {
	return gatewayclient.Prompt{}, errors.New("not found")
}

func (f *fakeGateway) DecideApproval(context.Context, gatewayclient.Prompt, gatewayclient.StepID, gatewayclient.Action, string, string) (gatewayclient.Approval, error) {
	return gatewayclient.Approval{}, errors.New("not implemented")
}

func (f *fakeGateway) ListJobs(context.Context, bool) ([]schedulerapi.Job, error) {
	return f.jobs, nil
}

func TestRefreshCmdUsesGateway(t *testing.T) {
	gw := &fakeGateway{
		tasks: []gatewayclient.Task{{
			ID: "task-1", Title: "t", State: gatewayclient.TaskStateRunning, Version: 1,
		}},
		health: gatewayclient.Health{OK: true},
		model:  gatewayclient.ModelConfig{Model: "p/m", Models: []string{"p/m"}},
	}
	gw.health.Core.Runtime.DataDir = "/tmp/aze-data"

	m := newModel(paths.ModeUser, gw)
	msg := m.refreshCmd(1, refreshFull)()
	done, ok := msg.(refreshDoneMsg)
	if !ok {
		t.Fatalf("msg type %T", msg)
	}
	if done.err != nil {
		t.Fatal(done.err)
	}
	if len(done.tasks) != 1 || done.tasks[0].ID != "task-1" {
		t.Fatalf("tasks = %#v", done.tasks)
	}

	statusMsg := m.loadStatusCmd()()
	status, ok := statusMsg.(statusDoneMsg)
	if !ok {
		t.Fatalf("status type %T", statusMsg)
	}
	if status.err != nil || !status.health.OK || status.model.Model != "p/m" {
		t.Fatalf("status = %#v", status)
	}
	if status.health.Core.Runtime.DataDir != "/tmp/aze-data" {
		t.Fatalf("dataDir = %q", status.health.Core.Runtime.DataDir)
	}
}

func TestModelUpdateRefresh(t *testing.T) {
	gw := &fakeGateway{
		tasks: []gatewayclient.Task{{
			ID: "task-1", Title: "hello", State: gatewayclient.TaskStateCompleted, Version: 2,
		}},
	}
	m := newModel(paths.ModeUser, gw)
	m.width, m.height = 100, 40
	updated, _ := m.Update(refreshDoneMsg{tasks: gw.tasks})
	mm, ok := updated.(model)
	if !ok {
		t.Fatalf("type %T", updated)
	}
	if len(mm.tasks) != 1 || mm.tasks[0].Title != "hello" {
		t.Fatalf("tasks = %#v", mm.tasks)
	}
}

func TestModelListAndCron(t *testing.T) {
	gw := &fakeGateway{
		tasks: []gatewayclient.Task{{
			ID: "task-abc", Title: "one", State: gatewayclient.TaskStateRunning, Version: 1,
		}},
		jobs: []schedulerapi.Job{{
			ID: "job-1", Name: "heartbeat", Status: schedulerapi.StatusActive,
			NextRunAt: "2026-07-30T12:00:00Z", IntervalSeconds: 60,
		}},
		model: gatewayclient.ModelConfig{
			Model:  "deepseek/a",
			Models: []string{"deepseek/a", "deepseek/b"},
		},
		health: gatewayclient.Health{OK: true},
	}
	gw.health.Core.Runtime.DataDir = "/data/aze"

	m := newModel(paths.ModeUser, gw)
	m.width, m.height = 120, 40

	updated, _ := m.Update(statusDoneMsg{health: gw.health, model: gw.model})
	mm := updated.(model)
	if mm.dataDir != "/data/aze" || mm.modelName != "deepseek/a" || len(mm.models) != 2 {
		t.Fatalf("status fields: dataDir=%q model=%q models=%v", mm.dataDir, mm.modelName, mm.models)
	}

	updated, _ = mm.Update(commandDoneMsg{
		openList: listModels, modelName: gw.model.Model, models: gw.model.Models, status: "select a model",
	})
	mm = updated.(model)
	if mm.list != listModels || mm.listLen() != 2 {
		t.Fatalf("listModels: kind=%v len=%d", mm.list, mm.listLen())
	}
	view := renderPickerOverlay(&mm, 80)
	for _, want := range []string{"Models", "deepseek/a", "deepseek/b"} {
		if !strings.Contains(view, want) {
			t.Fatalf("model list missing %q:\n%s", want, view)
		}
	}

	msg := mm.cronCmd()()
	done := msg.(commandDoneMsg)
	if done.err != nil || done.openList != listJobs || len(done.jobs) != 1 {
		t.Fatalf("cronCmd = %#v", done)
	}
	updated, _ = mm.Update(done)
	mm = updated.(model)
	if mm.list != listJobs || mm.listLen() != 1 {
		t.Fatalf("listJobs: kind=%v len=%d", mm.list, mm.listLen())
	}

	updated, _ = mm.Update(commandDoneMsg{clearTask: true, openList: listSessions, status: "sessions"})
	mm = updated.(model)
	if mm.list != listSessions || mm.task != nil {
		t.Fatalf("sessions: list=%v task=%v", mm.list, mm.task)
	}
	tid := gatewayclient.TaskID("task-abc")
	mm.sessions = []gatewayclient.Session{{
		ID: "session-1", Title: "one", LatestTaskID: &tid, LatestState: gatewayclient.TaskStateRunning,
	}}
	view = renderPickerOverlay(&mm, 80)
	for _, want := range []string{"Sessions", "session-1", "one"} {
		if !strings.Contains(view, want) {
			t.Fatalf("sessions missing %q:\n%s", want, view)
		}
	}

	strip := mm.renderContextStrip()
	if !strings.Contains(strip, "deepseek/a") || !strings.Contains(strip, "ctx") {
		t.Fatalf("strip=%q", strip)
	}
	panel := mm.renderContextPanel(20)
	if !strings.Contains(panel, "Metrics") || !strings.Contains(panel, "tokens") || !strings.Contains(panel, "MCP") {
		t.Fatalf("panel:\n%s", panel)
	}
}
