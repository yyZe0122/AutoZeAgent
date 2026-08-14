package sessiontodo

import (
	"context"
	"strings"
	"testing"

	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func TestReplaceAndList(t *testing.T) {
	ctx := context.Background()
	db, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(ctx, "s1", []Item{
		{Content: "read file", Status: "in_progress"},
		{ID: "done-1", Content: "wrote patch", Status: "completed"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Status != StatusInProgress || items[1].ID != "done-1" {
		t.Fatalf("%+v", items)
	}
	block := CompactBlock(items)
	if !strings.Contains(block, "Session todos:") || !strings.Contains(block, "in_progress") {
		t.Fatalf("block=%q", block)
	}
}
