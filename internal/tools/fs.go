package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yyZe0122/yunmengze-agent/internal/policy"
	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

const defaultFileReadLimit = 16 * 1024 * 1024

type fileTool struct {
	name  string
	guard *PathGuard
}

func newFileTools(guard *PathGuard) []Tool {
	names := []string{"fs_read", "fs_list", "fs_stat", "fs_glob", "fs_grep", "fs_write", "fs_patch", "fs_mkdir"}
	result := make([]Tool, 0, len(names))
	for _, name := range names {
		result = append(result, &fileTool{name: name, guard: guard})
	}
	return result
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
	case "fs_glob":
		var input globInput
		if err := decodeStrict(raw, &input); err != nil {
			return "", err
		}
		path = input.Path
		if strings.TrimSpace(path) == "" {
			path = "."
		}
	case "fs_grep":
		var input grepInput
		if err := decodeStrict(raw, &input); err != nil {
			return "", err
		}
		path = input.Path
		if strings.TrimSpace(path) == "" {
			path = "."
		}
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
	case "fs_glob":
		return t.glob(ctx, raw)
	case "fs_grep":
		return t.grep(ctx, raw)
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

const (
	defaultGlobMaxResults = 200
	maxGlobMaxResults     = 1000
	defaultGrepMaxMatches = 50
	maxGrepMaxMatches     = 200
	defaultGrepMaxFiles   = 100
	maxGrepMaxFiles       = 500
	grepMaxFileBytes      = 1 << 20 // 1 MiB per file
	grepMaxPatternLen     = 256
	grepMaxWalkFiles      = 5000
)

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Max     int    `json:"max,omitempty"`
}

func (t *fileTool) glob(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input globInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return nil, errors.New("pattern is required")
	}
	if strings.Contains(pattern, "**") {
		return nil, errors.New("recursive ** patterns are not supported; use a single-level glob or path subtree")
	}
	base := strings.TrimSpace(input.Path)
	if base == "" {
		base = "."
	}
	root, err := t.guard.Resolve(base)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("path must be a directory for fs_glob")
	}
	limit := input.Max
	if limit <= 0 {
		limit = defaultGlobMaxResults
	}
	if limit > maxGlobMaxResults {
		limit = maxGlobMaxResults
	}
	// filepath.Glob is non-recursive; match under root only.
	matches, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	out := make([]string, 0, minInt(len(matches), limit))
	truncated := false
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := t.guard.Resolve(match)
		if err != nil {
			continue
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		out = append(out, resolved)
	}
	return encodeResult(map[string]any{
		"path": root, "pattern": pattern, "matches": out,
		"count": len(out), "truncated": truncated,
	})
}

type grepInput struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path,omitempty"`
	Glob            string `json:"glob,omitempty"`
	MaxMatches      int    `json:"max_matches,omitempty"`
	MaxFiles        int    `json:"max_files,omitempty"`
	Literal         bool   `json:"literal,omitempty"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty"`
}

type grepMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

func (t *fileTool) grep(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input grepInput
	if err := decodeStrict(raw, &input); err != nil {
		return nil, err
	}
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" {
		return nil, errors.New("pattern is required")
	}
	if len(pattern) > grepMaxPatternLen {
		return nil, errors.New("pattern too long")
	}
	base := strings.TrimSpace(input.Path)
	if base == "" {
		base = "."
	}
	root, err := t.guard.Resolve(base)
	if err != nil {
		return nil, err
	}
	maxMatches := input.MaxMatches
	if maxMatches <= 0 {
		maxMatches = defaultGrepMaxMatches
	}
	if maxMatches > maxGrepMaxMatches {
		maxMatches = maxGrepMaxMatches
	}
	maxFiles := input.MaxFiles
	if maxFiles <= 0 {
		maxFiles = defaultGrepMaxFiles
	}
	if maxFiles > maxGrepMaxFiles {
		maxFiles = maxGrepMaxFiles
	}
	matcher, err := compileGrepMatcher(pattern, input.Literal, input.CaseInsensitive)
	if err != nil {
		return nil, err
	}
	fileGlob := strings.TrimSpace(input.Glob)
	if strings.Contains(fileGlob, "**") {
		return nil, errors.New("recursive ** in glob is not supported")
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	var files []string
	if !info.IsDir() {
		files = []string{root}
	} else {
		files, err = t.collectGrepFiles(ctx, root, fileGlob, maxFiles)
		if err != nil {
			return nil, err
		}
	}

	matches := make([]grepMatch, 0, maxMatches)
	filesScanned := 0
	truncated := false
	for _, filePath := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(matches) >= maxMatches {
			truncated = true
			break
		}
		filesScanned++
		fileMatches, fileTrunc, err := grepFile(ctx, filePath, matcher, maxMatches-len(matches))
		if err != nil {
			// Skip unreadable / binary files.
			continue
		}
		matches = append(matches, fileMatches...)
		if fileTrunc {
			truncated = true
		}
	}
	if len(matches) >= maxMatches {
		truncated = true
	}
	return encodeResult(map[string]any{
		"path": root, "pattern": pattern, "matches": matches,
		"match_count": len(matches), "files_scanned": filesScanned,
		"truncated": truncated,
	})
}

