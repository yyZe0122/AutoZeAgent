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
	for _, want := range []string{"/new", "/tasks", "/cron", "/compact", "/perm", "/expand", "/journey", "/memory", "/refresh-memory", "/model", "/skills", "/theme", "Tab", "every", "skill-id", "e / E / c"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q", want)
		}
	}
	if strings.Contains(text, "/approve") {
		t.Fatal("help should not list removed /approve")
	}
	if strings.Contains(text, "a / r") || strings.Contains(text, "approve → run") {
		t.Fatal("help should not advertise removed approval keys/workflow")
	}
}

func TestPaintKeywordsHighlightsSlash(t *testing.T) {
	out := paintKeywords("try /new then /skills apply")
	want := "try " + styleKeyword.Render("/new") + " then " + styleKeyword.Render("/skills") + " apply"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestIsBuiltinSlash(t *testing.T) {
	if !isBuiltinSlash("/model") || !isBuiltinSlash("/MODEL") {
		t.Fatal("expected /model builtin")
	}
	if isBuiltinSlash("/git") {
		t.Fatal("/git should not be builtin")
	}
}
