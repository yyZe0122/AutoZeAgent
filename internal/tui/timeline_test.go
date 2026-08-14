package tui

import (
	"strings"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/gatewayclient"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
)

func TestBuildTimelineOrder(t *testing.T) {
	task := &gatewayclient.Task{
		ID: "task-1", Title: "Do thing", Objective: "Do the thing carefully",
		State: gatewayclient.TaskStateRunning, CreatedAt: "t0", UpdatedAt: "t1",
	}
	plan := &gatewayclient.Plan{
		ID: "plan-1", Revision: 1, State: "ready",
		Steps: []gatewayclient.Step{{Title: "step"}},
	}
	result := "all good"
	runs := []gatewayclient.Run{{
		ID: "run-1", TaskID: "task-1", State: gatewayclient.RunStateCompleted,
		StartedAt: "t2", Result: &result,
	}}
	items := buildTimeline(task, plan, runs)
	if len(items) < 4 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].Kind != tlUser || !strings.Contains(items[0].Body, "carefully") {
		t.Fatalf("first = %#v", items[0])
	}
	var kinds []timelineKind
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	joined := ""
	for _, k := range kinds {
		joined += string(k) + ","
	}
	if !strings.Contains(joined, string(tlPlan)) || !strings.Contains(joined, string(tlRun)) {
		t.Fatalf("kinds = %s", joined)
	}
	text := renderTimeline(items)
	if !strings.Contains(text, "carefully") || !strings.Contains(text, "all good") {
		t.Fatalf("render = %s", text)
	}
}

func TestBuildTimelineNilTask(t *testing.T) {
	if items := buildTimeline(nil, nil, nil); items != nil {
		t.Fatalf("got %#v", items)
	}
}

func TestShortID(t *testing.T) {
	if shortID("abc") != "abc" {
		t.Fatal(shortID("abc"))
	}
	long := "task-" + strings.Repeat("x", 40)
	if !strings.HasSuffix(shortID(long), "…") {
		t.Fatal(shortID(long))
	}
}

func TestFoldBodyTruncates(t *testing.T) {
	long := strings.Repeat("line\n", timelineBodyMaxLines+5)
	out := foldBody(long)
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation marker: %q", out)
	}
	if strings.Count(out, "\n") > timelineBodyMaxLines+2 {
		t.Fatalf("still too many lines: %d", strings.Count(out, "\n"))
	}
}

func TestFoldTailKeepsLatest(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("L")
		b.WriteString(itoa(i))
		b.WriteByte('\n')
	}
	out := foldTail(b.String(), 3, 500)
	if !strings.Contains(out, "L19") || !strings.Contains(out, "expand") {
		t.Fatalf("tail fold = %q", out)
	}
	if strings.Contains(out, "L0\n") {
		t.Fatalf("should drop early lines: %q", out)
	}
}

func TestThinkingBlockCollapsed(t *testing.T) {
	longThink := strings.Repeat("reason step\n", 20)
	items := []timelineItem{{
		Kind: tlRun, Title: "assistant",
		Blocks: []contentBlock{{
			Kind: blockThinking, Text: longThink, Key: "t1",
		}, {
			Kind: blockReply, Text: "final answer", Key: "r1",
		}},
	}}
	out := renderTimelineExpanded(items, expandState{})
	if !strings.Contains(out, "thinking") || !strings.Contains(out, "expand") {
		t.Fatalf("expected collapsed thinking: %s", out)
	}
	if strings.Count(out, "reason step") > 2 {
		t.Fatalf("thinking not collapsed: %s", out)
	}
	if !strings.Contains(out, "final answer") {
		t.Fatalf("reply missing: %s", out)
	}
	// Expand.
	out2 := renderTimelineExpanded(items, expandState{keys: map[string]bool{"t1": true}})
	if strings.Count(out2, "reason step") < 10 {
		t.Fatalf("expected expanded thinking: %s", out2)
	}
}

func TestDoneBanner(t *testing.T) {
	items := []timelineItem{{
		Kind: tlDone, Title: "done · idle", State: gatewayclient.TaskStateCompleted,
	}}
	out := renderTimeline(items)
	if !strings.Contains(out, "done") || !strings.Contains(out, "─") {
		t.Fatalf("render = %s", out)
	}
}

