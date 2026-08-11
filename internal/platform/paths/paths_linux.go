//go:build linux

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
		configBase := envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		dataBase := envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		runtimeBase := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeBase == "" {
			runtimeBase = filepath.Join(dataBase, "yunmengze", "run")
		}
		return Layout{
			Mode:       mode,
			ConfigDir:  filepath.Join(configBase, "yunmengze"),
			DataDir:    filepath.Join(dataBase, "yunmengze"),
			RuntimeDir: filepath.Join(runtimeBase, "yunmengze"),
			LogDir:     filepath.Join(dataBase, "yunmengze", "logs"),
		}, nil
	case ModeSystem:
		return Layout{
			Mode:       mode,
			ConfigDir:  "/etc/yunmengze",
			DataDir:    "/var/lib/yunmengze",
			RuntimeDir: "/run/yunmengze",
			LogDir:     "/var/log/yunmengze",
		}, nil
	default:
		return Layout{}, fmt.Errorf("unsupported runtime mode %q", mode)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
