package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	"github.com/yyZe0122/yunmengze-agent/internal/skillcatalog"
)

func TestSkillsListAndView(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "git-helper", `---
name: Git Helper
description: Review commits and rebase
triggers: rebase
---
Use git carefully.
`)
	if err := os.MkdirAll(filepath.Join(root, "git-helper", "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "git-helper", "references", "notes.md"), []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, diags := skillcatalog.Discover([]skillcatalog.Root{{Path: root, Source: skillcatalog.SourceUser}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	broker := &Broker{registry: map[string]Tool{}}
	if err := RegisterSkillTools(broker, cat, nil); err != nil {
		t.Fatal(err)
	}
	list := broker.registry["skills_list"]
	view := broker.registry["skill_view"]
	if list == nil || view == nil {
		t.Fatalf("tools missing: %v", broker.registry)
	}
	raw, err := list.Execute(context.Background(), json.RawMessage(`{"query":"please rebase"}`))
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Skills []struct {
			ID        string `json:"id"`
			Suggested bool   `json:"suggested"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Skills) != 1 || listed.Skills[0].ID != "git-helper" || !listed.Skills[0].Suggested {
		t.Fatalf("list = %s", raw)
	}
	ctx := runmeta.With(context.Background(), runmeta.Context{RunID: "run-1", Actor: "agent"})
	body, err := view.Execute(ctx, json.RawMessage(`{"name":"git-helper"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Use git carefully") || !strings.Contains(string(body), "references/notes.md") {
		t.Fatalf("view = %s", body)
	}
	dup, err := view.Execute(ctx, json.RawMessage(`{"name":"git-helper"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dup), `"deduped":true`) {
		t.Fatalf("dedup = %s", dup)
	}
	linked, err := view.Execute(ctx, json.RawMessage(`{"name":"git-helper","file_path":"references/notes.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(linked), "note") {
		t.Fatalf("linked = %s", linked)
	}
	if _, err := view.Execute(ctx, json.RawMessage(`{"name":"missing"}`)); err == nil {
		t.Fatal("expected missing skill error")
	}
}

type archiveAll struct{}

func (archiveAll) ArchivedSkillIDs(context.Context) map[string]struct{} {
	return map[string]struct{}{"git-helper": {}}
}
func (archiveAll) RecordUsed(context.Context, []string, string) error { return nil }

func TestSkillViewRejectsArchived(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "git-helper", `---
name: Git Helper
description: Review commits and rebase
---
body
`)
	cat, _ := skillcatalog.Discover([]skillcatalog.Root{{Path: root, Source: skillcatalog.SourceUser}})
	broker := &Broker{registry: map[string]Tool{}}
	if err := RegisterSkillTools(broker, cat, archiveAll{}); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.registry["skill_view"].Execute(context.Background(), json.RawMessage(`{"name":"git-helper"}`)); err == nil {
		t.Fatal("archived skill should fail")
	}
	raw, err := broker.registry["skills_list"].Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "git-helper") {
		t.Fatalf("archived skill listed: %s", raw)
	}
}

func TestSkillDraftProposeAndDiscard(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "demo", `---
name: Demo
description: A demo skill for tests
---
old body
`)
	cat, diags := skillcatalog.Discover([]skillcatalog.Root{{Path: root, Source: skillcatalog.SourceUser}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	broker := &Broker{registry: map[string]Tool{}}
	if err := RegisterSkillDraftTool(broker, SkillDraftAdapter{Catalog: cat}); err != nil {
		t.Fatal(err)
	}
	tool := broker.registry["skill_draft"]
	body := "---\nname: Demo\ndescription: A demo skill for tests\n---\nnew body\n"
	raw, err := json.Marshal(map[string]string{"action": "propose", "skill_id": "demo", "body": body})
	if err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"ok":true`) {
		t.Fatalf("propose = %s", out)
	}
	discard, err := json.Marshal(map[string]string{"action": "discard", "skill_id": "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), discard); err != nil {
		t.Fatal(err)
	}
}

func TestSkillDraftRejectsSystem(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "sys", `---
name: Sys
description: system skill
---
body
`)
	cat, diags := skillcatalog.Discover([]skillcatalog.Root{{Path: root, Source: skillcatalog.SourceSystem}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	broker := &Broker{registry: map[string]Tool{}}
	if err := RegisterSkillDraftTool(broker, SkillDraftAdapter{Catalog: cat}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{
		"action": "propose", "skill_id": "sys",
		"body": "---\nname: Sys\ndescription: system skill\n---\nx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.registry["skill_draft"].Execute(context.Background(), raw); err == nil {
		t.Fatal("system skill draft should fail")
	}
}

func writeTestSkill(t *testing.T, root, id, body string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, skillcatalog.SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
