package tools

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"autozeagent.local/autozeagent/internal/platform/pathsecurity"
)

// PathGuard contains tool paths under configured roots (ADR-012 / ADR-046).
// Roots may grow via AddRoot when sessions bind a client workspace.
type PathGuard struct {
	mu       sync.RWMutex
	roots    []string
	allowAll bool
}

func NewPathGuard(roots []string) (*PathGuard, error) {
	return newPathGuard(roots, false)
}

// NewPathGuardAllowAll builds a guard that only requires absolute/resolved paths
// (no root containment). For local single-user chat.workspace.allow_all only.
func NewPathGuardAllowAll() *PathGuard {
	return &PathGuard{allowAll: true}
}

func newPathGuard(roots []string, allowAll bool) (*PathGuard, error) {
	if allowAll {
		return &PathGuard{allowAll: true}, nil
	}
	if len(roots) == 0 {
		return nil, errors.New("at least one tool filesystem root is required")
	}
	g := &PathGuard{}
	for _, root := range roots {
		if err := g.addRootLocked(root); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// AddRoot appends an absolute workspace root after symlink resolve.
// No-op when allow_all. Duplicate roots are ignored.
func (g *PathGuard) AddRoot(root string) error {
	if g == nil {
		return errors.New("path guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.allowAll {
		return nil
	}
	return g.addRootLocked(root)
}

// Roots returns a copy of configured roots (empty when allow_all).
func (g *PathGuard) Roots() []string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.allowAll {
		return nil
	}
	return append([]string(nil), g.roots...)
}

// AllowAll reports unrestricted root mode.
func (g *PathGuard) AllowAll() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.allowAll
}

func (g *PathGuard) addRootLocked(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("filesystem root cannot be empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve filesystem root: %w", err)
	}
	real, err := pathsecurity.ResolveExisting(absolute)
	if err != nil {
		return fmt.Errorf("resolve filesystem root symlinks: %w", err)
	}
	for _, existing := range g.roots {
		if existing == real {
			return nil
		}
	}
	g.roots = append(g.roots, real)
	return nil
}

// Resolve rejects lexical traversal and symlink traversal outside configured
// roots. Callers must still open or rename promptly to minimize TOCTOU windows.
func (g *PathGuard) Resolve(value string) (string, error) {
	if g == nil {
		return "", errors.New("path guard is nil")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("path is required")
	}
	g.mu.RLock()
	allowAll := g.allowAll
	roots := append([]string(nil), g.roots...)
	g.mu.RUnlock()

	if !filepath.IsAbs(value) {
		return g.resolveRelative(value, roots, allowAll)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	real, err := pathsecurity.ResolveExisting(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}
	if allowAll {
		return real, nil
	}
	for _, root := range roots {
		if pathsecurity.Contains(root, real) {
			return real, nil
		}
	}
	return "", fmt.Errorf("path escapes configured filesystem roots: %s", value)
}

func (g *PathGuard) resolveRelative(value string, roots []string, allowAll bool) (string, error) {
	if allowAll {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", fmt.Errorf("resolve relative path: %w", err)
		}
		real, err := pathsecurity.ResolveExisting(absolute)
		if err != nil {
			return "", fmt.Errorf("resolve path symlinks: %w", err)
		}
		return real, nil
	}
	var lastErr error
	for _, root := range roots {
		absolute, err := filepath.Abs(filepath.Join(root, value))
		if err != nil {
			lastErr = err
			continue
		}
		real, err := pathsecurity.ResolveExisting(absolute)
		if err != nil {
			lastErr = err
			continue
		}
		for _, r := range roots {
			if pathsecurity.Contains(r, real) {
				return real, nil
			}
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("resolve relative path: %w", lastErr)
	}
	return "", fmt.Errorf("path escapes configured filesystem roots: %s", value)
}
