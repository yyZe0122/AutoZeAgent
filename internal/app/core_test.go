package app

import (
	"context"
	"testing"

	"autozeagent.local/autozeagent/internal/platform/paths"
)

func TestCoreIsHealthy(t *testing.T) {
	layout, err := paths.Resolve(paths.ModeUser)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	core, err := New(Config{Runtime: layout, Version: "test"})
	if err != nil {
		t.Fatalf("new core: %v", err)
	}
	status := core.Status()
	if status.State != StateHealthy {
		t.Fatalf("state = %s, want healthy", status.State)
	}
	if status.Version != "test" {
		t.Fatalf("version = %q, want test", status.Version)
	}
}

func TestCoreRunsAndStopsBackgroundRunner(t *testing.T) {
	layout, _ := paths.Resolve(paths.ModeUser)
	runner := &fakeBackgroundRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	core, err := New(Config{Runtime: layout, BackgroundRunners: []BackgroundRunner{runner}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- core.Run(ctx) }()
	<-runner.started
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.stopped:
	default:
		t.Fatal("background runner did not stop before Core returned")
	}
}

type fakeBackgroundRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func (r *fakeBackgroundRunner) Run(ctx context.Context) {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
}

func TestCoreAddsBackgroundRunnerBeforeRun(t *testing.T) {
	layout, _ := paths.Resolve(paths.ModeUser)
	core, err := New(Config{Runtime: layout})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.AddBackgroundRunner(nil); err == nil {
		t.Fatal("nil background runner was accepted")
	}
	runner := &fakeBackgroundRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	if err := core.AddBackgroundRunner(runner); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- core.Run(ctx) }()
	<-runner.started
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCoreRejectsBackgroundRunnerAfterRunStarts(t *testing.T) {
	layout, _ := paths.Resolve(paths.ModeUser)
	runner := &fakeBackgroundRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	core, err := New(Config{Runtime: layout, BackgroundRunners: []BackgroundRunner{runner}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- core.Run(ctx) }()
	<-runner.started
	if err := core.AddBackgroundRunner(&fakeBackgroundRunner{started: make(chan struct{}), stopped: make(chan struct{})}); err == nil {
		cancel()
		<-done
		t.Fatal("background runner was accepted after Core started")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := core.Run(context.Background()); err == nil {
		t.Fatal("Core unexpectedly ran twice")
	}
}