func TestMessageRenderFlattened(t *testing.T) {
	items := []timelineItem{
		{Kind: tlUser, Title: "you", Body: "hello there"},
		{Kind: tlRun, Title: "assistant", Blocks: []contentBlock{
			{Kind: blockReply, Text: "hi back", Key: "r1"},
		}},
	}
	out := renderTimeline(items)
	if !strings.Contains(out, "hello") {
		t.Fatalf("user message missing: %s", out)
	}
	if !strings.Contains(out, "hi back") {
		t.Fatalf("assistant reply missing: %s", out)
	}
}

func TestLiveThinkingTail(t *testing.T) {
	items := appendLiveDraft(nil, strings.Repeat("think line\n", 30), "", nil)
	out := renderTimeline(items)
	if !strings.Contains(out, "thinking") {
		t.Fatalf("missing thinking: %s", out)
	}
	// Live fold shows tail marker with line count.
	if !strings.Contains(out, "lines") && !strings.Contains(out, "think line") {
		t.Fatalf("live thinking render: %s", out)
	}
}

func TestDropLiveDraft(t *testing.T) {
	base := []timelineItem{{Kind: tlUser, Title: "you", Body: "hi"}}
	with := appendLiveDraft(base, "", "hello", nil)
	if len(with) != 2 || with[1].Key != "live" {
		t.Fatalf("draft = %#v", with)
	}
	got := dropLiveDraft(with)
	if len(got) != 1 || got[0].Kind != tlUser {
		t.Fatalf("drop = %#v", got)
	}
	if dropLiveDraft(base)[0].Kind != tlUser {
		t.Fatal("drop without draft")
	}
}

func TestClearTaskResetsLiveStream(t *testing.T) {
	m := model{
		liveContent:   "partial",
		streamDirty:   true,
		streamPaintOn: true,
		streamMD:      streamingMD{cut: 4, src: "hi\n\n"},
		timeline:      []timelineItem{{Key: "live"}},
	}
	updated, _ := m.Update(commandDoneMsg{clearTask: true})
	got := updated.(model)
	if got.liveContent != "" || got.streamDirty || got.streamPaintOn || got.streamMD.cut != 0 {
		t.Fatalf("live leftover content=%q dirty=%v paint=%v cut=%d",
			got.liveContent, got.streamDirty, got.streamPaintOn, got.streamMD.cut)
	}
	if len(got.timeline) != 0 {
		t.Fatalf("timeline = %#v", got.timeline)
	}
}

func TestUpsertLiveDraftNoCaret(t *testing.T) {
	items := upsertLiveDraft(nil, "", "hello", nil)
	if len(items) != 1 || items[0].Key != "live" {
		t.Fatalf("items = %#v", items)
	}
	if len(items[0].Blocks) != 1 || items[0].Blocks[0].Text != "hello" {
		t.Fatalf("blocks = %#v", items[0].Blocks)
	}
	again := upsertLiveDraft(items, "", "hello world", nil)
	if len(again) != 1 || again[0].Blocks[0].Text != "hello world" {
		t.Fatalf("upsert = %#v", again)
	}
}

func TestTimelineRenderCacheHit(t *testing.T) {
	var cache timelineRenderCache
	opts := defaultRenderOpts()
	items := []timelineItem{{Kind: tlUser, Title: "you", Body: "hi", At: "t0"}}
	a := cache.render(items, expandState{}, opts)
	b := cache.render(items, expandState{}, opts)
	if a != b || a == "" {
		t.Fatalf("cache miss: %q vs %q", a, b)
	}
	items[0].Body = "changed"
	c := cache.render(items, expandState{}, opts)
	if c == a {
		t.Fatal("expected invalidate on body change")
	}
}

func TestExpandStateToggle(t *testing.T) {
	var e expandState
	e.toggle("k1")
	if !e.open("k1") {
		t.Fatal("expected open")
	}
	e.toggle("k1")
	if e.open("k1") {
		t.Fatal("expected closed")
	}
	e.setAll(true)
	if !e.open("any") {
		t.Fatal("all should open any key")
	}
	e.setAll(false)
	if e.open("any") {
		t.Fatal("none should close")
	}
}

func TestScheduleRefreshCoalesces(t *testing.T) {
	m := newModel(paths.ModeUser, &fakeGateway{})
	cmd1 := m.scheduleRefresh(refreshFull)
	if cmd1 == nil {
		t.Fatal("first refresh should start")
	}
	if !m.refreshing {
		t.Fatal("expected refreshing")
	}
	cmd2 := m.scheduleRefresh(refreshRuns)
	if cmd2 != nil {
		t.Fatal("second refresh should coalesce")
	}
	if !m.pendingRefresh {
		t.Fatal("expected pending")
	}
}
