package toolpermission

import (
	"context"
	"testing"
	"time"

	storesqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func TestStoreInsertListMark(t *testing.T) {
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
	if err := store.Insert(ctx, Request{
		ID: "perm-1", SessionID: "s1", TaskID: "t1", RunID: "r1",
		PlanID: "p1", PlanHash: "h", StepID: "st1",
		ToolCallID: "c1", ToolName: "process_exec", Capability: "process_exec",
		Path: "/tmp/ws", State: StatePending,
		CreatedAt: now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListPending(ctx, "s1", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v err=%v", list, err)
	}
	if err := store.MarkDecided(ctx, "perm-1", DecisionDeny, StateDenied, "", "user", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	list, err = store.ListPending(ctx, "s1", 10)
	if err != nil || len(list) != 0 {
		t.Fatalf("after decide list = %v", list)
	}
	got, err := store.Get(ctx, "perm-1")
	if err != nil || got.State != StateDenied {
		t.Fatalf("got = %+v err=%v", got, err)
	}
}

func TestWaiterNotify(t *testing.T) {
	w := NewWaiter()
	w.Register("p1")
	done := make(chan Decision, 1)
	go func() {
		d, err := w.Wait(context.Background(), "p1")
		if err != nil {
			t.Errorf("wait: %v", err)
			return
		}
		done <- d
	}()
	time.Sleep(20 * time.Millisecond)
	w.Notify(Decision{PermissionID: "p1", Decision: DecisionAllowOnce, GrantID: "g1", State: StateAllowedOnce})
	select {
	case d := <-done:
		if d.GrantID != "g1" {
			t.Fatalf("%+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
