package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *fileTool) write(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input writeInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	path, err := t.guard.Resolve(input.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := atomicWrite(ctx, path, []byte(input.Content)); err != nil {
		return nil, err
	}
	return encodeResult(map[string]any{"path": path, "size_bytes": len(input.Content)})
}

type patchInput struct {
	Path       string `json:"path"`
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (t *fileTool) patch(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input patchInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	if input.Old == "" {
		return nil, errors.New("patch old text is required")
	}
	path, err := t.guard.Resolve(input.Path)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := validateTextContent(content); err != nil {
		return nil, err
	}
	found := strings.Count(string(content), input.Old)
	if found == 0 {
		return nil, errors.New("patch old text not found")
	}
	if found > 1 && !input.ReplaceAll {
		return nil, errors.New("patch old text appears multiple times; provide more context or set replace_all to true")
	}
	replacements := 1
	if input.ReplaceAll {
		replacements = found
	}
	updated := strings.Replace(string(content), input.Old, input.New, replacements)
	if err := atomicWrite(ctx, path, []byte(updated)); err != nil {
		return nil, err
	}
	return encodeResult(map[string]any{"path": path, "replacements": replacements, "size_bytes": len(updated)})
}

func (t *fileTool) mkdir(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	path, err := t.guard.Resolve(input.Path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return nil, err
	}
	return encodeResult(map[string]any{"path": path})
}

func atomicWrite(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".yunmengze-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}
