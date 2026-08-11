package providerconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsMissingOnly(t *testing.T) {
	t.Setenv("AZE_TEST_EXISTING", "keep-me")
	dir := t.TempDir()
	path := filepath.Join(dir, "env")
	content := `
# comment
export AZE_TEST_NEW=hello
AZE_TEST_EXISTING=should-not-win
AZE_TEST_QUOTED="quoted value"
AZE_TEST_EMPTY=
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("AZE_TEST_NEW"); got != "hello" {
		t.Fatalf("AZE_TEST_NEW=%q", got)
	}
	if got := os.Getenv("AZE_TEST_EXISTING"); got != "keep-me" {
		t.Fatalf("existing overridden: %q", got)
	}
	if got := os.Getenv("AZE_TEST_QUOTED"); got != "quoted value" {
		t.Fatalf("quoted=%q", got)
	}
}

func TestLoadEnvFileMissingOK(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureEnvFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	created, path, err := EnsureEnvFile(dir)
	if err != nil || !created {
		t.Fatalf("created=%v path=%s err=%v", created, path, err)
	}
	if err := os.WriteFile(path, []byte("AZE_MARK=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created2, _, err := EnsureEnvFile(dir)
	if err != nil || created2 {
		t.Fatalf("second ensure created=%v err=%v", created2, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "AZE_MARK=1\n" {
		t.Fatalf("template overwrote user env: %q", raw)
	}
}
