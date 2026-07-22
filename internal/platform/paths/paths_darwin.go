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
		root := filepath.Join(home, "Library", "Application Support", "AutoZeAgent")
		return Layout{
			Mode:       mode,
			ConfigDir:  filepath.Join(root, "config"),
			DataDir:    root,
			RuntimeDir: filepath.Join(root, "run"),
			LogDir:     filepath.Join(home, "Library", "Logs", "AutoZeAgent"),
		}, nil
	case ModeSystem:
		return Layout{
			Mode:       mode,
			ConfigDir:  "/Library/Application Support/AutoZeAgent/config",
			DataDir:    "/Library/Application Support/AutoZeAgent/data",
			RuntimeDir: "/Library/Application Support/AutoZeAgent/run",
			LogDir:     "/Library/Logs/AutoZeAgent",
		}, nil
	default:
		return Layout{}, fmt.Errorf("unsupported runtime mode %q", mode)
	}
}
