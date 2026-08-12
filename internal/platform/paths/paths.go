package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Mode string

const (
	ModeUser   Mode = "user"
	ModeSystem Mode = "system"

	// DotDir is the user-mode home root (Claude/OpenCode-style), all platforms.
	DotDir = ".yunmengze"
	// HomeEnv overrides the entire user root when set to an absolute path.
	HomeEnv = "YMZ_HOME"
)

type Layout struct {
	Mode       Mode   `json:"mode"`
	ConfigDir  string `json:"config_dir"`
	DataDir    string `json:"data_dir"`
	RuntimeDir string `json:"runtime_dir"`
	LogDir     string `json:"log_dir"`
}

func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case ModeUser:
		return ModeUser, nil
	case ModeSystem:
		return ModeSystem, nil
	default:
		return "", fmt.Errorf("invalid runtime mode %q: expected user or system", value)
	}
}

// resolveUser is shared across OSes: ~/.yunmengze (or %USERPROFILE%\.yunmengze).
// Config, data, logs, and runtime live under one flat root (ConfigDir == DataDir).
func resolveUser() (Layout, error) {
	root := strings.TrimSpace(os.Getenv(HomeEnv))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Layout{}, fmt.Errorf("resolve user home: %w", err)
		}
		root = filepath.Join(home, DotDir)
	} else if !filepath.IsAbs(root) {
		return Layout{}, fmt.Errorf("%s must be an absolute path: %s", HomeEnv, root)
	}
	root = filepath.Clean(root)
	return Layout{
		Mode:       ModeUser,
		ConfigDir:  root,
		DataDir:    root,
		RuntimeDir: filepath.Join(root, "run"),
		LogDir:     filepath.Join(root, "logs"),
	}, nil
}

func Resolve(mode Mode) (Layout, error) {
	layout, err := resolve(mode)
	if err != nil {
		return Layout{}, err
	}
	layout.ConfigDir = filepath.Clean(layout.ConfigDir)
	layout.DataDir = filepath.Clean(layout.DataDir)
	layout.RuntimeDir = filepath.Clean(layout.RuntimeDir)
	layout.LogDir = filepath.Clean(layout.LogDir)
	if err := layout.Validate(); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

func (l Layout) Validate() error {
	if l.Mode != ModeUser && l.Mode != ModeSystem {
		return errors.New("layout mode must be user or system")
	}
	for name, value := range map[string]string{
		"config":  l.ConfigDir,
		"data":    l.DataDir,
		"runtime": l.RuntimeDir,
		"log":     l.LogDir,
	} {
		if value == "" || value == "." {
			return fmt.Errorf("%s directory is required", name)
		}
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s directory must be absolute: %s", name, value)
		}
	}
	return nil
}
