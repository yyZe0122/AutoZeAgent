package editrev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/artifacts"
	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

func TestSnapshotAndRewind(t *testing.T) {
	ctx := context.Background()
	db, err := coresqlite.Open(ctx, t.TempDir()+"/core.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	arts, err := artifacts.NewStore(db.SQL(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db.SQL(), arts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	ctx = runmeta.With(ctx, runmeta.Context{SessionID: "s1", RunID: "r1"})
	afterSum := sha256.Sum256([]byte("new\n"))
	afterHex := hex.EncodeToString(afterSum[:])
	if err := store.SnapshotBeforeWrite(ctx, path, []byte("old\n"), afterHex); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := store.Rewind(ctx, "s1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != path {
		t.Fatalf("path=%s", got.Path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old\n" {
		t.Fatalf("rewound=%q", body)
	}
}
