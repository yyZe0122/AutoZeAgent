// Package skillcatalog discovers file-based skills and reads their content on demand.
package skillcatalog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yyZe0122/yunmengze-agent/internal/injectscan"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/pathsecurity"
)

const (
	SkillFileName = "SKILL.md"
	DraftFileName = "SKILL.md.draft"
)

var (
	ErrSkillNotFound   = errors.New("skill not found")
	ErrInvalidSkill    = errors.New("invalid skill")
	ErrContextTooLarge = errors.New("skill context exceeds byte budget")
	ErrSystemSkill     = errors.New("system skills cannot be modified")
	ErrNoDraft         = errors.New("skill draft not found")
)

type Source string

const (
	SourceSystem  Source = "system"
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

// Root is a skill directory and its label. Roots are evaluated from low to
// high priority; a later root replaces an earlier skill with the same ID.
type Root struct {
	Path   string
	Source Source
}

type Skill struct {
	ID          string
	Name        string
	Description string
	FilePath    string
	Source      Source
	HasDraft    bool
	Triggers    []string
	rootPath    string
}

type Diagnostic struct {
	Path string
	Err  error
}

func (d Diagnostic) Error() string {
	if d.Path == "" {
		return d.Err.Error()
	}
	return d.Path + ": " + d.Err.Error()
}

type Catalog struct {
	mu    sync.RWMutex
	roots []Root
	byID  map[string]Skill
	order []string
}

// Discover scans <root>/<skill-id>/SKILL.md entries. Discovery reads only the
// frontmatter needed for the catalog; skill bodies are loaded by Read.
func Discover(roots []Root) (*Catalog, []Diagnostic) {
	catalog := &Catalog{roots: append([]Root(nil), roots...), byID: make(map[string]Skill)}
	var diagnostics []Diagnostic

	for _, configured := range roots {
		rootPath := strings.TrimSpace(configured.Path)
		if rootPath == "" {
			diagnostics = append(diagnostics, Diagnostic{Err: errors.New("skill root path is required")})
			continue
		}
		absolute, err := filepath.Abs(rootPath)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: rootPath, Err: err})
			continue
		}
		resolvedRoot, err := pathsecurity.ResolveExisting(absolute)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: absolute, Err: err})
			continue
		}
		entries, err := os.ReadDir(resolvedRoot)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: resolvedRoot, Err: err})
			continue
		}

		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				diagnostics = append(diagnostics, Diagnostic{Path: filepath.Join(resolvedRoot, entry.Name()), Err: errors.New("skill directory symlink is not allowed")})
				continue
			}
			if !entry.IsDir() {
				continue
			}
			filePath := filepath.Join(resolvedRoot, entry.Name(), SkillFileName)
			info, err := os.Lstat(filePath)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: filePath, Err: err})
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				diagnostics = append(diagnostics, Diagnostic{Path: filePath, Err: errors.New("skill file symlink is not allowed")})
				continue
			}
			if !info.Mode().IsRegular() || !pathsecurity.ContainsResolved(resolvedRoot, filePath) {
				diagnostics = append(diagnostics, Diagnostic{Path: filePath, Err: errors.New("skill file is outside its root or is not regular")})
				continue
			}
			metadata, err := readMetadata(filePath)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: filePath, Err: err})
				continue
			}
			id := strings.TrimSpace(entry.Name())
			if id == "" || id == "." || id == ".." {
				diagnostics = append(diagnostics, Diagnostic{Path: filePath, Err: fmt.Errorf("%w: invalid directory ID", ErrInvalidSkill)})
				continue
			}
			draftPath := filepath.Join(resolvedRoot, id, DraftFileName)
			hasDraft := false
			if info, err := os.Lstat(draftPath); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				hasDraft = true
			}
			catalog.byID[id] = Skill{
				ID:          id,
				Name:        metadata.name,
				Description: metadata.description,
				FilePath:    filePath,
				Source:      configured.Source,
				HasDraft:    hasDraft,
				Triggers:    metadata.triggers,
				rootPath:    resolvedRoot,
			}
		}
	}

	catalog.order = make([]string, 0, len(catalog.byID))
	for id := range catalog.byID {
		catalog.order = append(catalog.order, id)
	}
	sort.Strings(catalog.order)
	return catalog, diagnostics
}

