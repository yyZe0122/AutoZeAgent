package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
)

const (
	daemonLogFileName = "ymzd.jsonl"
	maxLogTail        = 10000
)

type logFilters struct {
	tail      int
	level     string
	component string
	runID     string
	sessionID string
	taskID    string
}

func runLogs(args []string) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	tail := flags.Int("tail", 200, "number of matching log entries to print")
	level := flags.String("level", "", "filter by log level")
	component := flags.String("component", "", "filter by component")
	runID := flags.String("run", "", "filter by run ID")
	sessionID := flags.String("session", "", "filter by session ID")
	taskID := flags.String("task", "", "filter by task ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("logs does not accept positional arguments")
	}
	if *tail < 1 || *tail > maxLogTail {
		return fmt.Errorf("tail must be between 1 and %d", maxLogTail)
	}
	levelValue := strings.ToLower(strings.TrimSpace(*level))
	if levelValue != "" {
		switch levelValue {
		case "debug", "info", "warn", "error":
		default:
			return errors.New("level must be debug, info, warn, or error")
		}
	}
	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	return writeLogs(os.Stdout, filepath.Join(layout.LogDir, daemonLogFileName), logFilters{
		tail:      *tail,
		level:     levelValue,
		component: strings.TrimSpace(*component),
		runID:     strings.TrimSpace(*runID),
		sessionID: strings.TrimSpace(*sessionID),
		taskID:    strings.TrimSpace(*taskID),
	})
}

func writeLogs(output io.Writer, logPath string, filters logFilters) error {
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	defer file.Close()

	lines := make([]string, filters.tail)
	matched := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		var record struct {
			Level     string `json:"level"`
			Component string `json:"component"`
			RunID     string `json:"run_id"`
			SessionID string `json:"session_id"`
			TaskID    string `json:"task_id"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if filters.level != "" && strings.ToLower(record.Level) != filters.level {
			continue
		}
		if filters.component != "" && record.Component != filters.component {
			continue
		}
		if filters.runID != "" && record.RunID != filters.runID {
			continue
		}
		if filters.sessionID != "" && record.SessionID != filters.sessionID {
			continue
		}
		if filters.taskID != "" && record.TaskID != filters.taskID {
			continue
		}
		lines[matched%filters.tail] = line
		matched++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read daemon log: %w", err)
	}
	start := 0
	count := matched
	if count > filters.tail {
		start = matched % filters.tail
		count = filters.tail
	}
	for index := 0; index < count; index++ {
		if _, err := fmt.Fprintln(output, lines[(start+index)%filters.tail]); err != nil {
			return err
		}
	}
	return nil
}
