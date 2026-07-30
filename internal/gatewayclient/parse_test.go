package gatewayclient

import "testing"

func TestParseApprovalAction(t *testing.T) {
	action, err := ParseApprovalAction("")
	if err != nil || action != ActionAllowPlan {
		t.Fatalf("default: got %q %v", action, err)
	}
	action, err = ParseApprovalAction("reject")
	if err != nil || action != ActionReject {
		t.Fatalf("reject: got %q %v", action, err)
	}
	if _, err := ParseApprovalAction("nope"); err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestParseTaskAction(t *testing.T) {
	action, ok := ParseTaskAction("/pause")
	if !ok || action != TaskActionPause {
		t.Fatalf("pause: %q %v", action, ok)
	}
	action, ok = ParseTaskAction("resume")
	if !ok || action != TaskActionResume {
		t.Fatalf("resume: %q %v", action, ok)
	}
	if _, ok := ParseTaskAction("/unknown"); ok {
		t.Fatal("expected false")
	}
}
