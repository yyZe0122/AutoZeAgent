package gateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const endpointFilename = "gateway.json"

type Endpoint struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Token   string `json:"token,omitempty"`
}

type localListener struct {
	net.Listener
	endpoint Endpoint
	fileMode os.FileMode
	cleanup  func() error
}

type LocalRunnerConfig struct {
	RuntimeDir string
	Handler    http.Handler
	OnError    func(error)
}

type LocalRunner struct {
	runtimeDir     string
	descriptorInfo os.FileInfo
	listener       localListener
	server         *http.Server
	onError        func(error)
	cleanupOnce    sync.Once
	cleanupErr     error
}

func NewLocalRunner(config LocalRunnerConfig) (*LocalRunner, error) {
	if strings.TrimSpace(config.RuntimeDir) == "" || config.Handler == nil {
		return nil, errors.New("gateway runtime directory and handler are required")
	}
	runtimeDir := filepath.Clean(config.RuntimeDir)
	if err := os.MkdirAll(runtimeDir, 0o750); err != nil {
		return nil, fmt.Errorf("create gateway runtime directory: %w", err)
	}
	if err := ensureNoActiveEndpoint(runtimeDir); err != nil {
		return nil, err
	}
	listener, err := listenLocal(runtimeDir)
	if err != nil {
		return nil, err
	}
	if err := writeEndpoint(runtimeDir, listener.endpoint, listener.fileMode); err != nil {
		_ = listener.Close()
		_ = listener.cleanup()
		return nil, err
	}
	descriptorInfo, err := os.Stat(filepath.Join(runtimeDir, endpointFilename))
	if err != nil {
		_ = listener.Close()
		_ = listener.cleanup()
		return nil, fmt.Errorf("inspect published gateway endpoint: %w", err)
	}
	handler := authenticateLocal(listener.endpoint.Token, config.Handler)
	return &LocalRunner{
		runtimeDir: runtimeDir, descriptorInfo: descriptorInfo, listener: listener,
		server:  &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second},
		onError: config.OnError,
	}, nil
}

func (r *LocalRunner) Run(ctx context.Context) {
	if ctx == nil {
		r.report(errors.New("gateway context is required"))
		return
	}
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = r.server.Shutdown(shutdownCtx)
			cancel()
		case <-stopped:
		}
	}()
	err := r.server.Serve(r.listener)
	close(stopped)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		r.report(fmt.Errorf("serve local gateway: %w", err))
	}
	if cleanupErr := r.cleanup(); cleanupErr != nil {
		r.report(cleanupErr)
	}
}

func (r *LocalRunner) Close() error {
	if r == nil {
		return nil
	}
	_ = r.server.Close()
	return r.cleanup()
}

func (r *LocalRunner) cleanup() error {
	r.cleanupOnce.Do(func() {
		var joined error
		if err := r.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			joined = errors.Join(joined, err)
		}
		if r.listener.cleanup != nil {
			joined = errors.Join(joined, r.listener.cleanup())
		}
		if err := removeOwnedEndpoint(r.runtimeDir, r.descriptorInfo, r.listener.endpoint); err != nil {
			joined = errors.Join(joined, err)
		}
		r.cleanupErr = joined
	})
	return r.cleanupErr
}

func (r *LocalRunner) report(err error) {
	if err != nil && r.onError != nil {
		r.onError(err)
	}
}

func authenticateLocal(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := []byte(r.Header.Get("Authorization"))
		if len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "local gateway authentication failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeEndpoint(runtimeDir string, endpoint Endpoint, mode os.FileMode) error {
	encoded, err := json.Marshal(endpoint)
	if err != nil {
		return fmt.Errorf("encode gateway endpoint: %w", err)
	}
	temporary, err := os.CreateTemp(runtimeDir, ".gateway-*.tmp")
	if err != nil {
		return fmt.Errorf("create gateway endpoint file: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("secure gateway endpoint file: %w", err)
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write gateway endpoint file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync gateway endpoint file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close gateway endpoint file: %w", err)
	}
	target := filepath.Join(runtimeDir, endpointFilename)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace gateway endpoint file: %w", err)
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("publish gateway endpoint file: %w", err)
	}
	return nil
}

func ensureNoActiveEndpoint(runtimeDir string) error {
	endpoint, err := readEndpoint(runtimeDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing gateway endpoint: %w", err)
	}
	if err := validateLocalEndpoint(runtimeDir, endpoint); err != nil {
		return fmt.Errorf("inspect existing gateway endpoint: %w", err)
	}
	connection, err := net.DialTimeout(endpoint.Network, endpoint.Address, 250*time.Millisecond)
	if err != nil {
		return nil
	}
	_ = connection.Close()
	return errors.New("local gateway is already running")
}

func readEndpoint(runtimeDir string) (Endpoint, error) {
	path := filepath.Join(filepath.Clean(runtimeDir), endpointFilename)
	info, err := os.Lstat(path)
	if err != nil {
		return Endpoint{}, fmt.Errorf("read gateway endpoint: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Endpoint{}, errors.New("gateway endpoint is not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Endpoint{}, fmt.Errorf("read gateway endpoint: %w", err)
	}
	var endpoint Endpoint
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&endpoint); err != nil {
		return Endpoint{}, fmt.Errorf("decode gateway endpoint: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Endpoint{}, errors.New("gateway endpoint must contain one JSON value")
	}
	if strings.TrimSpace(endpoint.Network) == "" || strings.TrimSpace(endpoint.Address) == "" {
		return Endpoint{}, errors.New("gateway endpoint is incomplete")
	}
	return endpoint, nil
}

func validateLocalEndpoint(runtimeDir string, endpoint Endpoint) error {
	switch endpoint.Network {
	case "unix":
		address := filepath.Clean(endpoint.Address)
		relative, err := filepath.Rel(filepath.Clean(runtimeDir), address)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return errors.New("gateway Unix socket escapes runtime directory")
		}
	case "tcp":
		host, _, err := net.SplitHostPort(endpoint.Address)
		if err != nil {
			return fmt.Errorf("invalid gateway loopback address: %w", err)
		}
		address, err := netip.ParseAddr(host)
		if err != nil || !address.IsLoopback() {
			return errors.New("gateway TCP address must be loopback")
		}
		if endpoint.Token == "" {
			return errors.New("gateway TCP endpoint requires authentication")
		}
	default:
		return fmt.Errorf("unsupported gateway network %q", endpoint.Network)
	}
	return nil
}

func removeOwnedEndpoint(runtimeDir string, owned os.FileInfo, endpoint Endpoint) error {
	if owned == nil {
		return nil
	}
	descriptor := filepath.Join(runtimeDir, endpointFilename)
	currentEndpoint, err := readEndpoint(runtimeDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read gateway endpoint during cleanup: %w", err)
	}
	if currentEndpoint != endpoint {
		return nil
	}
	current, err := os.Lstat(descriptor)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect gateway endpoint during cleanup: %w", err)
	}
	if !os.SameFile(owned, current) {
		return nil
	}
	if err := os.Remove(descriptor); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove gateway endpoint: %w", err)
	}
	return nil
}
