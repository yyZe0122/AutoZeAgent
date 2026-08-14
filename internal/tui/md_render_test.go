package tui

import (
	"strings"
	"testing"
)

func TestUnclosedFence(t *testing.T) {
	if !unclosedFence("hi\n```go\nfunc") {
		t.Fatal("want unclosed")
	}
	if unclosedFence("hi\n```go\nfunc\n```") {
		t.Fatal("want closed")
	}
}

func TestStreamingMDKeepsUnclosedPlain(t *testing.T) {
	var s streamingMD
	src := "```md\n**bold**"
	if got := s.render(src, 80, ThemeNight); got != src {
		t.Fatalf("got %q", got)
	}
}

func TestSafeMarkdownCutBlankLine(t *testing.T) {
	src := "hello\n\nworld still growing"
	cut := safeMarkdownCut(src)
	if cut != len("hello\n\n") {
		t.Fatalf("cut = %d want %d (%q)", cut, len("hello\n\n"), src[:cut])
	}
}

func TestSafeMarkdownCutSkipsOpenFence(t *testing.T) {
	src := "intro\n\n```go\nfunc main() {\n"
	cut := safeMarkdownCut(src)
	if cut != len("intro\n\n") {
		t.Fatalf("cut = %d (%q)", cut, src[:cut])
	}
}

func TestSafeMarkdownCutCrossesList(t *testing.T) {
	src := "intro\n\n- item\n\nmore growing"
	cut := safeMarkdownCut(src)
	if cut != len("intro\n\n- item\n\n") {
		t.Fatalf("cut = %d (%q)", cut, src[:cut])
	}
}

func TestStreamingMDFreezesPrefixTrailPlain(t *testing.T) {
	var s streamingMD
	a := s.render("hello\n\nwor", 80, ThemeNight)
	b := s.render("hello\n\nworld", 80, ThemeNight)
	if a == "" || b == "" {
		t.Fatal("empty render")
	}
	first, _, _ := strings.Cut(a, "\n")
	if first != "" && !strings.Contains(b, first) {
		t.Fatalf("prefix drifted:\n%q\n%q", a, b)
	}
	if !strings.Contains(b, "world") {
		t.Fatalf("trail missing: %q", b)
	}
}

func TestStreamingMDTrailStaysPlain(t *testing.T) {
	var s streamingMD
	got := s.render("hello\n\n**wo", 80, ThemeNight)
	if !strings.Contains(got, "**wo") {
		t.Fatalf("trail should stay raw: %q", got)
	}
}
