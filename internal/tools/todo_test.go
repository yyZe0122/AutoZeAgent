package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	"github.com/yyZe0122/yunmengze-agent/internal/sessiontodo"
)

type memTodos struct {
	items []sessiontodo.Item
}

func (m *memTodos) List(context.Context, string) ([]sessiontodo.Item, error) {
	return append([]sessiontodo.Item(nil), m.items...), nil
}

func (m *memTodos) Replace(_ context.Context, _ string, items []sessiontodo.Item) error {
	m.items = append([]sessiontodo.Item(nil), items...)
	return nil
}

func TestTodoWriteAndList(t *testing.T) {
	backend := &memTodos{}
	tt := &todoTools{backend: backend}
	list := &todoListTool{tt: tt}
	write := &todoWriteTool{tt: tt}
	if list.Definition().Risk != "R0" || write.Definition().Risk != "R0" {
		t.Fatal("todos must stay R0")
	}
	ctx := runmeta.With(context.Background(), runmeta.Context{SessionID: "s1"})
	raw, err := write.Execute(ctx, json.RawMessage(`{"items":[{"content":"patch fs.go","status":"in_progress"},{"content":"test","status":"pending"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Count int `json:"count"`
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 || out.Todos[0].Status != sessiontodo.StatusInProgress {
		t.Fatalf("%+v", out)
	}
	listed, err := list.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(listed, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 {
		t.Fatalf("list=%+v", out)
	}
}
