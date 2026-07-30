package gateway

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
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func NewLocalClient(runtimeDir string) (*Client, error) {
	endpoint, err := readEndpoint(runtimeDir)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: nil, DisableCompression: true, IdleConnTimeout: 30 * time.Second}
	if err := validateLocalEndpoint(runtimeDir, endpoint); err != nil {
		return nil, err
	}
	baseURL := "http://local"
	switch endpoint.Network {
	case "unix":
		address := filepath.Clean(endpoint.Address)
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", address)
		}
	case "tcp":
		baseURL = "http://" + endpoint.Address
	}
	return &Client{httpClient: &http.Client{Transport: transport}, baseURL: baseURL, token: endpoint.Token}, nil
}

func (c *Client) DoJSON(ctx context.Context, method, path string, input, output any) error {
	_, err := c.DoJSONResult(ctx, method, path, input, output)
	return err
}

// DoJSONResult is like DoJSON but also returns the HTTP status code on success.
func (c *Client) DoJSONResult(ctx context.Context, method, path string, input, output any) (int, error) {
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
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
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

// SSEEvent is one Server-Sent Event from StreamSSE.
type SSEEvent struct {
	ID    string
	Event string
	Data  []byte
}

// StreamSSE opens a long-lived GET to path (relative to the gateway base URL),
// honoring Last-Event-ID when after > 0. Events are sent until ctx is cancelled
// or the stream ends. The returned error is nil on clean ctx cancellation.
func (c *Client) StreamSSE(ctx context.Context, path string, after uint64, emit func(SSEEvent) error) error {
	if ctx == nil {
		return errors.New("gateway client context is required")
	}
	if emit == nil {
		return errors.New("SSE emit callback is required")
	}
	requestURL := c.baseURL + path
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
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("gateway returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	reader := bufio.NewReader(response.Body)
	var current SSEEvent
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
			current = SSEEvent{}
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
