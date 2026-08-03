package tui

import (
	"strings"
	"testing"

	"autozeagent.local/autozeagent/internal/gatewayclient"
	"autozeagent.local/autozeagent/internal/platform/paths"
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
	if !strings.Contains(text, "objective") || !strings.Contains(text, "all good") {
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

func TestTimelineRenderCacheHit(t *testing.T) {
	var cache timelineRenderCache
	items := []timelineItem{{Kind: tlUser, Title: "objective", Body: "hi", At: "t0"}}
	a := cache.render(items)
	b := cache.render(items)
	if a != b || a == "" {
		t.Fatalf("cache miss: %q vs %q", a, b)
	}
	items[0].Body = "changed"
	c := cache.render(items)
	if c == a {
		t.Fatal("expected invalidate on body change")
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
