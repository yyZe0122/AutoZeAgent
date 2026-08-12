package tui

import "testing"

func TestExpandChatCommandTemplate(t *testing.T) {
	t.Parallel()
	if got := expandChatCommandTemplate("Review:\n$ARGUMENTS", "x"); got != "Review:\nx" {
		t.Fatalf("%q", got)
	}
	if got := expandChatCommandTemplate("plain", "a"); got != "plain\n\na" {
		t.Fatalf("%q", got)
	}
}
