package modelstream

import (
	"testing"
	"time"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("sess-1", "", 8)
	defer cancel()

	h.Publish("sess-1", "task-1", "run-1", providerapi.StreamEvent{
		Type: providerapi.StreamDelta, ContentDelta: "hi",
	})
	h.Publish("other", "task-2", "run-2", providerapi.StreamEvent{
		Type: providerapi.StreamDelta, ContentDelta: "nope",
	})

	select {
	case env := <-ch:
		if env.Event.ContentDelta != "hi" || env.SessionID != "sess-1" {
			t.Fatalf("env = %#v", env)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
	select {
	case env := <-ch:
		t.Fatalf("unexpected second event %#v", env)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTeeStreamHandler(t *testing.T) {
	var got []string
	primary := func(e providerapi.StreamEvent) error {
		got = append(got, "p:"+e.ContentDelta)
		return nil
	}
	side := func(e providerapi.StreamEvent) error {
		got = append(got, "s:"+e.ContentDelta)
		return nil
	}
	h := providerapi.TeeStreamHandler(primary, side)
	if err := h(providerapi.StreamEvent{Type: providerapi.StreamDelta, ContentDelta: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "p:x" || got[1] != "s:x" {
		t.Fatalf("got %#v", got)
	}
}
