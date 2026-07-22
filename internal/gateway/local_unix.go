//go:build !windows

package gateway

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func listenLocal(runtimeDir string) (localListener, error) {
	address := filepath.Join(runtimeDir, "autozeagent.sock")
	if len(address) > 100 {
		return localListener{}, errors.New("gateway Unix socket path exceeds 100 bytes")
	}
	if info, err := os.Lstat(address); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return localListener{}, errors.New("refusing to replace non-socket gateway path")
		}
		if connection, dialErr := net.DialTimeout("unix", address, 250*time.Millisecond); dialErr == nil {
			_ = connection.Close()
			return localListener{}, errors.New("local gateway is already running")
		}
		if err := os.Remove(address); err != nil {
			return localListener{}, fmt.Errorf("remove stale gateway socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return localListener{}, fmt.Errorf("inspect gateway socket: %w", err)
	}
	listener, err := net.Listen("unix", address)
	if err != nil {
		return localListener{}, fmt.Errorf("listen on gateway Unix socket: %w", err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	if err := os.Chmod(address, 0o660); err != nil {
		listener.Close()
		os.Remove(address)
		return localListener{}, fmt.Errorf("secure gateway Unix socket: %w", err)
	}
	socketInfo, err := os.Lstat(address)
	if err != nil {
		listener.Close()
		os.Remove(address)
		return localListener{}, fmt.Errorf("inspect gateway Unix socket: %w", err)
	}
	return localListener{
		Listener: listener, endpoint: Endpoint{Network: "unix", Address: address}, fileMode: 0o640,
		cleanup: func() error {
			current, err := os.Lstat(address)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if !os.SameFile(socketInfo, current) {
				return nil
			}
			err = os.Remove(address)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		},
	}, nil
}