func (c *Catalog) Skills() []Skill {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	skills := make([]Skill, 0, len(c.order))
	for _, id := range c.order {
		skills = append(skills, c.byID[id])
	}
	return skills
}

// Get returns a discovered skill by ID.
func (c *Catalog) Get(id string) (Skill, bool) {
	if c == nil {
		return Skill{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	sk, ok := c.byID[strings.TrimSpace(id)]
	return sk, ok
}

// Reload re-discovers from the original roots (after apply/reject).
func (c *Catalog) Reload() []Diagnostic {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	roots := append([]Root(nil), c.roots...)
	c.mu.Unlock()
	fresh, diagnostics := Discover(roots)
	c.mu.Lock()
	defer c.mu.Unlock()
	if fresh != nil {
		c.byID = fresh.byID
		c.order = fresh.order
		c.roots = fresh.roots
	}
	return diagnostics
}

// Select resolves IDs in caller-provided order and rejects duplicates.
func (c *Catalog) Select(ids []string) ([]Skill, error) {
	if c == nil {
		return nil, ErrSkillNotFound
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	selected := make([]Skill, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, fmt.Errorf("%w: empty ID", ErrInvalidSkill)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("%w: duplicate ID %q", ErrInvalidSkill, id)
		}
		skill, ok := c.byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, id)
		}
		seen[id] = struct{}{}
		selected = append(selected, skill)
	}
	return selected, nil
}

// Read loads the current body for a discovered skill and rechecks containment.
func (c *Catalog) Read(id string) ([]byte, error) {
	if c == nil {
		return nil, ErrSkillNotFound
	}
	c.mu.RLock()
	skill, ok := c.byID[strings.TrimSpace(id)]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, strings.TrimSpace(id))
	}
	return readSkillBody(skill)
}

