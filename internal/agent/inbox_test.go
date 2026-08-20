package agent

import "testing"

func TestInboxClaimStepFIFOAndDedup(t *testing.T) {
	in := NewInbox()
	in.Steer("s1", "a", "first")
	in.Steer("s1", "a", "dup")
	in.Steer("s1", "b", "second")
	in.Steer("s2", "d", "other")

	got := in.ClaimStep("s1")
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("claim step = %+v", got)
	}
	if in.Pending("s1") {
		t.Fatal("s1 next-step should be empty")
	}
	if !in.Pending("s2") {
		t.Fatal("s2 must be untouched")
	}
}

func TestInboxClearDropsSessionOnly(t *testing.T) {
	in := NewInbox()
	in.Steer("s1", "a", "one")
	in.Steer("s2", "b", "two")
	in.Clear("s1")
	if in.Pending("s1") {
		t.Fatal("s1 should be cleared")
	}
	if !in.Pending("s2") {
		t.Fatal("s2 must be untouched")
	}
	in.Clear("")
	if in.Pending("s2") {
		t.Fatal("empty sessionID clears all")
	}
}
