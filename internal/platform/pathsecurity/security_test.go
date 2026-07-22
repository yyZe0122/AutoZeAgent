package pathsecurity

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveExistingHandlesMissingSuffix(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing", "file.txt")
	resolved, err := ResolveExisting(target)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !Contains(root, resolved) {
		t.Fatalf("resolved path %q escaped %q", resolved, root)
	}
}

func TestContainsResolvedRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	createTestDirectoryLink(t, outside, link)
	if ContainsResolved(root, filepath.Join(link, "new.txt")) {
		t.Fatal("symlink escape was allowed")
	}
}
func createTestDirectoryLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Skipf("symlink unavailable: %v", err)
	}
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("symlink and junction unavailable: %v: %s", err, output)
	}
}
