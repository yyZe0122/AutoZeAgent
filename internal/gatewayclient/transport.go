package gatewayclient

import (
	"bufio"
	"bytes"
	"context"
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
	"time"
)

const endpointFilename = "gateway.json"

// endpoint is the published local gateway discovery file (gateway.json).
// Shape matches the daemon-side descriptor; client keeps a local copy so it
// does not import the server package.
type endpoint struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Token   string `json:"token,omitempty"`
}

// transport is the low-level HTTP/SSE client for the local gateway.
type transport struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func newLocalTransport(runtimeDir string) (*transport, error) {
	ep, err := readEndpoint(runtimeDir)
	if err != nil {
		return nil, err
	}
	httpTransport := &http.Transport{Proxy: nil, DisableCompression: true, IdleConnTimeout: 30 * time.Second}
	if err := validateLocalEndpoint(runtimeDir, ep); err != nil {
		return nil, err
	}
	baseURL := "http://local"
	switch ep.Network {
	case "unix":
		address := filepath.Clean(ep.Address)
		httpTransport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", address)
		}
	case "tcp":
		baseURL = "http://" + ep.Address
	}
	return &transport{httpClient: &http.Client{Transport: httpTransport}, baseURL: baseURL, token: ep.Token}, nil
}

func (t *transport) DoJSON(ctx context.Context, method, path string, input, output any) error {
	_, err := t.DoJSONResult(ctx, method, path, input, output)
	return err
}

// DoJSONResult is like DoJSON but also returns the HTTP status code on success.
func (t *transport) DoJSONResult(ctx context.Context, method, path string, input, output any) (int, error) {
	if ctx == nil {
		return 0, errors.New("gateway client context is required")
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if t.token != "" {
		request.Header.Set("Authorization", "Bearer "+t.token)
	}
	response, err := t.httpClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return response.StatusCode, fmt.Errorf("gateway returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	if output == nil {
		_, err = io.Copy(io.Discard, response.Body)
		return response.StatusCode, err
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return response.StatusCode, err
	}
	return response.StatusCode, nil
}

// sseEvent is one Server-Sent Event from StreamSSE.
type sseEvent struct {
	ID    string
	Event string
	Data  []byte
}

// StreamSSE opens a long-lived GET to path (relative to the gateway base URL),
// honoring Last-Event-ID when after > 0. Events are sent until ctx is cancelled
// or the stream ends. The returned error is nil on clean ctx cancellation.
func (t *transport) StreamSSE(ctx context.Context, path string, after uint64, emit func(sseEvent) error) error {
	if ctx == nil {
		return errors.New("gateway client context is required")
	}
	if emit == nil {
		return errors.New("SSE emit callback is required")
	}
	requestURL := t.baseURL + path
	if after > 0 {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		requestURL += separator + "after=" + fmt.Sprintf("%d", after)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	if after > 0 {
		request.Header.Set("Last-Event-ID", fmt.Sprintf("%d", after))
	}
	if t.token != "" {
		request.Header.Set("Authorization", "Bearer "+t.token)
	}
	response, err := t.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("gateway returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	reader := bufio.NewReader(response.Body)
	var current sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if current.Data != nil || current.Event != "" || current.ID != "" {
					if emitErr := emit(current); emitErr != nil {
						return emitErr
					}
				}
				if ctx.Err() != nil {
					return nil
				}
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if current.Data != nil || current.Event != "" || current.ID != "" {
				if emitErr := emit(current); emitErr != nil {
					return emitErr
				}
			}
			current = sseEvent{}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		switch field {
		case "id":
			current.ID = value
		case "event":
			current.Event = value
		case "data":
			if current.Data == nil {
				current.Data = []byte(value)
			} else {
				current.Data = append(current.Data, '\n')
				current.Data = append(current.Data, value...)
			}
		}
	}
}

func readEndpoint(runtimeDir string) (endpoint, error) {
	path := filepath.Join(filepath.Clean(runtimeDir), endpointFilename)
	info, err := os.Lstat(path)
	if err != nil {
		return endpoint{}, fmt.Errorf("read gateway endpoint: %w", err)
	}
	if !info.Mode().IsRegular() {
		return endpoint{}, errors.New("gateway endpoint is not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return endpoint{}, fmt.Errorf("read gateway endpoint: %w", err)
	}
	var ep endpoint
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ep); err != nil {
		return endpoint{}, fmt.Errorf("decode gateway endpoint: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return endpoint{}, errors.New("gateway endpoint must contain one JSON value")
	}
	if strings.TrimSpace(ep.Network) == "" || strings.TrimSpace(ep.Address) == "" {
		return endpoint{}, errors.New("gateway endpoint is incomplete")
	}
	return ep, nil
}

func validateLocalEndpoint(runtimeDir string, ep endpoint) error {
	switch ep.Network {
	case "unix":
		address := filepath.Clean(ep.Address)
		relative, err := filepath.Rel(filepath.Clean(runtimeDir), address)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return errors.New("gateway Unix socket escapes runtime directory")
		}
	case "tcp":
		host, _, err := net.SplitHostPort(ep.Address)
		if err != nil {
			return fmt.Errorf("invalid gateway loopback address: %w", err)
		}
		address, err := netip.ParseAddr(host)
		if err != nil || !address.IsLoopback() {
			return errors.New("gateway TCP address must be loopback")
		}
		if ep.Token == "" {
			return errors.New("gateway TCP endpoint requires authentication")
		}
	default:
		return fmt.Errorf("unsupported gateway network %q", ep.Network)
	}
	return nil
}
