package tui

import "testing"

func TestUnclosedFence(t *testing.T) {
	if !unclosedFence("hi\n```go\nfunc") {
		t.Fatal("want unclosed")
	}
	if unclosedFence("hi\n```go\nfunc\n```") {
		t.Fatal("want closed")
	}
}

func TestRenderLiveMarkdownKeepsUnclosedPlain(t *testing.T) {
	src := "```md\n**bold**"
	if got := renderLiveMarkdown(src, 80, ThemeNight, "k1"); got != src {
		t.Fatalf("got %q", got)
	}
}

func TestRenderLiveMarkdownKeysDoNotCollide(t *testing.T) {
	liveMDMu.Lock()
	liveMDCache = nil
	liveMDMu.Unlock()
	a := renderLiveMarkdown("**one**", 80, ThemeNight, "stream-a")
	b := renderLiveMarkdown("**two**", 80, ThemeNight, "stream-b")
	if a == b {
		t.Fatalf("different keys must not share output: %q", a)
	}
	again := renderLiveMarkdown("**one**", 80, ThemeNight, "stream-a")
	if again != a {
		t.Fatalf("same key should hit cache: %q vs %q", again, a)
	}
}
