package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
)

func TestTabTogglesDraftMode(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	if m.draftMode != modeAgent {
		t.Fatalf("default mode = %q", m.draftMode)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm := updated.(model)
	if mm.draftMode != modePlan {
		t.Fatalf("after tab = %q", mm.draftMode)
	}
	box := mm.renderInputBox(80)
	if !strings.Contains(box, "plan") {
		t.Fatalf("input box missing plan:\n%s", box)
	}
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm = updated.(model)
	if mm.draftMode != modeAgent {
		t.Fatalf("toggle back = %q", mm.draftMode)
	}
}

func TestPickerDoesNotClearTask(t *testing.T) {
	task := gatewayclient.Task{ID: "task-keep", Title: "keep", State: gatewayclient.TaskStateRunning}
	m := newModel(paths.ModeUser, &fakeGateway{tasks: []gatewayclient.Task{task}})
	m.task = &task
	m.timeline = []timelineItem{{Kind: tlUser, Title: "objective", Body: "hello"}}
	updated, _ := m.Update(commandDoneMsg{openList: listSessions, status: "session history"})
	mm := updated.(model)
	if mm.list != listSessions {
		t.Fatalf("list = %v", mm.list)
	}
	if mm.task == nil || mm.task.ID != "task-keep" {
		t.Fatalf("task cleared: %#v", mm.task)
	}
	if len(mm.timeline) == 0 {
		t.Fatal("timeline cleared")
	}
	body := renderSessionView(&mm)
	if !strings.Contains(body, "hello") {
		t.Fatalf("session view missing timeline:\n%s", body)
	}
	ov := renderPickerOverlay(&mm, 80)
	if !strings.Contains(ov, "Sessions") {
		t.Fatalf("overlay:\n%s", ov)
	}
}

func TestPgUpWorksWhilePickerOpen(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	m.width, m.height = 100, 40
	m.list = listSessions
	m.layout()
	// fill viewport
	m.viewport.SetContent(strings.Repeat("line\n", 40))
	m.viewport.GotoBottom()
	before := m.viewport.YOffset
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	mm := updated.(model)
	if mm.viewport.YOffset >= before && before > 0 {
		t.Fatalf("expected scroll up: before=%d after=%d", before, mm.viewport.YOffset)
	}
}
