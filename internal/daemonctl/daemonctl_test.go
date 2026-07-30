package daemonctl

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteReadRemovePID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	pid, err := readPID(dir)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want %d", pid, os.Getpid())
	}
	if err := RemovePID(dir); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}
	if _, err := os.Stat(pidPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("pid file still present: %v", err)
	}
}

func TestRemovePIDSkipsOtherProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	other := os.Getpid() + 100000
	if err := os.WriteFile(pidPath(dir), []byte(strconv.Itoa(other)+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := RemovePID(dir); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}
	if _, err := os.Stat(pidPath(dir)); err != nil {
		t.Fatalf("expected foreign pid file kept: %v", err)
	}
}

func TestReadPIDInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(pidPath(dir), []byte("not-a-pid\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readPID(dir); err == nil {
		t.Fatal("expected error for invalid pid")
	}
}

func TestPidPath(t *testing.T) {
	t.Parallel()
	got := pidPath("/tmp/runtime")
	want := filepath.Join("/tmp/runtime", pidFilename)
	if got != want {
		t.Fatalf("pidPath = %q, want %q", got, want)
	}
}
