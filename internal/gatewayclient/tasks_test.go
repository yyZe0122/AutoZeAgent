package gatewayclient

import "testing"

func TestTaskTitleTruncates(t *testing.T) {
	short := TaskTitle("hello")
	if short != "hello" {
		t.Fatalf("short = %q", short)
	}
	long := make([]rune, 100)
	for i := range long {
		long[i] = 'a'
	}
	title := TaskTitle(string(long))
	if len([]rune(title)) != 81 { // 80 + ellipsis
		t.Fatalf("len = %d title = %q", len([]rune(title)), title)
	}
}

func TestRandomID(t *testing.T) {
	id, err := RandomID("plan-")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) < 10 || id[:5] != "plan-" {
		t.Fatalf("id = %q", id)
	}
}
