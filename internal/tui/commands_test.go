package tui

import (
	"strings"
	"testing"
)

func TestParseSlash(t *testing.T) {
	name, arg := parseSlash("/new hello world")
	if name != "/new" || arg != "hello world" {
		t.Fatalf("got %q %q", name, arg)
	}
	name, arg = parseSlash("plain text")
	if name != "" || arg != "plain text" {
		t.Fatalf("got %q %q", name, arg)
	}
}

func TestCanonicalAliases(t *testing.T) {
	if canonicalSlash("/q") != "/quit" {
		t.Fatal(canonicalSlash("/q"))
	}
	if canonicalSlash("/exit") != "/quit" {
		t.Fatal(canonicalSlash("/exit"))
	}
	if canonicalSlash("/clear") != "/back" {
		t.Fatal(canonicalSlash("/clear"))
	}
	if canonicalSlash("/sessions") != "/sessions" {
		t.Fatal(canonicalSlash("/sessions"))
	}
	name, _ := parseSlash("/q")
	if name != "/quit" {
		t.Fatalf("parse alias = %q", name)
	}
}

func TestHelpTextListsCommands(t *testing.T) {
	text := helpText()
	for _, want := range []string{"/new", "/tasks", "/approve", "/cron", "/model", "/theme", "Tab"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q", want)
		}
	}
}
