//go:build linux

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
			ConfigDir:  "/etc/yunmengze",
			DataDir:    "/var/lib/yunmengze",
			RuntimeDir: "/run/yunmengze",
			LogDir:     "/var/log/yunmengze",
		}, nil
	default:
		return Layout{}, fmt.Errorf("unsupported runtime mode %q", mode)
	}
}
