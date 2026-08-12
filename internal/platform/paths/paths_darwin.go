//go:build darwin

package paths

import (
	"fmt"
)

func resolve(mode Mode) (Layout, error) {
	switch mode {
	case ModeUser:
		return resolveUser()
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
