package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/pkg/toolapi"
)

const defaultFileReadLimit = 16 * 1024 * 1024

type fileTool struct {
	name  string
	guard *PathGuard
}

func newFileTools(roots []string) ([]Tool, error) {
	guard, err := NewPathGuard(roots)
	if err != nil {
		return nil, err
	}
	names := []string{"fs_read", "fs_list", "fs_stat", "fs_write", "fs_patch", "fs_mkdir"}
	result := make([]Tool, 0, len(names))
	for _, name := range names {
		result = append(result, &fileTool{name: name, guard: guard})
	}
	return result, nil
}

func (t *fileTool) Definition() toolapi.Definition {
	risk := policy.RiskR0
	if t.name == "fs_write" || t.name == "fs_patch" || t.name == "fs_mkdir" {
		risk = policy.RiskR1
	}
	return toolapi.Definition{
		Name: t.name, Description: fileDescription(t.name), InputSchema: json.RawMessage(fileSchema(t.name)),
		Risk: string(risk), DefaultTimeoutMillis: 5000,
	}
}

func (t *fileTool) Authorization(raw json.RawMessage) (Authorization, error) {
	path, err := t.authorizationPath(raw)
	if err != nil {
		return Authorization{}, err
	}
	resolved, err := t.guard.Resolve(path)
	if err != nil {
		return Authorization{}, err
	}
	return Authorization{Capability: t.name, Path: resolved}, nil
}

func (t *fileTool) authorizationPath(raw json.RawMessage) (string, error) {
	var path string
	switch t.name {
	case "fs_read":
		var input readInput
		if err := decodeStrict(raw, &input); err != nil {
			return "", err
		}
		path = input.Path
	case "fs_write":
		var input writeInput
		if err := decodeStrict(raw, &input); err != nil {
			return "", err
		}
		path = input.Path
	case "fs_patch":
		var input patchInput
		if err := decodeStrict(raw, &input); err != nil {
			return "", err
		}
		path = input.Path
	case "fs_list", "fs_stat", "fs_mkdir":
		var input struct {
			Path string `json:"path"`
		}
		if err := decodeStrict(raw, &input); err != nil {
			return "", err
		}
		path = input.Path
	default:
		return "", ErrUnknownTool
	}
	return path, nil
}

func (t *fileTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch t.name {
	case "fs_read":
		return t.read(ctx, raw)
	case "fs_list":
		return t.list(ctx, raw)
	case "fs_stat":
		return t.stat(ctx, raw)
	case "fs_write":
		return t.write(ctx, raw)
	case "fs_patch":
		return t.patch(ctx, raw)
	case "fs_mkdir":
		return t.mkdir(ctx, raw)
	default:
		return nil, ErrUnknownTool
	}
}

type readInput struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

func (t *fileTool) read(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input readInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	path, err := t.guard.Resolve(input.Path)
	if err != nil {
		return nil, err
	}
	limit := input.MaxBytes
	if limit <= 0 {
		limit = defaultFileReadLimit
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	readLimit := limit
	if limit <= (1<<63-1)-int64(utf8.UTFMax) {
		readLimit += int64(utf8.UTFMax)
	}
	content, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	truncated := int64(len(content)) > limit
	content, err = textPrefix(content, limit)
	if err != nil {
		return nil, err
	}
	return encodeResult(map[string]any{"path": path, "content": string(content), "size_bytes": len(content), "truncated": truncated})
}

func textPrefix(content []byte, limit int64) ([]byte, error) {
	end := len(content)
	if limit < int64(end) {
		end = int(limit)
	}
	position := 0
	for position < end {
		if content[position] == 0 {
			return nil, errors.New("file contains binary data")
		}
		r, size := utf8.DecodeRune(content[position:])
		if r == utf8.RuneError && size == 1 {
			return nil, errors.New("file content is not valid UTF-8 text")
		}
		if position+size > end {
			break
		}
		position += size
	}
	return content[:position], nil
}

func validateTextContent(content []byte) error {
	_, err := textPrefix(content, int64(len(content)))
	return err
}
func (t *fileTool) list(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
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
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	result := make([]entry, 0, len(entries))
	for _, item := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind := "file"
		if item.IsDir() {
			kind = "directory"
		} else if item.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		result = append(result, entry{Name: item.Name(), Type: kind})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return encodeResult(map[string]any{"path": path, "entries": result})
}

func (t *fileTool) stat(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
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
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return encodeResult(map[string]any{
		"path": path, "name": info.Name(), "size_bytes": info.Size(), "mode": info.Mode().String(),
		"is_directory": info.IsDir(), "modified_at": info.ModTime().UTC(),
	})
}

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
	temporary, err := os.CreateTemp(directory, ".autozeagent-*.tmp")
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

func fileDescription(name string) string {
	const pathHint = " Prefer absolute paths under configured roots; relative paths resolve against the workspace root."
	return map[string]string{
		"fs_read":  "Read a file inside configured filesystem roots." + pathHint,
		"fs_list":  "List a directory inside configured filesystem roots." + pathHint,
		"fs_stat":  "Read file metadata inside configured filesystem roots." + pathHint,
		"fs_write": "Atomically write a file inside configured filesystem roots." + pathHint,
		"fs_patch": "Replace an exact text occurrence in a file." + pathHint,
		"fs_mkdir": "Create a directory inside configured filesystem roots." + pathHint,
	}[name]
}

func fileSchema(name string) string {
	pathProp := `{"type":"string","description":"Prefer absolute path under configured roots; relative paths resolve against the workspace root."}`
	schemas := map[string]string{
		"fs_read":  `{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":` + pathProp + `,"max_bytes":{"type":"integer","minimum":1}}}`,
		"fs_list":  `{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":` + pathProp + `}}`,
		"fs_stat":  `{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":` + pathProp + `}}`,
		"fs_write": `{"type":"object","additionalProperties":false,"required":["path","content"],"properties":{"path":` + pathProp + `,"content":{"type":"string"}}}`,
		"fs_patch": `{"type":"object","additionalProperties":false,"required":["path","old","new"],"properties":{"path":` + pathProp + `,"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}}}`,
		"fs_mkdir": `{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":` + pathProp + `}}`,
	}
	return schemas[name]
}
