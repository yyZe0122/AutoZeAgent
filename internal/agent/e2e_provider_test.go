//go:build e2e

package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/agent"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/internal/providers"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

// Opt-in live provider smoke: full agent loop, no tools.
//
//	YMZ_E2E_PROVIDER=1 go test -tags e2e ./internal/agent/ -run TestE2EProviderAgentRun -count=1
//
// Uses ConfigDir from YMZ_CONFIG_DIR or ~/.config/yunmengze (Linux user layout).
func TestE2EProviderAgentRun(t *testing.T) {
	if os.Getenv("YMZ_E2E_PROVIDER") != "1" {
		t.Skip("set YMZ_E2E_PROVIDER=1 to run live provider e2e")
	}
	configDir := strings.TrimSpace(os.Getenv("YMZ_CONFIG_DIR"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		configDir = filepath.Join(home, ".config", "yunmengze")
	}
	resolved, err := providerconfig.Load(configDir)
	if err != nil {
		t.Fatalf("load provider config from %s: %v", configDir, err)
	}
	provider, err := providers.NewConfigured(*resolved)
	if err != nil {
		t.Fatal(err)
	}

	database, err := coresqlite.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO sessions(session_id,state,created_at,updated_at) VALUES(?,?,?,?)", []any{"session-e2e", "active", stamp, stamp}},
		{"INSERT INTO tasks(task_id,session_id,title,objective,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", []any{"task-e2e", "session-e2e", "E2E", "ping", "running", stamp, stamp}},
		{"INSERT INTO plans(plan_id,task_id,revision,state,scope_hash,created_at,updated_at,document) VALUES(?,?,?,?,?,?,?,?)", []any{"plan-e2e", "task-e2e", 1, "approved", "hash-e2e", stamp, stamp, `{}`}},
		{"INSERT INTO plan_steps(step_id,plan_id,position,title,state,effect_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", []any{"step-e2e", "plan-e2e", 0, "Chat", "running", "R0", stamp, stamp}},
		{"INSERT INTO runs(run_id,task_id,plan_id,state,started_at,updated_at,step_id) VALUES(?,?,?,?,?,?,?)", []any{"run-e2e", "task-e2e", "plan-e2e", "running", stamp, stamp, "step-e2e"}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	store, err := agent.NewRecordStore(db)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := agent.New(agent.Config{
		Provider: provider,
		Broker:   noopBroker{},
		Records:  store,
		Model:    resolved.ModelID,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := runner.Run(ctx, agent.RunRequest{
		RunID: "run-e2e", TaskID: "task-e2e", PlanID: "plan-e2e", PlanHash: "hash-e2e",
		StepID: "step-e2e", Actor: "e2e", TraceID: "e2e-provider",
		Messages: []providerapi.Message{
			{Role: providerapi.RoleUser, Content: "Reply with exactly the single word: pong"},
		},
	})
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}
	if strings.TrimSpace(result.Content) == "" {
		t.Fatal("empty provider content")
	}
	if !strings.Contains(strings.ToLower(result.Content), "pong") {
		t.Fatalf("content %q does not contain pong", result.Content)
	}
	t.Logf("model=%s content=%q usage=%+v", resolved.ModelID, result.Content, result.Usage)
}

type noopBroker struct{}

func (noopBroker) Definitions() []toolapi.Definition { return nil }

func (noopBroker) Execute(context.Context, toolapi.Request) (toolapi.Response, error) {
	return toolapi.Response{}, toolapi.ErrDenied
}
