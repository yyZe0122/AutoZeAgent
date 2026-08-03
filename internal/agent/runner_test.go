package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coresqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/pkg/providerapi"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

func TestRunnerTrimsLongToolResultsForProviderOnly(t *testing.T) {
	store, db, _ := openAgentFixture(t)
	longBody := strings.Repeat("Z", 200)
	longOutputBytes, err := json.Marshal(map[string]string{"body": longBody})
	if err != nil {
		t.Fatal(err)
	}
	longOutput := string(longOutputBytes)
	provider := &sequenceProvider{responses: []providerapi.CompletionResponse{
		{
			ToolCalls: []providerapi.ToolCall{{ID: "call-long", Name: "test_read", Arguments: `{}`}},
			Usage:     providerapi.Usage{TotalTokens: 1},
		},
		{Content: "done", Usage: providerapi.Usage{TotalTokens: 1}},
	}}
	broker := &longOutputBroker{recordingBroker: recordingBroker{db: db, definitions: testDefinitions()}, output: longOutput}
	runner, err := New(Config{
		Provider: provider, Broker: broker, Records: store, Model: "test-model",
		MaxToolResultRunes: 96,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" {
		t.Fatalf("result = %+v", result)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
	// Second request includes tool result: trimmed for provider.
	var toolMsg *providerapi.Message
	for i := range provider.requests[1].Messages {
		m := &provider.requests[1].Messages[i]
		if m.Role == providerapi.RoleTool && m.ToolCallID == "call-long" {
			toolMsg = m
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("missing tool message in provider request")
	}
	if toolMsg.Content == longOutput {
		t.Fatal("provider still received full tool output")
	}
	if !strings.Contains(toolMsg.Content, "trimmed") {
		t.Fatalf("expected trim marker, got %q", toolMsg.Content)
	}
	// Persisted record keeps full content.
	records, err := store.List(context.Background(), request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var persisted string
	for _, rec := range records {
		if rec.Type == RecordToolResult && rec.Message.ToolCallID == "call-long" {
			persisted = rec.Message.Content
			break
		}
	}
	if persisted != longOutput {
		t.Fatalf("persisted tool result len=%d want %d", len(persisted), len(longOutput))
	}
}

func TestRunnerRetriesRetryableProviderErrors(t *testing.T) {
	store, _, _ := openAgentFixture(t)
	provider := &sequenceProvider{
		errors: []error{
			&providerapi.ProviderError{Provider: "test", Kind: providerapi.ErrorUnavailable, Retryable: true, RetryAfter: time.Millisecond},
			&providerapi.ProviderError{Provider: "test", Kind: providerapi.ErrorUnavailable, Retryable: true, RetryAfter: time.Millisecond},
		},
		responses: []providerapi.CompletionResponse{{}, {}, {
			Content: "done", Usage: providerapi.Usage{TotalTokens: 3},
		}},
	}
	runner := newTestRunner(t, provider, &recordingBroker{}, store)

	result, err := runner.Run(context.Background(), testRunRequest())
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want 3", provider.calls)
	}
	if result.Iterations != 1 || result.Content != "done" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunnerDoesNotRetryNonRetryableProviderError(t *testing.T) {
	store, _, _ := openAgentFixture(t)
	want := &providerapi.ProviderError{Provider: "test", Kind: providerapi.ErrorInvalidRequest, Retryable: false}
	provider := &sequenceProvider{errors: []error{want}}
	runner := newTestRunner(t, provider, &recordingBroker{}, store)

	_, err := runner.Run(context.Background(), testRunRequest())
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}

func TestRunnerEnforcesPersistedTokenBudget(t *testing.T) {
	store, _, _ := openAgentFixture(t)
	provider := &sequenceProvider{responses: []providerapi.CompletionResponse{{
		Content: "too expensive", Usage: providerapi.Usage{TotalTokens: 11},
	}}}
	request := testRunRequest()
	request.MaxOutputTokens = 10
	request.MaxTotalTokens = 10
	runner := newTestRunner(t, provider, &recordingBroker{}, store)

	result, err := runner.Run(context.Background(), request)
	if !errors.Is(err, ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if result.Usage.TotalTokens != 11 {
		t.Fatalf("usage = %+v", result.Usage)
	}

	// A restarted Runner must read the durable usage and refuse another
	// Provider call instead of treating the over-budget final record as success.
	restartedProvider := &sequenceProvider{responses: []providerapi.CompletionResponse{{Content: "should not run"}}}
	restarted := newTestRunner(t, restartedProvider, &recordingBroker{}, store)
	_, err = restarted.Run(context.Background(), request)
	if !errors.Is(err, ErrTokenBudgetExceeded) {
		t.Fatalf("restarted Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	if restartedProvider.calls != 0 {
		t.Fatalf("restarted provider calls = %d, want 0", restartedProvider.calls)
	}
}

func TestRunnerStopsBeforeToolsWhenCostBudgetIsExceeded(t *testing.T) {
	store, db, _ := openAgentFixture(t)
	provider := &sequenceProvider{responses: []providerapi.CompletionResponse{{
		ToolCalls: []providerapi.ToolCall{{ID: "call-budget", Name: "test_read", Arguments: `{}`}},
		Usage:     providerapi.Usage{TotalTokens: 2, Cost: providerapi.Cost{Currency: "USD", Micros: 101}},
	}}}
	broker := &recordingBroker{db: db, definitions: testDefinitions()}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	request.MaxCostMicros = 100
	runner := newTestRunner(t, provider, broker, store)

	_, err := runner.Run(context.Background(), request)
	if !errors.Is(err, ErrCostBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrCostBudgetExceeded", err)
	}
	if broker.calls != 0 {
		t.Fatalf("tool calls = %d, want 0", broker.calls)
	}
}

func TestRunnerFeedsDeniedToolResultAndContinues(t *testing.T) {
	store, _, _ := openAgentFixture(t)
	provider := &sequenceProvider{responses: []providerapi.CompletionResponse{
		{
			ToolCalls: []providerapi.ToolCall{{ID: "call-deny", Name: "test_read", Arguments: `{"path":"rel"}`}},
			Usage:     providerapi.Usage{TotalTokens: 2},
		},
		{
			Content: "recovered after deny", Usage: providerapi.Usage{TotalTokens: 3},
		},
	}}
	broker := &recordingBroker{
		definitions: testDefinitions(),
		executeErr:  fmt.Errorf("%w: paths must be absolute", toolapi.ErrDenied),
	}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	runner := newTestRunner(t, provider, broker, store)

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "recovered after deny" || result.Iterations != 2 {
		t.Fatalf("result = %+v", result)
	}
	if broker.calls != 1 {
		t.Fatalf("tool calls = %d, want 1", broker.calls)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if len(provider.requests) < 2 {
		t.Fatalf("provider requests = %d", len(provider.requests))
	}
	msgs := provider.requests[1].Messages
	if len(msgs) < 1 {
		t.Fatal("second provider request has no messages")
	}
	toolMsg := msgs[len(msgs)-1]
	if toolMsg.Role != providerapi.RoleTool || toolMsg.ToolCallID != "call-deny" {
		t.Fatalf("tool message = %+v", toolMsg)
	}
	if !strings.Contains(toolMsg.Content, "tool_denied") {
		t.Fatalf("tool content = %q, want tool_denied", toolMsg.Content)
	}
	if len(result.ToolCalls) != 1 || string(result.ToolCalls[0].Output) != toolMsg.Content {
		t.Fatalf("result tool calls = %+v", result.ToolCalls)
	}
}

func TestRunnerRestoresDeniedToolResultAfterRestart(t *testing.T) {
	store, _, _ := openAgentFixture(t)
	firstProvider := &sequenceProvider{
		responses: []providerapi.CompletionResponse{{
			ToolCalls: []providerapi.ToolCall{{ID: "call-deny", Name: "test_read", Arguments: `{}`}},
			Usage:     providerapi.Usage{TotalTokens: 2},
		}},
		errors: []error{nil, context.Canceled},
	}
	broker := &recordingBroker{
		definitions: testDefinitions(),
		executeErr:  fmt.Errorf("%w: duration exceeds grant", toolapi.ErrDenied),
	}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	firstRunner := newTestRunner(t, firstProvider, broker, store)

	_, err := firstRunner.Run(context.Background(), request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first Run() error = %v, want context.Canceled", err)
	}
	if broker.calls != 1 {
		t.Fatalf("tool calls before restart = %d, want 1", broker.calls)
	}

	secondProvider := &sequenceProvider{responses: []providerapi.CompletionResponse{{
		Content: "after deny restore", Usage: providerapi.Usage{TotalTokens: 3},
	}}}
	secondRunner := newTestRunner(t, secondProvider, broker, store)
	result, err := secondRunner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "after deny restore" || result.Iterations != 2 {
		t.Fatalf("recovered result = %+v", result)
	}
	if broker.calls != 1 {
		t.Fatalf("tool calls after restart = %d, want still 1", broker.calls)
	}
	if secondProvider.calls != 1 {
		t.Fatalf("second provider calls = %d, want 1", secondProvider.calls)
	}
	toolMsg := secondProvider.requests[0].Messages[len(secondProvider.requests[0].Messages)-1]
	if toolMsg.Role != providerapi.RoleTool || !strings.Contains(toolMsg.Content, "tool_denied") {
		t.Fatalf("restored tool message = %+v", toolMsg)
	}
}

func TestRunnerRespectsMaxIterations(t *testing.T) {
	store, db, _ := openAgentFixture(t)
	// Each response requests a tool call so the loop never finishes with content.
	var responses []providerapi.CompletionResponse
	for i := 0; i < 5; i++ {
		responses = append(responses, providerapi.CompletionResponse{
			ToolCalls: []providerapi.ToolCall{{
				ID: fmt.Sprintf("call-%d", i), Name: "test_read", Arguments: `{}`,
			}},
			Usage: providerapi.Usage{TotalTokens: 1, InputTokens: 1},
		})
	}
	provider := &sequenceProvider{responses: responses}
	broker := &recordingBroker{db: db, definitions: testDefinitions()}
	runner, err := New(Config{
		Provider: provider, Broker: broker, Records: store, Model: "test-model",
		MaxIterations: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	result, err := runner.Run(context.Background(), request)
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("Run() error = %v, want ErrMaxIterations", err)
	}
	if result.Iterations != 2 {
		t.Fatalf("iterations = %d, want 2", result.Iterations)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
}

func TestRunnerStillFailsOnNonDeniedToolError(t *testing.T) {
	store, _, _ := openAgentFixture(t)
	provider := &sequenceProvider{responses: []providerapi.CompletionResponse{{
		ToolCalls: []providerapi.ToolCall{{ID: "call-fail", Name: "test_read", Arguments: `{}`}},
		Usage:     providerapi.Usage{TotalTokens: 2},
	}}}
	want := errors.New("broker internal failure")
	broker := &recordingBroker{definitions: testDefinitions(), executeErr: want}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	runner := newTestRunner(t, provider, broker, store)

	_, err := runner.Run(context.Background(), request)
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}

func TestRunnerRestoresDurableToolTurnAfterRestart(t *testing.T) {
	store, db, _ := openAgentFixture(t)
	firstProvider := &sequenceProvider{
		responses: []providerapi.CompletionResponse{{
			ToolCalls: []providerapi.ToolCall{{ID: "call-1", Name: "test_read", Arguments: `{}`}},
			Usage:     providerapi.Usage{TotalTokens: 4},
		}},
		errors: []error{nil, context.Canceled},
	}
	broker := &recordingBroker{db: db, definitions: testDefinitions()}
	request := testRunRequest()
	request.AllowedTools = []string{"test_read"}
	request.MaxTotalTokens = 20
	firstRunner := newTestRunner(t, firstProvider, broker, store)

	_, err := firstRunner.Run(context.Background(), request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first Run() error = %v, want context.Canceled", err)
	}
	if broker.calls != 1 {
		t.Fatalf("tool calls before restart = %d, want 1", broker.calls)
	}

	secondProvider := &sequenceProvider{responses: []providerapi.CompletionResponse{{
		Content: "recovered", Usage: providerapi.Usage{TotalTokens: 3},
	}}}
	secondRunner := newTestRunner(t, secondProvider, broker, store)
	result, err := secondRunner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "recovered" || result.Iterations != 2 || result.Usage.TotalTokens != 7 {
		t.Fatalf("recovered result = %+v", result)
	}
	if broker.calls != 1 {
		t.Fatalf("tool calls after restart = %d, want still 1", broker.calls)
	}
	if secondProvider.calls != 1 || len(secondProvider.requests[0].Messages) != 4 {
		t.Fatalf("second provider calls/messages = %d/%d", secondProvider.calls, len(secondProvider.requests[0].Messages))
	}
	if got := secondProvider.requests[0].Messages[3]; got.Role != providerapi.RoleTool || got.ToolCallID != "call-1" {
		t.Fatalf("restored tool message = %+v", got)
	}
}

type sequenceProvider struct {
	responses []providerapi.CompletionResponse
	errors    []error
	requests  []providerapi.CompletionRequest
	calls     int
}

func (p *sequenceProvider) Stream(_ context.Context, request providerapi.CompletionRequest, handler providerapi.StreamHandler) error {
	index := p.calls
	p.calls++
	p.requests = append(p.requests, request)
	if index < len(p.errors) && p.errors[index] != nil {
		return p.errors[index]
	}
	if index >= len(p.responses) {
		return errors.New("unexpected provider call")
	}
	return providerapi.EmitResponse(p.responses[index], handler)
}

type longOutputBroker struct {
	recordingBroker
	output string
}

func (b *longOutputBroker) Execute(_ context.Context, request toolapi.Request) (toolapi.Response, error) {
	b.calls++
	if b.executeErr != nil {
		return toolapi.Response{}, b.executeErr
	}
	now := time.Now().UTC()
	response := toolapi.Response{
		CallID: request.CallID, Tool: request.Tool, Output: json.RawMessage(b.output),
		StartedAt: now, FinishedAt: now,
	}
	if b.db != nil {
		payload, err := json.Marshal(map[string]any{"response": response})
		if err != nil {
			return toolapi.Response{}, err
		}
		_, err = b.db.Exec(`INSERT INTO tool_calls
			(tool_call_id, run_id, step_id, grant_id, tool_name, state, request, response, started_at, finished_at)
			VALUES (?, ?, ?, NULL, ?, 'succeeded', ?, ?, ?, ?)`,
			request.CallID, request.RunID, request.StepID, request.Tool, string(request.Arguments), string(payload),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return toolapi.Response{}, err
		}
	}
	return response, nil
}

type recordingBroker struct {
	db          *sql.DB
	definitions []toolapi.Definition
	calls       int
	executeErr  error
}

func (b *recordingBroker) Definitions() []toolapi.Definition {
	return append([]toolapi.Definition(nil), b.definitions...)
}

func (b *recordingBroker) Execute(_ context.Context, request toolapi.Request) (toolapi.Response, error) {
	b.calls++
	if b.executeErr != nil {
		return toolapi.Response{}, b.executeErr
	}
	now := time.Now().UTC()
	response := toolapi.Response{
		CallID: request.CallID, Tool: request.Tool, Output: json.RawMessage(`{"ok":true}`),
		StartedAt: now, FinishedAt: now,
	}
	if b.db != nil {
		payload, err := json.Marshal(map[string]any{"response": response})
		if err != nil {
			return toolapi.Response{}, err
		}
		_, err = b.db.Exec(`INSERT INTO tool_calls
			(tool_call_id, run_id, step_id, grant_id, tool_name, state, request, response, started_at, finished_at)
			VALUES (?, ?, ?, NULL, ?, 'succeeded', ?, ?, ?, ?)`,
			request.CallID, request.RunID, request.StepID, request.Tool, string(request.Arguments), string(payload),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return toolapi.Response{}, err
		}
	}
	return response, nil
}

func newTestRunner(t *testing.T, provider StreamingProvider, broker ToolBroker, store *RecordStore) *Runner {
	t.Helper()
	runner, err := New(Config{Provider: provider, Broker: broker, Records: store, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func testRunRequest() RunRequest {
	return RunRequest{
		RunID: "run-agent", TaskID: "task-agent", PlanID: "plan-agent", PlanHash: "hash-agent",
		StepID: "step-agent", Actor: "agent", TraceID: "trace-agent",
		Messages: []providerapi.Message{
			{Role: providerapi.RoleSystem, Content: "Follow the plan."},
			{Role: providerapi.RoleUser, Content: "Inspect progress."},
		},
	}
}

func testDefinitions() []toolapi.Definition {
	return []toolapi.Definition{{
		Name: "test_read", Description: "Read test state", InputSchema: json.RawMessage(`{"type":"object"}`),
	}}
}

func openAgentFixture(t *testing.T) (*RecordStore, *sql.DB, func()) {
	t.Helper()
	database, err := coresqlite.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeDB := func() { _ = database.Close() }
	t.Cleanup(closeDB)
	db := database.SQL()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO sessions(session_id,state,created_at,updated_at) VALUES(?,?,?,?)", []any{"session-agent", "active", stamp, stamp}},
		{"INSERT INTO tasks(task_id,session_id,title,objective,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", []any{"task-agent", "session-agent", "Agent", "Inspect progress", "running", stamp, stamp}},
		{"INSERT INTO plans(plan_id,task_id,revision,state,scope_hash,created_at,updated_at,document) VALUES(?,?,?,?,?,?,?,?)", []any{"plan-agent", "task-agent", 1, "approved", "hash-agent", stamp, stamp, `{}`}},
		{"INSERT INTO plan_steps(step_id,plan_id,position,title,state,effect_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", []any{"step-agent", "plan-agent", 0, "Inspect", "running", "R0", stamp, stamp}},
		{"INSERT INTO runs(run_id,task_id,plan_id,state,started_at,updated_at,step_id) VALUES(?,?,?,?,?,?,?)", []any{"run-agent", "task-agent", "plan-agent", "running", stamp, stamp, "step-agent"}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			closeDB()
			t.Fatal(err)
		}
	}
	store, err := NewRecordStore(db)
	if err != nil {
		closeDB()
		t.Fatal(err)
	}
	return store, db, closeDB
}
