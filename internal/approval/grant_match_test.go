package approval

import (
	"errors"
	"testing"
	"time"
)

func TestCommandArgsMatchSchemeA(t *testing.T) {
	tests := []struct {
		name      string
		grantCmd  string
		grantArgs []string
		reqCmd    string
		reqArgs   []string
		wantErr   bool
	}{
		{name: "empty grant accepts any", grantCmd: "", grantArgs: nil, reqCmd: "make", reqArgs: []string{"test"}, wantErr: false},
		{name: "empty grant empty request", grantCmd: "", grantArgs: []string{}, reqCmd: "", reqArgs: nil, wantErr: false},
		{name: "command match args empty grant", grantCmd: "git", grantArgs: nil, reqCmd: "git", reqArgs: []string{"status"}, wantErr: false},
		{name: "command mismatch", grantCmd: "git", grantArgs: nil, reqCmd: "echo", reqArgs: nil, wantErr: true},
		{name: "args exact match", grantCmd: "echo", grantArgs: []string{"hi"}, reqCmd: "echo", reqArgs: []string{"hi"}, wantErr: false},
		{name: "args mismatch when grant non-empty", grantCmd: "echo", grantArgs: []string{"hi"}, reqCmd: "echo", reqArgs: []string{"bye"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commandArgsMatch(tt.grantCmd, tt.grantArgs, tt.reqCmd, tt.reqArgs)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrGrantDenied) {
				t.Fatalf("err = %v, want ErrGrantDenied", err)
			}
		})
	}
}

func TestGrantMatchesRequestPathScopedProcess(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	grant := CapabilityGrant{
		ID: "g1", TaskID: "t1", PlanID: "p1", StepID: "s1", PlanHash: "hash",
		Scope: CapabilityScope{
			Capability: "process_exec", Paths: []string{"/tmp/ws"},
			MaxDurationMillis: 60_000, MaxCalls: 100,
		},
		ExpiresAt: now.Add(time.Hour),
	}
	req := GrantRequest{
		TaskID: "t1", PlanID: "p1", StepID: "s1", PlanHash: "hash",
		Capability: "process_exec", Path: "/tmp/ws", Command: "make", Arguments: []string{"test"},
		Duration: time.Second,
	}
	if err := grantMatchesRequest(grant, req); err != nil {
		t.Fatalf("path-scoped process grant: %v", err)
	}
	req.Path = "/other"
	if err := grantMatchesRequest(grant, req); err == nil {
		t.Fatal("expected path denial")
	}
}

func TestNormalizeProcessExecEmptyCommand(t *testing.T) {
	got, err := NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "process_exec", Paths: []string{"/tmp/ws"},
		MaxDurationMillis: 1000, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "" {
		t.Fatalf("command = %q", got.Command)
	}
	_, err = NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "process_exec", MaxDurationMillis: 1000, MaxCalls: 1,
	})
	if err == nil {
		t.Fatal("process_exec without paths must fail")
	}
}

func TestNormalizeGitRequiresPaths(t *testing.T) {
	got, err := NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "git_status", Paths: []string{"/tmp/repo"},
		MaxDurationMillis: 1000, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Capability != "git_status" {
		t.Fatalf("got %#v", got)
	}
	_, err = NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "git_status", MaxDurationMillis: 1000, MaxCalls: 1,
	})
	if err == nil {
		t.Fatal("git without paths must fail")
	}
}
