package approval

import (
	"errors"
	"testing"
)

func TestNormalizeCapabilityProcessExecRequiresAbsolutePath(t *testing.T) {
	_, err := NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "process_exec", Command: "echo", Arguments: nil,
		MaxDurationMillis: 1000, MaxCalls: 1, OneTime: true,
	})
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty paths error = %v, want ErrInvalidPlan", err)
	}

	_, err = NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "process_exec", Command: "echo", Paths: []string{"relative"},
		Arguments: nil, MaxDurationMillis: 1000, MaxCalls: 1, OneTime: true,
	})
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("relative path error = %v, want ErrInvalidPlan", err)
	}

	got, err := NormalizeCapabilityForPlan(CapabilityScope{
		Capability: "process_exec", Command: "echo", Paths: []string{"/tmp/workspace"},
		Arguments: nil, MaxDurationMillis: 1000, MaxCalls: 1, OneTime: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Arguments == nil {
		t.Fatal("arguments must normalize nil to empty slice")
	}
	if len(got.Arguments) != 0 {
		t.Fatalf("arguments = %#v, want empty", got.Arguments)
	}
	if got.Command != "echo" || len(got.Paths) != 1 {
		t.Fatalf("normalized = %+v", got)
	}
}
