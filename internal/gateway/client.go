package gateway

import (
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
	if ctx == nil {
		return errors.New("gateway client context is required")
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
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
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("gateway returned %s: %s", response.Status, strings.TrimSpace(string(payload)))
	}
	if output == nil {
		_, err = io.Copy(io.Discard, response.Body)
		return err
	}
	return json.NewDecoder(response.Body).Decode(output)
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
