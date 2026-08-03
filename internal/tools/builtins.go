package tools

import (
	"database/sql"
	"errors"

	"autozeagent.local/autozeagent/internal/tools/internal/executor"
)

// ExecutorConfig controls the broker-owned process runner. The concrete runner
// remains inside the tools package so planners and optional modules cannot
// import or invoke it directly.
type ExecutorConfig struct {
	MaxOutputBytes int
	AllowedEnv     []string
	UID            *uint32
	GID            *uint32
}

func RegisterBuiltins(broker *Broker, roots []string, config ExecutorConfig) error {
	if broker == nil {
		return errors.New("tool broker is required")
	}
	runner, err := executor.NewRunner(executor.Config{
		MaxOutputBytes: config.MaxOutputBytes,
		AllowedEnv:     config.AllowedEnv,
		UID:            config.UID,
		GID:            config.GID,
	})
	if err != nil {
		return err
	}
	fileTools, err := newFileTools(roots)
	if err != nil {
		return err
	}
	process, err := newProcessTool(roots, runner)
	if err != nil {
		return err
	}
	gitTools, err := newGitTools(roots, runner)
	if err != nil {
		return err
	}
	all := append(fileTools, process)
	all = append(all, gitTools...)
	all = append(all, newHTTPGetTool(16*1024*1024))
	for _, tool := range all {
		if err := broker.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

// RegisterTaskTool registers the ADR-039 task tool. Runner may be set later via SetRunner.
func RegisterTaskTool(broker *Broker, db *sql.DB, runner SubagentRunner) (*taskTool, error) {
	if broker == nil {
		return nil, errors.New("tool broker is required")
	}
	tool, err := NewTaskTool(TaskToolConfig{DB: db, Runner: runner})
	if err != nil {
		return nil, err
	}
	if err := broker.Register(tool); err != nil {
		return nil, err
	}
	return tool, nil
}