func readSkillBody(skill Skill) ([]byte, error) {
	info, err := os.Lstat(skill.FilePath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !pathsecurity.ContainsResolved(skill.rootPath, skill.FilePath) {
		return nil, fmt.Errorf("%w: skill path is no longer safe", ErrInvalidSkill)
	}
	data, err := os.ReadFile(skill.FilePath)
	if err != nil {
		return nil, err
	}
	_, body, err := splitSkillFile(data)
	if err != nil {
		return nil, err
	}
	if err := injectscan.Scan(body); err != nil {
		return nil, fmt.Errorf("%w: skill %q body: %v", ErrInvalidSkill, skill.ID, err)
	}
	return []byte(body), nil
}

const maxLinkedFiles = 32

// ListLinked returns relative paths under the skill directory (not SKILL.md / drafts / backups).
// Symlinks are skipped. Results are sorted and capped.
func (c *Catalog) ListLinked(id string) ([]string, error) {
	skill, err := c.lookup(id)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(skill.FilePath)
	var out []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skipLinkedName(rel) {
			return nil
		}
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		if !pathsecurity.ContainsResolved(skill.rootPath, path) || !pathsecurity.ContainsResolved(dir, path) {
			return nil
		}
		out = append(out, rel)
		if len(out) >= maxLinkedFiles {
			return errLinkedCap
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLinkedCap) {
		return nil, err
	}
	sort.Strings(out)
	if len(out) > maxLinkedFiles {
		out = out[:maxLinkedFiles]
	}
	return out, nil
}

var errLinkedCap = errors.New("linked file cap")

// ReadLinked loads a regular file under the skill directory. Rejects traversal, symlinks, and SKILL.md itself.
func (c *Catalog) ReadLinked(id, rel string) ([]byte, error) {
	skill, err := c.lookup(id)
	if err != nil {
		return nil, err
	}
	rel = strings.TrimSpace(rel)
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return nil, fmt.Errorf("%w: linked path is required and must be relative", ErrInvalidSkill)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: linked path escapes skill directory", ErrInvalidSkill)
	}
	slash := filepath.ToSlash(clean)
	if skipLinkedName(slash) {
		return nil, fmt.Errorf("%w: use skill_view without file_path to load SKILL.md", ErrInvalidSkill)
	}
	dir := filepath.Dir(skill.FilePath)
	target := filepath.Join(dir, filepath.FromSlash(slash))
	info, err := os.Lstat(target)
	if err != nil {
		return nil, fmt.Errorf("%w: linked file %q: %v", ErrSkillNotFound, slash, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: linked path is not a regular file", ErrInvalidSkill)
	}
	if !pathsecurity.ContainsResolved(skill.rootPath, target) || !pathsecurity.ContainsResolved(dir, target) {
		return nil, fmt.Errorf("%w: linked path is outside the skill directory", ErrInvalidSkill)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	if err := injectscan.Scan(string(data)); err != nil {
		return nil, fmt.Errorf("%w: skill %q file %q: %v", ErrInvalidSkill, skill.ID, slash, err)
	}
	return data, nil
}

func (c *Catalog) lookup(id string) (Skill, error) {
	if c == nil {
		return Skill{}, ErrSkillNotFound
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	skill, ok := c.byID[strings.TrimSpace(id)]
	if !ok {
		return Skill{}, fmt.Errorf("%w: %s", ErrSkillNotFound, strings.TrimSpace(id))
	}
	return skill, nil
}

func skipLinkedName(rel string) bool {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	switch base {
	case SkillFileName, DraftFileName:
		return true
	}
	return strings.HasPrefix(base, SkillFileName+".bak.")
}

// Context renders selected skills without truncation. Callers must pass Skill
// values returned by this catalog so a forged or stale selection fails closed.
func (c *Catalog) Context(selected []Skill, maxBytes int) (string, error) {
	if maxBytes < 0 {
		return "", errors.New("skill context byte budget cannot be negative")
	}
	var builder strings.Builder
	for _, chosen := range selected {
		if c == nil {
			return "", fmt.Errorf("%w: stale or forged selection %q", ErrInvalidSkill, chosen.ID)
		}
		c.mu.RLock()
		current, ok := c.byID[chosen.ID]
		c.mu.RUnlock()
		if !ok || current.ID != chosen.ID || current.FilePath != chosen.FilePath || current.rootPath != chosen.rootPath {
			return "", fmt.Errorf("%w: stale or forged selection %q", ErrInvalidSkill, chosen.ID)
		}
		body, err := readSkillBody(current)
		if err != nil {
			return "", err
		}
		section := fmt.Sprintf("<skill id=%q name=%q description=%q source=%q>\n%s\n</skill>\n",
			html.EscapeString(chosen.ID),
			html.EscapeString(chosen.Name),
			html.EscapeString(chosen.Description),
			html.EscapeString(string(chosen.Source)),
			html.EscapeString(string(body)),
		)
		if builder.Len()+len(section) > maxBytes {
			return "", ErrContextTooLarge
		}
		builder.WriteString(section)
	}
	return builder.String(), nil
}

type metadata struct {
	name        string
	description string
	triggers    []string
}

func readMetadata(filePath string) (metadata, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return metadata{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return metadata{}, fmt.Errorf("%w: missing frontmatter", ErrInvalidSkill)
	}
	values := make(map[string]string)
	closed := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			closed = true
			break
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, found := strings.Cut(line, ":")
		if !found {
			return metadata{}, fmt.Errorf("%w: invalid frontmatter line %q", ErrInvalidSkill, line)
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "description" && key != "triggers" {
			continue
		}
		value, err := parseScalar(strings.TrimSpace(raw))
		if err != nil {
			return metadata{}, fmt.Errorf("%w: %s: %v", ErrInvalidSkill, key, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return metadata{}, err
	}
	if !closed {
		return metadata{}, fmt.Errorf("%w: unterminated frontmatter", ErrInvalidSkill)
	}
	name := strings.TrimSpace(values["name"])
	description := strings.TrimSpace(values["description"])
	if name == "" || description == "" {
		return metadata{}, fmt.Errorf("%w: name and description are required", ErrInvalidSkill)
	}
	return metadata{name: name, description: description, triggers: splitTriggers(values["triggers"])}, nil
}

func parseScalar(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "`") {
		return strconv.Unquote(value)
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", errors.New("unterminated quoted value")
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), nil
	}
	return value, nil
}

func splitSkillFile(data []byte) (metadata, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return metadata{}, "", fmt.Errorf("%w: missing frontmatter", ErrInvalidSkill)
	}
	closing := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			closing = index
			break
		}
	}
	if closing < 0 {
		return metadata{}, "", fmt.Errorf("%w: unterminated frontmatter", ErrInvalidSkill)
	}
	return metadata{}, strings.Join(lines[closing+1:], "\n"), nil
}

