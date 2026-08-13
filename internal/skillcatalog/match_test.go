package skillcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchByIDNameTriggers(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, root, "git-helper", `---
name: Git Helper
description: Review commits and rebase
triggers: rebase, cherry-pick
---
Use git carefully.
`)
	writeSkillMD(t, root, "go-test", `---
name: Go tests
description: run package tests
---
go test -count=1
`)
	cat, diags := Discover([]Root{{Path: root, Source: SourceUser}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	hits := cat.Match("please rebase this branch", 3)
	if len(hits) != 1 || hits[0].ID != "git-helper" {
		t.Fatalf("rebase hits = %+v", hits)
	}
	hits = cat.Match("git-helper please", 3)
	if len(hits) != 1 || hits[0].ID != "git-helper" {
		t.Fatalf("id hits = %+v", hits)
	}
	hits = cat.Match("run package tests now", 3)
	if len(hits) != 1 || hits[0].ID != "go-test" {
		t.Fatalf("desc hits = %+v", hits)
	}
	if got := cat.Match("unrelated weather", 3); len(got) != 0 {
		t.Fatalf("want no hits, got %+v", got)
	}
	if got := cat.Match("contest season", 3); len(got) != 0 {
		t.Fatalf("short desc token must not hit contest: %+v", got)
	}
}

func TestMatchTriggerBeatsDescription(t *testing.T) {
	root := t.TempDir()
	writeSkillMD(t, root, "aaa-desc", `---
name: Desc First
description: rebase helper notes
---
body
`)
	writeSkillMD(t, root, "zzz-trig", `---
name: Trigger Last
description: other workflow
triggers: rebase
---
body
`)
	cat, diags := Discover([]Root{{Path: root, Source: SourceUser}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	hits := cat.Match("please rebase this", 3)
	if len(hits) < 2 || hits[0].ID != "zzz-trig" {
		t.Fatalf("trigger should rank first: %+v", hits)
	}
}

func writeSkillMD(t *testing.T, root, id, body string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
