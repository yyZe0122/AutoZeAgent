//go:build windows

package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolve(mode Mode) (Layout, error) {
	switch mode {
	case ModeUser:
		configBase, err := os.UserConfigDir()
		if err != nil {
			return Layout{}, fmt.Errorf("resolve user config directory: %w", err)
		}
		dataBase := os.Getenv("LOCALAPPDATA")
		if dataBase == "" {
			dataBase = configBase
		}
		root := filepath.Join(dataBase, "YunmengZe")
		return Layout{
			Mode:       mode,
			ConfigDir:  filepath.Join(configBase, "YunmengZe"),
			DataDir:    root,
			RuntimeDir: filepath.Join(root, "run"),
			LogDir:     filepath.Join(root, "logs"),
		}, nil
	case ModeSystem:
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		root := filepath.Join(programData, "YunmengZe")
		return Layout{
			Mode:       mode,
			ConfigDir:  filepath.Join(root, "config"),
			DataDir:    filepath.Join(root, "data"),
			RuntimeDir: filepath.Join(root, "run"),
			LogDir:     filepath.Join(root, "logs"),
		}, nil
	default:
		return Layout{}, fmt.Errorf("unsupported runtime mode %q", mode)
	}
}