// DraftPath is <root>/<id>/SKILL.md.draft for a discovered skill.
func (s Skill) DraftPath() string {
	if strings.TrimSpace(s.FilePath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.FilePath), DraftFileName)
}

// WriteDraft writes a full SKILL.md document to SKILL.md.draft after injectscan.
func (c *Catalog) WriteDraft(id, body string) (Skill, string, error) {
	skill, err := c.mutableSkill(id)
	if err != nil {
		return Skill{}, "", err
	}
	normalized, hash, err := validateSkillDocument(body)
	if err != nil {
		return Skill{}, "", err
	}
	draftPath := skill.DraftPath()
	if !pathsecurity.ContainsResolved(skill.rootPath, draftPath) {
		return Skill{}, "", fmt.Errorf("%w: draft path is outside skill root", ErrInvalidSkill)
	}
	if err := os.WriteFile(draftPath, []byte(normalized), 0o600); err != nil {
		return Skill{}, "", err
	}
	c.markDraft(skill.ID, true)
	return skill, hash, nil
}

// DiscardDraft removes SKILL.md.draft if present.
func (c *Catalog) DiscardDraft(id string) (Skill, error) {
	skill, err := c.mutableSkill(id)
	if err != nil {
		return Skill{}, err
	}
	draftPath := skill.DraftPath()
	if err := os.Remove(draftPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Skill{}, err
	}
	c.markDraft(skill.ID, false)
	return skill, nil
}

// ApplyDraft replaces SKILL.md with the draft after backup. Returns backup path and hash.
func (c *Catalog) ApplyDraft(id string, now time.Time) (Skill, string, string, error) {
	skill, err := c.mutableSkill(id)
	if err != nil {
		return Skill{}, "", "", err
	}
	draftPath := skill.DraftPath()
	data, err := os.ReadFile(draftPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Skill{}, "", "", ErrNoDraft
		}
		return Skill{}, "", "", err
	}
	normalized, hash, err := validateSkillDocument(string(data))
	if err != nil {
		return Skill{}, "", "", err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	backup := skill.FilePath + ".bak." + now.UTC().Format("20060102T150405.000000000Z")
	if !pathsecurity.ContainsResolved(skill.rootPath, backup) {
		return Skill{}, "", "", fmt.Errorf("%w: backup path is outside skill root", ErrInvalidSkill)
	}
	current, err := os.ReadFile(skill.FilePath)
	if err != nil {
		return Skill{}, "", "", err
	}
	if err := os.WriteFile(backup, current, 0o600); err != nil {
		return Skill{}, "", "", err
	}
	tmp := skill.FilePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(normalized), 0o600); err != nil {
		return Skill{}, "", "", err
	}
	if err := os.Rename(tmp, skill.FilePath); err != nil {
		_ = os.Remove(tmp)
		return Skill{}, "", "", err
	}
	_ = os.Remove(draftPath)
	c.Reload()
	updated, ok := c.Get(id)
	if !ok {
		updated = skill
	}
	return updated, backup, hash, nil
}

func (c *Catalog) mutableSkill(id string) (Skill, error) {
	skill, ok := c.Get(id)
	if !ok {
		return Skill{}, fmt.Errorf("%w: %s", ErrSkillNotFound, strings.TrimSpace(id))
	}
	if skill.Source == SourceSystem {
		return Skill{}, fmt.Errorf("%w: %s", ErrSystemSkill, skill.ID)
	}
	return skill, nil
}

func (c *Catalog) markDraft(id string, has bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if sk, ok := c.byID[id]; ok {
		sk.HasDraft = has
		c.byID[id] = sk
	}
}

