//go:build windows

package gateway

import (
	"fmt"
	"net"
)

func listenLocal(_ string) (localListener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return localListener{}, fmt.Errorf("listen on gateway loopback: %w", err)
	}
	token, err := randomID("")
	if err != nil {
		listener.Close()
		return localListener{}, fmt.Errorf("generate gateway token: %w", err)
	}
	return localListener{
		Listener: listener,
		endpoint: Endpoint{Network: "tcp", Address: listener.Addr().String(), Token: token},
		fileMode: 0o600,
		cleanup:  func() error { return nil },
	}, nil
}
