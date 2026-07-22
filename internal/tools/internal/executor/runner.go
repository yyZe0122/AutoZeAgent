// Package executor provides the low-level process runner used exclusively by
// Tool Broker handlers.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	MaxOutputBytes int
	AllowedEnv     []string
	UID            *uint32
	GID            *uint32
}

type Runner struct {
	maxOutput  int
	allowedEnv map[string]struct{}
	uid        *uint32
	gid        *uint32
}

func NewRunner(config Config) (*Runner, error) {
	if config.MaxOutputBytes <= 0 {
		config.MaxOutputBytes = 1024 * 1024
	}
	if len(config.AllowedEnv) == 0 {
		config.AllowedEnv = []string{"PATH", "HOME", "USER", "TMP", "TEMP", "TMPDIR", "USERPROFILE", "SYSTEMROOT", "COMSPEC", "PATHEXT"}
	}
	allowed := make(map[string]struct{}, len(config.AllowedEnv))
	for _, name := range config.AllowedEnv {
		name = strings.ToUpper(strings.TrimSpace(name))
		if name == "" || strings.Contains(name, "=") {
			return nil, fmt.Errorf("invalid environment allowlist entry %q", name)
		}
		allowed[name] = struct{}{}
	}
	return &Runner{maxOutput: config.MaxOutputBytes, allowedEnv: allowed, uid: config.UID, gid: config.GID}, nil
}

type Request struct {
	Command     string
	Arguments   []string
	Directory   string
	Environment map[string]string
}

type Result struct {
	Command         string    `json:"command"`
	Arguments       []string  `json:"arguments"`
	Directory       string    `json:"directory"`
	ExitCode        int       `json:"exit_code"`
	Stdout          string    `json:"stdout"`
	Stderr          string    `json:"stderr"`
	StdoutTruncated bool      `json:"stdout_truncated"`
	StderrTruncated bool      `json:"stderr_truncated"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

func (r *Runner) Run(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("process context is required")
	}
	request.Command = strings.TrimSpace(request.Command)
	if request.Command == "" {
		return Result{}, errors.New("process command is required")
	}
	if strings.TrimSpace(request.Directory) == "" || !filepath.IsAbs(request.Directory) {
		return Result{}, errors.New("process working directory must be absolute")
	}
	directory, err := filepath.Abs(request.Directory)
	if err != nil {
		return Result{}, fmt.Errorf("resolve process working directory: %w", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return Result{}, fmt.Errorf("stat process working directory: %w", err)
	}
	if !info.IsDir() {
		return Result{}, errors.New("process working directory is not a directory")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	command := exec.Command(request.Command, request.Arguments...)
	command.Dir = directory
	environment, err := r.environment(request.Environment)
	if err != nil {
		return Result{}, err
	}
	command.Env = environment
	group, err := prepareProcess(command, processPolicy{UID: r.uid, GID: r.gid})
	if err != nil {
		return Result{}, fmt.Errorf("prepare process isolation: %w", err)
	}
	defer group.Close()
	stdout := &limitedBuffer{limit: r.maxOutput}
	stderr := &limitedBuffer{limit: r.maxOutput}
	command.Stdout = stdout
	command.Stderr = stderr
	result := Result{Command: request.Command, Arguments: append([]string(nil), request.Arguments...), Directory: directory, ExitCode: -1, StartedAt: time.Now().UTC()}
	if err := command.Start(); err != nil {
		result.FinishedAt = time.Now().UTC()
		return result, fmt.Errorf("start process: %w", err)
	}
	if err := group.Started(command); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		result.FinishedAt = time.Now().UTC()
		return result, fmt.Errorf("attach process isolation: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		_ = group.Terminate(command)
		waitErr = <-wait
		result.FinishedAt = time.Now().UTC()
		result.Stdout, result.StdoutTruncated = stdout.String()
		result.Stderr, result.StderrTruncated = stderr.String()
		return result, ctx.Err()
	}
	result.FinishedAt = time.Now().UTC()
	result.Stdout, result.StdoutTruncated = stdout.String()
	result.Stderr, result.StderrTruncated = stderr.String()
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if waitErr != nil {
		return result, fmt.Errorf("process exited with code %d: %w", result.ExitCode, waitErr)
	}
	return result, nil
}

func (r *Runner) environment(overrides map[string]string) ([]string, error) {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		if _, allowed := r.allowedEnv[upper]; allowed {
			values[upper] = value
		}
	}
	for name, value := range overrides {
		upper := strings.ToUpper(strings.TrimSpace(name))
		if _, allowed := r.allowedEnv[upper]; !allowed {
			return nil, fmt.Errorf("environment variable %s is not allowed", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("environment variable %s contains NUL", name)
		}
		values[upper] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result, nil
}

type limitedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(content)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(content) > remaining {
		content = content[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(content)
	return original, nil
}

func (b *limitedBuffer) String() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String(), b.truncated
}

type processPolicy struct {
	UID *uint32
	GID *uint32
}
