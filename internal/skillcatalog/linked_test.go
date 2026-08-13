package skillcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndReadLinked(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, root, "demo", `---
name: Demo
description: Demo skill body
---
Main instructions.
`)
	refDir := filepath.Join(root, "demo", "references")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "api.md"), []byte("# api\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo", "SKILL.md.draft"), []byte("draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, diags := Discover([]Root{{Path: root, Source: SourceUser}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	files, err := cat.ListLinked("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "references/api.md" {
		t.Fatalf("linked = %v", files)
	}
	body, err := cat.ReadLinked("demo", "references/api.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# api\n" {
		t.Fatalf("body = %q", body)
	}
	if _, err := cat.ReadLinked("demo", "../outside.md"); err == nil {
		t.Fatal("expected traversal reject")
	}
	if _, err := cat.ReadLinked("demo", "SKILL.md"); err == nil {
		t.Fatal("expected SKILL.md reject")
	}
}
