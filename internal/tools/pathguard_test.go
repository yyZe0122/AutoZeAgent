package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathGuardResolvesRelativeAgainstRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := guard.Resolve("sub")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(nested)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("Resolve(relative) = %q, want %q", resolved, want)
	}
}

func TestPathGuardRejectsEscape(t *testing.T) {
	root := t.TempDir()
	guard, err := NewPathGuard([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Resolve(filepath.Join("..", "outside")); err == nil {
		t.Fatal("Resolve(escape) succeeded; want error")
	}
}
