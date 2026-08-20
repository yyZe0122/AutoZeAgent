package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/policy"
	"github.com/yyZe0122/yunmengze-agent/internal/tools/internal/executor"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

type gitTool struct {
	name   string
	guard  *PathGuard
	runner *executor.Runner
}

func newGitTools(guard *PathGuard, runner *executor.Runner) ([]Tool, error) {
	if runner == nil {
		return nil, errors.New("git process runner is required")
	}
	if guard == nil {
		return nil, errors.New("path guard is required")
	}
	names := []string{"git_status", "git_diff", "git_add", "git_commit"}
	result := make([]Tool, 0, len(names))
	for _, name := range names {
		result = append(result, &gitTool{name: name, guard: guard, runner: runner})
	}
	return result, nil
}

func (t *gitTool) Definition() toolapi.Definition {
	return toolapi.Definition{
		Name: t.name, Description: gitDescription(t.name), Risk: string(gitRisk(t.name)), DefaultTimeoutMillis: 30000,
		InputSchema: json.RawMessage(gitSchema(t.name)),
	}
}

func (t *gitTool) Authorization(raw json.RawMessage) (Authorization, error) {
	repository, arguments, err := t.parse(raw)
	if err != nil {
		return Authorization{}, err
	}
	return Authorization{Capability: t.name, Path: repository, Command: "git", Arguments: arguments}, nil
}

func (t *gitTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	repository, arguments, err := t.parse(raw)
	if err != nil {
		return nil, err
	}
	result, runErr := t.runner.Run(ctx, executor.Request{
		Command: "git", Arguments: arguments, Directory: repository, CallID: toolCallIDFromContext(ctx),
	})
	return encodeProcessResult(result, runErr)
}

func (t *gitTool) parse(raw json.RawMessage) (string, []string, error) {
	var common struct {
		Repository string   `json:"repository"`
		Paths      []string `json:"paths,omitempty"`
		Staged     bool     `json:"staged,omitempty"`
		Message    string   `json:"message,omitempty"`
	}
	if err := decodeStrict(raw, &common); err != nil {
		return "", nil, err
	}
	repository, err := t.guard.Resolve(common.Repository)
	if err != nil {
		return "", nil, err
	}
	if err := validateGitPaths(common.Paths); err != nil {
		return "", nil, err
	}
	arguments := []string{"-C", repository}
	switch t.name {
	case "git_status":
		arguments = append(arguments, "status", "--porcelain=v1")
	case "git_diff":
		arguments = append(arguments, "diff", "--no-ext-diff")
		if common.Staged {
			arguments = append(arguments, "--cached")
		}
		if len(common.Paths) > 0 {
			arguments = append(arguments, "--")
			arguments = append(arguments, common.Paths...)
		}
	case "git_add":
		if len(common.Paths) == 0 {
			return "", nil, errors.New("git.add requires at least one relative path")
		}
		arguments = append(arguments, "add", "--")
		arguments = append(arguments, common.Paths...)
	case "git_commit":
		message := strings.TrimSpace(common.Message)
		if message == "" {
			return "", nil, errors.New("git.commit message is required")
		}
		arguments = append(arguments, "commit", "-m", message)
	default:
		return "", nil, ErrUnknownTool
	}
	return repository, arguments, nil
}

func validateGitPaths(paths []string) error {
	for _, value := range paths {
		value = strings.TrimSpace(value)
		if value == "" || filepath.IsAbs(value) {
			return errors.New("git paths must be non-empty relative paths")
		}
		clean := filepath.Clean(value)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("git path escapes repository")
		}
	}
	return nil
}

func gitRisk(name string) policy.RiskLevel {
	switch name {
	case "git_status", "git_diff":
		return policy.RiskR0
	case "git_add":
		return policy.RiskR1
	default:
		return policy.RiskR2
	}
}
func gitDescription(name string) string {
	return map[string]string{
		"git_status": "Read repository working tree status.",
		"git_diff":   "Read repository differences.",
		"git_add":    "Stage approved repository paths.",
		"git_commit": "Create a local commit with an approved message.",
	}[name]
}

func gitSchema(name string) string {
	base := map[string]string{
		"git_status": `{"type":"object","additionalProperties":false,"required":["repository"],"properties":{"repository":{"type":"string"}}}`,
		"git_diff":   `{"type":"object","additionalProperties":false,"required":["repository"],"properties":{"repository":{"type":"string"},"paths":{"type":"array","items":{"type":"string"}},"staged":{"type":"boolean"}}}`,
		"git_add":    `{"type":"object","additionalProperties":false,"required":["repository","paths"],"properties":{"repository":{"type":"string"},"paths":{"type":"array","minItems":1,"items":{"type":"string"}}}}`,
		"git_commit": `{"type":"object","additionalProperties":false,"required":["repository","message"],"properties":{"repository":{"type":"string"},"message":{"type":"string"}}}`,
	}
	return base[name]
}
