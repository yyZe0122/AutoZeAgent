package chatsession

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/contextpack"
	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	storesqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
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

func TestPackSessionHistoryAntiThrashSkipsLLM(t *testing.T) {
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
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	// Seed max anti-thrash durable compactions inside the window.
	for i := 0; i < contextpack.DefaultAntiThrashMax; i++ {
		if err := store.InsertCompaction(ctx, contextpack.Compaction{
			ID: fmt.Sprintf("seed-%d", i), SessionID: "session-thrash",
			Summary: "prior", CreatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
	}
	compactor := &trackingCompactor{}
	enabled := true
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: &fakeAgent{done: make(chan struct{})},
		Transcript: queries, WorkspaceRoots: []string{t.TempDir()},
		ContextWindow: 4_096, Context: store, Compactor: compactor,
		CompactionEnabled: &enabled,
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	// With thrash max already written, AllowLLMCompact is false. Even if packing
	// still tries head summary (no prior inject path), LLM must not run.
	// Seed rows also satisfy LatestCompaction → inject short-circuits re-summary;
	// either way LLM call count stays 0.
	history := largeHistory(8)
	out, err := svc.packSessionHistory(ctx, "session-thrash", history)
	if err != nil {
		t.Fatal(err)
	}
	if compactor.callCount() != 0 {
		t.Fatalf("compactor calls = %d, want 0 under anti-thrash", compactor.callCount())
	}
	if len(out) == 0 {
		t.Fatal("expected packed messages")
	}

	// True thrash gate on first-time compact: use a fresh session id with thrash
	// rows but empty summaries cannot exist; instead call pack on session that
	// has thrash count via inserts with summary, then pack a session that only
	// shares CountCompactionsSince — same session. Cover gate via store helper
	// and ForceCompact bypass (below).
	if store.AllowLLMCompact(ctx, "session-thrash", now, contextpack.DefaultAntiThrashWindow, contextpack.DefaultAntiThrashMax) {
		t.Fatal("expected AllowLLMCompact false after seeding thrash max")
	}
}

func TestForceCompactWritesSummary(t *testing.T) {
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
	store, err := contextpack.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	tl := &fixedTranscript{msgs: []corequery.TranscriptMessage{
		{ID: "1", Role: "user", Content: "hello"},
		{ID: "2", Role: "assistant", Content: "hi"},
		{ID: "3", Role: "user", Content: "more context " + strings.Repeat("x", 200)},
		{ID: "4", Role: "assistant", Content: "ok"},
		{ID: "5", Role: "user", Content: "third"},
		{ID: "6", Role: "assistant", Content: "done"},
	}}
	compactor := &trackingCompactor{}
	enabled := true
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: &fakeAgent{done: make(chan struct{})},
		Transcript: tl, WorkspaceRoots: []string{t.TempDir()},
		ContextWindow: 4_096, Context: store, Compactor: compactor,
		CompactionEnabled: &enabled,
		Now:               func() time.Time { return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ForceCompact(ctx, "session-force", "keep paths")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "llm" {
		t.Fatalf("source=%q", res.Source)
	}
	if compactor.callCount() != 1 {
		t.Fatalf("compactor calls=%d", compactor.callCount())
	}
	c, err := store.LatestCompaction(ctx, "session-force")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Summary, "summary of") {
		t.Fatalf("summary=%q", c.Summary)
	}
}

func TestLastHistoryIDMapsHeadCut(t *testing.T) {
	full := []providerapi.Message{
		{Role: providerapi.RoleUser, Content: "u1"},
		{Role: providerapi.RoleAssistant, Content: "a1"},
		{Role: providerapi.RoleUser, Content: "u2"},
		{Role: providerapi.RoleAssistant, Content: "a2"},
	}
	ids := []string{"id1", "id2", "id3", "id4"}
	head := full[:2]
	if got := lastHistoryID(ids, head, full); got != "id2" {
		t.Fatalf("through=%q want id2", got)
	}
}

func TestAssembleKeepsTailWhenThroughMissing(t *testing.T) {
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
	store, err := contextpack.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertCompaction(ctx, contextpack.Compaction{
		ID: "c-old", SessionID: "session-through-miss",
		Summary:          "[Prior session context — compacted]\nkeep middle",
		ThroughMessageID: "gone-id", Model: "main-model",
		CreatedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	history := []providerapi.Message{
		{Role: providerapi.RoleUser, Content: "middle-path=/src/keep.go"},
		{Role: providerapi.RoleAssistant, Content: "middle-ok"},
		{Role: providerapi.RoleUser, Content: "recent"},
		{Role: providerapi.RoleAssistant, Content: "recent-ok"},
		{Role: providerapi.RoleUser, Content: "tail-u"},
		{Role: providerapi.RoleAssistant, Content: "tail-a"},
	}
	ids := []string{"a", "b", "c", "d", "e", "f"}
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: &fakeAgent{done: make(chan struct{})},
		Transcript: &fixedTranscript{}, WorkspaceRoots: []string{t.TempDir()},
		ContextWindow: 128_000, Context: store, MainModel: "main-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.assembleContextView(ctx, "session-through-miss", nil, history, ids, "now", 128_000, 2048, "main-model")
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, m := range view.Messages() {
		joined += m.Content
	}
	if !strings.Contains(joined, "middle-path=/src/keep.go") {
		t.Fatalf("through miss dropped middle tail: %q", joined)
	}
}

func TestForceCompactWritesModelAndThrough(t *testing.T) {
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
	store, err := contextpack.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	tl := &fixedTranscript{msgs: []corequery.TranscriptMessage{
		{ID: "1", Role: "user", Content: "hello"},
		{ID: "2", Role: "assistant", Content: "hi"},
		{ID: "3", Role: "user", Content: "more context " + strings.Repeat("x", 200)},
		{ID: "4", Role: "assistant", Content: "ok"},
		{ID: "5", Role: "user", Content: "third"},
		{ID: "6", Role: "assistant", Content: "done"},
	}}
	enabled := true
	svc, err := New(Config{
		DB: db, Repository: repo, Approvals: approvals, Agent: &fakeAgent{done: make(chan struct{})},
		Transcript: tl, WorkspaceRoots: []string{t.TempDir()},
		ContextWindow: 4_096, Context: store, Compactor: &trackingCompactor{},
		CompactionEnabled: &enabled, MainModel: "wired-main",
		Now: func() time.Time { return time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ForceCompact(ctx, "session-force-model", ""); err != nil {
		t.Fatal(err)
	}
	c, err := store.LatestCompaction(ctx, "session-force-model")
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "wired-main" {
		t.Fatalf("model=%q", c.Model)
	}
	if c.ThroughMessageID == "" {
		t.Fatal("through_message_id empty")
	}
}

type fixedTranscript struct {
	msgs []corequery.TranscriptMessage
}

func (f *fixedTranscript) SessionTranscript(context.Context, kernel.SessionID, corequery.TranscriptOptions) ([]corequery.TranscriptMessage, error) {
	return f.msgs, nil
}

func (f *fixedTranscript) SessionTranscriptTail(context.Context, kernel.SessionID, int) ([]corequery.TranscriptMessage, error) {
	return f.msgs, nil
}
