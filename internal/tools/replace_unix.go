//go:build !windows

package tools

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
