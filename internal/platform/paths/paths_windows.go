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
		return resolveUser()
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
