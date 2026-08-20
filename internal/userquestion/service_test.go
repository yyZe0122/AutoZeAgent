package userquestion

import (
	"context"
	"testing"
	"time"

	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func TestAnswerDeliversToWaiter(t *testing.T) {
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
	svc, err := New(Config{Store: store, Now: func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	req, err := svc.CreatePending(ctx, Request{
		SessionID: "s1", TaskID: "t1", RunID: "r1", ToolCallID: "c1",
		Questions: []Item{{ID: "q1", Question: "Ship it?", Options: []Option{{Label: "yes"}, {Label: "no"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Decision, 1)
	go func() {
		d, err := svc.Waiter().Wait(ctx, req.ID)
		if err != nil {
			t.Errorf("wait: %v", err)
			return
		}
		done <- d
	}()
	if _, err := svc.Answer(ctx, req.ID, "user", map[string][]string{"q1": {"yes"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-done:
		if d.State != StateAnswered || d.Answers["q1"][0] != "yes" {
			t.Fatalf("decision = %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not unblock")
	}
}
