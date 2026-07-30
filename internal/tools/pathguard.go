package tools

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"autozeagent.local/autozeagent/internal/platform/pathsecurity"
)

type PathGuard struct {
	roots []string
}

func NewPathGuard(roots []string) (*PathGuard, error) {
	if len(roots) == 0 {
		return nil, errors.New("at least one tool filesystem root is required")
	}
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			return nil, errors.New("filesystem root cannot be empty")
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve filesystem root: %w", err)
		}
		real, err := pathsecurity.ResolveExisting(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve filesystem root symlinks: %w", err)
		}
		resolved = append(resolved, real)
	}
	return &PathGuard{roots: resolved}, nil
}

// Resolve rejects lexical traversal and symlink traversal outside configured
// roots. Callers must still open or rename promptly to minimize TOCTOU windows.
func (g *PathGuard) Resolve(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(value) {
		return g.resolveRelative(value)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	real, err := pathsecurity.ResolveExisting(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}
	for _, root := range g.roots {
		if pathsecurity.Contains(root, real) {
			return real, nil
		}
	}
	return "", fmt.Errorf("path escapes configured filesystem roots: %s", value)
}

func (g *PathGuard) resolveRelative(value string) (string, error) {
	var lastErr error
	for _, root := range g.roots {
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
		for _, r := range g.roots {
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
