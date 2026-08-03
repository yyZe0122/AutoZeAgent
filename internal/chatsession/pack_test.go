package chatsession

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/contextpack"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	storesqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

type trackingCompactor struct {
	mu    sync.Mutex
	calls int
}

func (t *trackingCompactor) CompactSummary(_ context.Context, head []providerapi.Message) (string, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return "summary of " + strings.TrimSpace(head[0].Content), nil
}

func (t *trackingCompactor) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func largeHistory(rounds int) []providerapi.Message {
	big := strings.Repeat("T", 8_000)
	var out []providerapi.Message
	for i := 0; i < rounds; i++ {
		out = append(out,
			providerapi.Message{Role: providerapi.RoleUser, Content: "u"},
			providerapi.Message{Role: providerapi.RoleAssistant, Content: "a", ToolCalls: []providerapi.ToolCall{
				{ID: "c" + string(rune('a'+i%26)), Name: "fs_read", Arguments: `{}`},
			}},
			providerapi.Message{Role: providerapi.RoleTool, ToolCallID: "c" + string(rune('a'+i%26)), Content: big},
		)
	}
	return out
}

func TestPackSessionHistorySkipsCompactorWhenDisabled(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	repo, err := kernel.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := approval.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := corequery.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := contextpack.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	compactor := &trackingCompactor{}
	disabled := false
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: &fakeAgent{done: make(chan struct{})},
		Transcript: queries, WorkspaceRoots: []string{t.TempDir()},
		ContextWindow: 4_096, Context: store, Compactor: compactor,
		CompactionEnabled: &disabled,
		Now:               func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	history := largeHistory(8)
	out, err := svc.packSessionHistory(ctx, "session-pack-off", history)
	if err != nil {
		t.Fatal(err)
	}
	if compactor.callCount() != 0 {
		t.Fatalf("compactor calls = %d, want 0 when disabled", compactor.callCount())
	}
	if _, err := store.LatestCompaction(ctx, "session-pack-off"); err == nil {
		t.Fatal("expected no session_compactions when disabled")
	}
	if len(out) == 0 {
		t.Fatal("expected packed messages")
	}
}

func TestPackSessionHistoryCompactsWhenEnabledAndOverBudget(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := database.SQL()
	repo, err := kernel.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	approvals, err := approval.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := corequery.New(db)
	if err != nil {
		t.Fatal(err)
	}
	store, err := contextpack.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	compactor := &trackingCompactor{}
	enabled := true
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: &fakeAgent{done: make(chan struct{})},
		Transcript: queries, WorkspaceRoots: []string{t.TempDir()},
		ContextWindow: 4_096, Context: store, Compactor: compactor,
		CompactionEnabled: &enabled,
		Now:               func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	history := largeHistory(8)
	out, err := svc.packSessionHistory(ctx, "session-pack-on", history)
	if err != nil {
		t.Fatal(err)
	}
	if compactor.callCount() < 1 {
		t.Fatalf("compactor calls = %d, want >= 1", compactor.callCount())
	}
	c, err := store.LatestCompaction(ctx, "session-pack-on")
	if err != nil {
		t.Fatalf("latest compaction: %v", err)
	}
	if !strings.Contains(c.Summary, "summary of") {
		t.Fatalf("summary = %q", c.Summary)
	}
	if len(out) == 0 {
		t.Fatal("expected packed messages after compact")
	}
	found := false
	for _, m := range out {
		if m.Role == providerapi.RoleSystem && strings.Contains(m.Content, "compacted") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected compact summary system message in provider view: %+v", out)
	}
}
