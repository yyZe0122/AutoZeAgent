package skillmaintain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/skillcatalog"
	storesqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func writeSkill(t *testing.T, root, id, body string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, skillcatalog.SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const sampleSkill = `---
name: Demo
description: A demo skill for tests
---
Use absolute paths.
`

const draftSkill = `---
name: Demo
description: A demo skill for tests
---
Prefer tests with -count=1.
`

func TestApplyDraftAndUsage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSkill(t, root, "demo", sampleSkill)
	cat, diags := skillcatalog.Discover([]skillcatalog.Root{{Path: root, Source: skillcatalog.SourceUser}})
	if len(diags) > 0 {
		t.Fatalf("discover: %v", diags)
	}
	database, err := storesqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := NewStore(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	svc, err := New(Config{Store: store, Catalog: cat, UnusedTTL: 24 * time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cat.WriteDraft("demo", draftSkill); err != nil {
		t.Fatal(err)
	}
	sk, err := svc.ApplyDraft(ctx, "demo", "tester")
	if err != nil {
		t.Fatal(err)
	}
	if sk.ID != "demo" {
		t.Fatalf("skill = %+v", sk)
	}
	body, err := os.ReadFile(filepath.Join(root, "demo", skillcatalog.SkillFileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != draftSkill {
		t.Fatalf("applied body = %q", body)
	}
	if err := svc.RecordUsed(ctx, []string{"demo"}, "user"); err != nil {
		t.Fatal(err)
	}
	usage, err := store.GetUsage(ctx, "demo")
	if err != nil || usage.LastUsedAt == "" || usage.ArchivedAt != "" {
		t.Fatalf("usage = %+v err=%v", usage, err)
	}
	events, err := store.ListEvents(ctx, "demo", 10)
	if err != nil || len(events) < 2 {
		t.Fatalf("events = %+v err=%v", events, err)
	}
}

func TestArchiveExpiredSkipsNeverUsed(t *testing.T) {
	ctx := context.Background()
	database, err := storesqlite.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := NewStore(database.SQL())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	svc, err := New(Config{Store: store, UnusedTTL: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if _, err := database.SQL().ExecContext(ctx, `
		INSERT INTO skill_usage(skill_id, last_used_at, archived_at, updated_at) VALUES(?,?,?,?)`,
		"stale", old, "", old); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL().ExecContext(ctx, `
		INSERT INTO skill_usage(skill_id, last_used_at, archived_at, updated_at) VALUES(?,?,?,?)`,
		"never", "", "", old); err != nil {
		t.Fatal(err)
	}
	svc.Maintain(ctx)
	stale, _ := store.GetUsage(ctx, "stale")
	never, _ := store.GetUsage(ctx, "never")
	if stale.ArchivedAt == "" {
		t.Fatal("stale should archive")
	}
	if never.ArchivedAt != "" {
		t.Fatal("never-used must not archive")
	}
	events, err := store.ListEvents(ctx, "stale", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Action == ActionArchive && e.Actor == "system" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing archive event: %+v", events)
	}
	neverEvents, err := store.ListEvents(ctx, "never", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(neverEvents) != 0 {
		t.Fatalf("never-used must not emit archive: %+v", neverEvents)
	}
}
