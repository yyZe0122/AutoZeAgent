package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
	"github.com/yyZe0122/yunmengze-agent/internal/version"
)

func TestHeaderShowsAgentVersion(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	m.width, m.height = 80, 24
	got := m.renderHeader()
	if !strings.Contains(got, version.Version) {
		t.Fatalf("header missing version %q: %s", version.Version, got)
	}
}

func TestTabTogglesDraftMode(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	if m.draftMode != modeAgent {
		t.Fatalf("default mode = %q", m.draftMode)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm := updated.(model)
	if mm.draftMode != modeAuto {
		t.Fatalf("after tab = %q", mm.draftMode)
	}
	box := mm.renderInputBox(80)
	if !strings.Contains(box, "auto") {
		t.Fatalf("input box missing auto:\n%s", box)
	}
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm = updated.(model)
	if mm.draftMode != modePlan {
		t.Fatalf("second tab = %q", mm.draftMode)
	}
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm = updated.(model)
	if mm.draftMode != modeAgent {
		t.Fatalf("third tab = %q", mm.draftMode)
	}
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	mm = updated.(model)
	if mm.draftMode != modePlan {
		t.Fatalf("shift-tab = %q", mm.draftMode)
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

func TestLayoutReservesFramePadding(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	m.width, m.height = 120, 40
	m.layout()
	if m.viewport.Width >= m.width {
		t.Fatalf("viewport width %d not inset from %d", m.viewport.Width, m.width)
	}
	if m.viewport.Height >= m.height {
		t.Fatalf("viewport height %d not inset from %d", m.viewport.Height, m.height)
	}
	wantW := m.width - framePadX*2 - 1 - contextPanelWidth - 3
	if m.viewport.Width != wantW {
		t.Fatalf("viewport width = %d want %d", m.viewport.Width, wantW)
	}
	wantH := m.height - framePadY*2 - 1 - 4 - moduleGap*2
	if m.viewport.Height != wantH {
		t.Fatalf("viewport height = %d want %d", m.viewport.Height, wantH)
	}

	m.syncViewport(true)
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("view too short:\n%s", view)
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("expected top pad, first line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Fatalf("expected left pad, line = %q", lines[1])
	}
}

func TestLayoutNarrowWindowDoesNotOverflow(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	m.width, m.height = 30, 20
	m.layout()
	if m.innerWidth() != m.width-framePadX*2 {
		t.Fatalf("innerWidth = %d want %d", m.innerWidth(), m.width-framePadX*2)
	}
	m.syncViewport(true)
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line %d width %d > %d: %q", i, w, m.width, line)
		}
	}

	m.width, m.height = 18, 16
	m.layout()
	avail := m.innerWidth() - 1
	if avail < 1 {
		avail = 1
	}
	if m.viewport.Width > avail {
		t.Fatalf("viewport width %d inflated past available %d", m.viewport.Width, avail)
	}
	m.syncViewport(true)
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("narrow line %d width %d > %d: %q", i, w, m.width, line)
		}
	}
}

func TestTruncateIsSingleLine(t *testing.T) {
	got := truncate("abcdefghijklmnopqrstuvwxyz", 8)
	if strings.Contains(got, "\n") {
		t.Fatalf("truncate wrapped:\n%q", got)
	}
	if w := lipgloss.Width(got); w > 8 {
		t.Fatalf("width %d > 8: %q", w, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing ellipsis: %q", got)
	}
	cjk := truncate("云梦泽编码循环上下文", 6)
	if strings.Contains(cjk, "\n") {
		t.Fatalf("cjk wrapped:\n%q", cjk)
	}
	if w := lipgloss.Width(cjk); w > 6 {
		t.Fatalf("cjk width %d > 6: %q", w, cjk)
	}
}
