package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		value string
		want  slog.Level
	}{
		{value: "debug", want: slog.LevelDebug},
		{value: "info", want: slog.LevelInfo},
		{value: "WARN", want: slog.LevelWarn},
		{value: "warning", want: slog.LevelWarn},
		{value: "error", want: slog.LevelError},
		{value: "", want: slog.LevelInfo},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := parseLogLevel(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("parseLogLevel(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
	if _, err := parseLogLevel("trace"); err == nil {
		t.Fatal("parseLogLevel(trace) succeeded, want error")
	}
}

func TestReplaceLogAttrRedactsSecretsOnly(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{ReplaceAttr: replaceLogAttr}))
	logger.Info("provider request", "api_key", "secret-value", "input_tokens", 42)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if got := record["api_key"]; got != "[REDACTED]" {
		t.Fatalf("api_key = %v, want redacted", got)
	}
	if got := record["input_tokens"]; got != float64(42) {
		t.Fatalf("input_tokens = %v, want 42", got)
	}
}

func TestRotateLog(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, daemonLogName)
	if err := os.WriteFile(current, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current+".1", []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateLog(current, 1, 3); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, current+".1", "current")
	assertFileContent(t, current+".2", "previous")
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatalf("current log still exists after rotation: %v", err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", path, content, want)
	}
}
