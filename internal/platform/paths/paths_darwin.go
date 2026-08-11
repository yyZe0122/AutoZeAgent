//go:build darwin

package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolve(mode Mode) (Layout, error) {
	switch mode {
	case ModeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return Layout{}, fmt.Errorf("resolve user home: %w", err)
		}
		root := filepath.Join(home, "Library", "Application Support", "YunmengZe")
		return Layout{
			Mode:       mode,
			ConfigDir:  filepath.Join(root, "config"),
			DataDir:    root,
			RuntimeDir: filepath.Join(root, "run"),
			LogDir:     filepath.Join(home, "Library", "Logs", "YunmengZe"),
		}, nil
	case ModeSystem:
		return Layout{
			Mode:       mode,
			ConfigDir:  "/Library/Application Support/YunmengZe/config",
			DataDir:    "/Library/Application Support/YunmengZe/data",
			RuntimeDir: "/Library/Application Support/YunmengZe/run",
			LogDir:     "/Library/Logs/YunmengZe",
		}, nil
	default:
		return Layout{}, fmt.Errorf("unsupported runtime mode %q", mode)
	}
}
