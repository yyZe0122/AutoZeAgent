package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	"github.com/yyZe0122/yunmengze-agent/internal/userquestion"
)

func TestAskUserHeadlessReturnsUnavailable(t *testing.T) {
	tool := &askUserTool{at: &askUserTools{backend: &stubAskUser{waiter: userquestion.NewWaiter()}}}
	out, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"questions": []map[string]any{{"id": "q1", "question": "ok?"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(out, &payload) != nil || payload.Error != "unavailable" {
		t.Fatalf("payload = %s", out)
	}
}

func TestAskUserInteractiveWaitsForAnswer(t *testing.T) {
	waiter := userquestion.NewWaiter()
	backend := &stubAskUser{waiter: waiter}
	tool := &askUserTool{at: &askUserTools{backend: backend}}
	ctx := runmeta.With(context.Background(), runmeta.Context{
		SessionID: "s", TaskID: "t", RunID: "r", CallID: "c", Interactive: true,
	})
	go func() {
		for backend.lastID == "" {
		}
		waiter.Notify(userquestion.Decision{
			QuestionID: backend.lastID, State: userquestion.StateAnswered,
			Answers: map[string][]string{"q1": {"yes"}},
		})
	}()
	out, err := tool.Execute(ctx, mustJSON(t, map[string]any{
		"questions": []map[string]any{{"id": "q1", "question": "ok?", "options": []map[string]any{{"label": "yes"}}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Answers map[string][]string `json:"answers"`
	}
	if json.Unmarshal(out, &payload) != nil || payload.Answers["q1"][0] != "yes" {
		t.Fatalf("payload = %s", out)
	}
}

type stubAskUser struct {
	waiter *userquestion.Waiter
	lastID string
}

func (s *stubAskUser) CreatePending(_ context.Context, req userquestion.Request) (userquestion.Request, error) {
	req.ID = "uq-test"
	s.lastID = req.ID
	s.waiter.Register(req.ID)
	return req, nil
}

func (s *stubAskUser) Waiter() *userquestion.Waiter { return s.waiter }

func (s *stubAskUser) MarkUnavailable(context.Context, string, string) (userquestion.Request, error) {
	return userquestion.Request{}, nil
}
