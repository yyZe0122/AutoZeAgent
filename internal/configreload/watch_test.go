package configreload

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
)

func TestIsWatchedEvent(t *testing.T) {
	cases := []struct {
		name string
		op   fsnotify.Op
		want bool
	}{
		{providerconfig.Filename, fsnotify.Write, true},
		{providerconfig.LocalFilename, fsnotify.Create, true},
		{providerconfig.EnvFilename, fsnotify.Rename, true},
		{"agent.json.tmp", fsnotify.Write, true},
		{"agent.json.bak", fsnotify.Write, false},
		{"other.txt", fsnotify.Write, false},
		{"core.db", fsnotify.Write, false},
		{"env.old", fsnotify.Write, false},
	}
	for _, tc := range cases {
		ev := fsnotify.Event{Name: filepath.Join("/tmp/cfg", tc.name), Op: tc.op}
		if got := isWatchedEvent(ev); got != tc.want {
			t.Fatalf("%s op=%v: got %v want %v", tc.name, tc.op, got, tc.want)
		}
	}
}

func TestDebounceMergesEvents(t *testing.T) {
	var calls atomic.Int32
	w, err := New(Options{
		ConfigDir: t.TempDir(),
		Debounce:  40 * time.Millisecond,
		OnChange:  func() { calls.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	w.schedule()
	w.schedule()
	w.schedule()
	time.Sleep(80 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d want 1", got)
	}
}

func TestFireCoalescesWhileRunning(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	w, err := New(Options{
		ConfigDir: t.TempDir(),
		Debounce:  time.Millisecond,
		OnChange: func() {
			n := calls.Add(1)
			if n == 1 {
				close(started)
				<-release
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	go w.fire()
	<-started
	// While first OnChange blocked, mark dirty via fire
	w.fire()
	close(release)
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d want 2 (initial + dirty re-run)", got)
	}
}