type grepMatcher interface {
	MatchString(s string) bool
}

type literalMatcher struct {
	needle string
}

func (m literalMatcher) MatchString(s string) bool {
	return strings.Contains(s, m.needle)
}

func compileGrepMatcher(pattern string, literal, caseInsensitive bool) (grepMatcher, error) {
	if literal || !looksLikeRegex(pattern) {
		needle := pattern
		if caseInsensitive {
			needle = strings.ToLower(needle)
			return caseFoldLiteralMatcher{needle: needle}, nil
		}
		return literalMatcher{needle: needle}, nil
	}
	expr := pattern
	if caseInsensitive {
		expr = "(?i)" + expr
	}
	re, err := compileBoundedRegex(expr)
	if err != nil {
		return nil, err
	}
	return re, nil
}

type caseFoldLiteralMatcher struct {
	needle string
}

func (m caseFoldLiteralMatcher) MatchString(s string) bool {
	return strings.Contains(strings.ToLower(s), m.needle)
}

func looksLikeRegex(pattern string) bool {
	return strings.ContainsAny(pattern, `.*+?[](){}^$|\`)
}

// compileBoundedRegex uses the standard library with a length guard already applied.
func compileBoundedRegex(expr string) (grepMatcher, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return regexpMatcher{re: re}, nil
}

type regexpMatcher struct {
	re *regexp.Regexp
}

func (m regexpMatcher) MatchString(s string) bool {
	return m.re.MatchString(s)
}

func (t *fileTool) collectGrepFiles(ctx context.Context, root, fileGlob string, maxFiles int) ([]string, error) {
	var files []string
	walked := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "bin" || name == "dist" {
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		walked++
		if walked > grepMaxWalkFiles {
			return errGrepWalkLimit
		}
		if fileGlob != "" {
			ok, matchErr := filepath.Match(fileGlob, d.Name())
			if matchErr != nil || !ok {
				return nil
			}
		}
		resolved, resErr := t.guard.Resolve(path)
		if resErr != nil {
			return nil
		}
		files = append(files, resolved)
		if len(files) >= maxFiles {
			return errGrepFileLimit
		}
		return nil
	})
	if err != nil && !errors.Is(err, errGrepFileLimit) && !errors.Is(err, errGrepWalkLimit) {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

var (
	errGrepFileLimit = errors.New("grep file limit")
	errGrepWalkLimit = errors.New("grep walk limit")
)

func grepFile(ctx context.Context, path string, matcher grepMatcher, remaining int) ([]grepMatch, bool, error) {
	if remaining <= 0 {
		return nil, true, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.Size() > grepMaxFileBytes {
		return nil, false, errors.New("file too large")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, grepMaxFileBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(content)) > grepMaxFileBytes {
		return nil, false, errors.New("file too large")
	}
	if err := validateTextContent(content); err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	lines := strings.Split(string(content), "\n")
	out := make([]grepMatch, 0, minInt(remaining, 8))
	truncated := false
	for i, line := range lines {
		if len(out) >= remaining {
			truncated = true
			break
		}
		// Drop trailing \r from CRLF.
		line = strings.TrimSuffix(line, "\r")
		if matcher.MatchString(line) {
			out = append(out, grepMatch{Path: path, Line: i + 1, Content: truncateRunes(line, 240)})
		}
	}
	return out, truncated, nil
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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

func fileDescription(name string) string {
	const pathHint = " Prefer absolute paths under configured roots; relative paths resolve against the workspace root."
	return map[string]string{
		"fs_read":  "Read a file inside configured filesystem roots." + pathHint,
		"fs_list":  "List a directory inside configured filesystem roots." + pathHint,
		"fs_stat":  "Read file metadata inside configured filesystem roots." + pathHint,
		"fs_glob":  "Match files under a directory with a single-level glob (no **). Prefer over shell find." + pathHint,
		"fs_grep":  "Search file contents under a path (literal or simple regex). Prefer over process_exec grep." + pathHint,
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
		"fs_glob":  `{"type":"object","additionalProperties":false,"required":["pattern"],"properties":{"pattern":{"type":"string"},"path":` + pathProp + `,"max":{"type":"integer","minimum":1,"maximum":1000}}}`,
		"fs_grep":  `{"type":"object","additionalProperties":false,"required":["pattern"],"properties":{"pattern":{"type":"string"},"path":` + pathProp + `,"glob":{"type":"string"},"max_matches":{"type":"integer","minimum":1,"maximum":200},"max_files":{"type":"integer","minimum":1,"maximum":500},"literal":{"type":"boolean"},"case_insensitive":{"type":"boolean"}}}`,
		"fs_write": `{"type":"object","additionalProperties":false,"required":["path","content"],"properties":{"path":` + pathProp + `,"content":{"type":"string"}}}`,
		"fs_patch": `{"type":"object","additionalProperties":false,"required":["path","old","new"],"properties":{"path":` + pathProp + `,"old":{"type":"string"},"new":{"type":"string"},"replace_all":{"type":"boolean"}}}`,
		"fs_mkdir": `{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":` + pathProp + `}}`,
	}
	return schemas[name]
}