func validateSkillDocument(raw string) (string, string, error) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return "", "", fmt.Errorf("%w: empty document", ErrInvalidSkill)
	}
	if err := injectscan.Scan(text); err != nil {
		return "", "", err
	}
	meta, body, err := splitSkillFile([]byte(text))
	if err != nil {
		return "", "", err
	}
	_ = meta
	if err := injectscan.Scan(body); err != nil {
		return "", "", err
	}
	// Re-parse required frontmatter via a temp file-less path: reuse readMetadata rules.
	if _, err := parseFrontmatterMap(text); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(text))
	return text, hex.EncodeToString(sum[:]), nil
}

func parseFrontmatterMap(text string) (metadata, error) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return metadata{}, fmt.Errorf("%w: missing frontmatter", ErrInvalidSkill)
	}
	values := make(map[string]string)
	closed := false
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			closed = true
			break
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, found := strings.Cut(line, ":")
		if !found {
			return metadata{}, fmt.Errorf("%w: invalid frontmatter line %q", ErrInvalidSkill, line)
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "description" && key != "triggers" {
			continue
		}
		value, err := parseScalar(strings.TrimSpace(raw))
		if err != nil {
			return metadata{}, fmt.Errorf("%w: %s: %v", ErrInvalidSkill, key, err)
		}
		values[key] = value
	}
	if !closed {
		return metadata{}, fmt.Errorf("%w: unterminated frontmatter", ErrInvalidSkill)
	}
	name := strings.TrimSpace(values["name"])
	description := strings.TrimSpace(values["description"])
	if name == "" || description == "" {
		return metadata{}, fmt.Errorf("%w: name and description are required", ErrInvalidSkill)
	}
	return metadata{name: name, description: description, triggers: splitTriggers(values["triggers"])}, nil
}

const MaxAutoSkills = 3

const (
	matchNone    = 0
	matchDesc    = 1
	matchIDName  = 2
	matchTrigger = 3
)

// Match returns skills whose id, name, triggers, or description tokens appear in query.
// Deterministic; no LLM. Ranked trigger > id/name > description, then id; capped at limit (default 3).
func (c *Catalog) Match(query string, limit int) []Skill {
	if c == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = MaxAutoSkills
	}
	hay := strings.ToLower(query)
	c.mu.RLock()
	defer c.mu.RUnlock()
	type hit struct {
		sk    Skill
		score int
	}
	var hits []hit
	for _, id := range c.order {
		sk := c.byID[id]
		if score := skillMatchScore(sk, hay); score > matchNone {
			hits = append(hits, hit{sk: sk, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].sk.ID < hits[j].sk.ID
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Skill, len(hits))
	for i, h := range hits {
		out[i] = h.sk
	}
	return out
}

func skillMatchScore(sk Skill, hayLower string) int {
	if hayLower == "" {
		return matchNone
	}
	best := matchNone
	for _, t := range sk.Triggers {
		if containsToken(hayLower, strings.ToLower(t)) {
			return matchTrigger
		}
	}
	if containsToken(hayLower, strings.ToLower(sk.ID)) || containsToken(hayLower, strings.ToLower(sk.Name)) {
		best = matchIDName
	}
	for _, tok := range weakDescriptionTokens(sk.Description) {
		if containsToken(hayLower, tok) && best < matchDesc {
			best = matchDesc
		}
	}
	return best
}

func containsToken(hay, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	if hasCJK(needle) {
		return strings.Contains(hay, needle)
	}
	start := 0
	for {
		i := strings.Index(hay[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isWordChar(lastRuneBefore(hay, i))
		after := i + len(needle)
		afterOK := after >= len(hay) || !isWordChar(firstRuneAt(hay, after))
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
		if start >= len(hay) {
			return false
		}
	}
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x2E80 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0x20000 && r <= 0x2FA1F {
			return true
		}
	}
	return false
}

func isWordChar(r rune) bool {
	if r == '_' {
		return true
	}
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func lastRuneBefore(s string, i int) rune {
	if i <= 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func firstRuneAt(s string, i int) rune {
	if i >= len(s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

func weakDescriptionTokens(desc string) []string {
	var out []string
	for _, raw := range strings.FieldsFunc(strings.ToLower(desc), func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == '|' || r == ' ' || r == '\t' || r == '\n'
	}) {
		tok := strings.Trim(raw, ".:;!?()[]{}\"'")
		if len([]rune(tok)) < 4 {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func splitTriggers(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	seen := make(map[string]struct{}, len(parts))
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}
