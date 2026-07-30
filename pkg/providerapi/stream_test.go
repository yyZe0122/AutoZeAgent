package providerapi

import (
	"context"
	"testing"
)

type fakeStreamProvider struct {
	events []StreamEvent
}

func (f fakeStreamProvider) Stream(_ context.Context, _ CompletionRequest, h StreamHandler) error {
	for _, e := range f.events {
		if err := h(e); err != nil {
			return err
		}
	}
	return nil
}

func TestCollectStreamWithThinking(t *testing.T) {
	usage := Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}
	p := fakeStreamProvider{events: []StreamEvent{
		{Type: StreamThinking, ThinkingDelta: "plan "},
		{Type: StreamThinking, ThinkingDelta: "step"},
		{Type: StreamDelta, ContentDelta: "hello "},
		{Type: StreamDelta, ContentDelta: "world"},
		{Type: StreamComplete, Usage: &usage, FinishReason: "stop"},
	}}
	resp, err := CollectStream(context.Background(), p, CompletionRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Thinking != "plan step" || resp.Content != "hello world" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestEmitResponseIncludesThinking(t *testing.T) {
	var types []StreamEventType
	err := EmitResponse(CompletionResponse{
		Thinking: "t", Content: "c", Usage: Usage{}, FinishReason: "stop",
	}, func(e StreamEvent) error {
		types = append(types, e.Type)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 3 || types[0] != StreamThinking || types[1] != StreamDelta || types[2] != StreamComplete {
		t.Fatalf("types = %#v", types)
	}
}
