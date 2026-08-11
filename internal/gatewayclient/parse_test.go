package gatewayclient

import "testing"

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
