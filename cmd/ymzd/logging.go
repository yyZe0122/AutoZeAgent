package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/version"
)

const (
	daemonLogName    = "ymzd.jsonl"
	daemonLogMaxSize = 10 << 20
	daemonLogBackups = 3
)

func defaultLogLevel() string {
	if value := strings.TrimSpace(os.Getenv("YMZ_LOG_LEVEL")); value != "" {
		return value
	}
	return "info"
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q: expected debug, info, warn, or error", value)
	}
}

func configureLogging(logDir string, level slog.Level, mode string) (*os.File, error) {
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	logPath := filepath.Join(logDir, daemonLogName)
	if err := rotateLog(logPath, daemonLogMaxSize, daemonLogBackups); err != nil {
		return nil, fmt.Errorf("rotate daemon log: %w", err)
	}
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("secure daemon log: %w", err)
	}
	handler := slog.NewJSONHandler(io.MultiWriter(os.Stderr, file), &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceLogAttr,
	})
	slog.SetDefault(slog.New(handler).With(
		"service", "ymzd",
		"mode", mode,
		"version", version.Version,
	))
	return file, nil
}

func rotateLog(path string, maxBytes int64, backups int) error {
	if maxBytes <= 0 || backups <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() < maxBytes {
		return nil
	}
	if err := os.Remove(path + fmt.Sprintf(".%d", backups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := backups - 1; index >= 1; index-- {
		oldPath := path + fmt.Sprintf(".%d", index)
		newPath := path + fmt.Sprintf(".%d", index+1)
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(path, path+".1")
}

func replaceLogAttr(_ []string, attr slog.Attr) slog.Attr {
	switch strings.ToLower(attr.Key) {
	case "authorization", "api_key", "apikey", "access_token", "refresh_token", "secret", "password",
		"headers", "prompt", "arguments", "request_body", "response_body", "content":
		return slog.String(attr.Key, "[REDACTED]")
	default:
		return attr
	}
}
