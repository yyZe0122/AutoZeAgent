package contextpack

import (
	"context"
	"testing"
	"time"

	storesqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func TestCountCompactionsSinceAndAllowLLMCompact(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := NewStore(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	for i := 0; i < DefaultAntiThrashMax; i++ {
		if err := store.InsertCompaction(ctx, Compaction{
			ID: "c" + string(rune('a'+i)), SessionID: "s1", Summary: "sum",
			CreatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := store.CountCompactionsSince(ctx, "s1", now.Add(-DefaultAntiThrashWindow).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if n != DefaultAntiThrashMax {
		t.Fatalf("count=%d", n)
	}
	if store.AllowLLMCompact(ctx, "s1", now, DefaultAntiThrashWindow, DefaultAntiThrashMax) {
		t.Fatal("expected thrash gate to deny LLM compact")
	}
	if !store.AllowLLMCompact(ctx, "s2", now, DefaultAntiThrashWindow, DefaultAntiThrashMax) {
		t.Fatal("empty session should allow")
	}
}
