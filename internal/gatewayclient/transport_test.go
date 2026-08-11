package gatewayclient

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDoJSONResultReturnsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tasks" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	tr := &transport{httpClient: server.Client(), baseURL: server.URL}
	var out map[string]any
	status, err := tr.DoJSONResult(context.Background(), http.MethodPost, "/v1/tasks", map[string]string{"title": "t"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusAccepted {
		t.Fatalf("status = %d", status)
	}
	if out["ok"] != true {
		t.Fatalf("out = %#v", out)
	}
}

func TestDoJSONResultPropagatesErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"conflict","message":"nope"}}`)
	}))
	t.Cleanup(server.Close)

	tr := &transport{httpClient: server.Client(), baseURL: server.URL}
	status, err := tr.DoJSONResult(context.Background(), http.MethodGet, "/v1/x", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if status != http.StatusConflict {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(err.Error(), "409") {
		t.Fatalf("error = %v", err)
	}
}

func TestStreamSSEParsesEventsAndHonorsCancel(t *testing.T) {
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Last-Event-ID") != "7" {
			t.Fatalf("Last-Event-ID = %q", r.Header.Get("Last-Event-ID"))
		}
		if !strings.Contains(r.URL.RawQuery, "after=7") {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("flusher unavailable")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "id: 8\nevent: task.updated\ndata: {\"sequence\":8}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "id: 9\nevent: plan.ready\ndata: line-a\ndata: line-b\n\n")
		flusher.Flush()
		once.Do(func() {})
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	tr := &transport{httpClient: server.Client(), baseURL: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []sseEvent
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.StreamSSE(ctx, "/v1/events/stream", 7, func(event sseEvent) error {
			got = append(got, event)
			if len(got) == 2 {
				cancel()
			}
			return nil
		})
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for stream")
	}

	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].ID != "8" || got[0].Event != "task.updated" {
		t.Fatalf("first = %#v", got[0])
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["sequence"] != float64(8) {
		t.Fatalf("payload = %#v", payload)
	}
	if got[1].ID != "9" || string(got[1].Data) != "line-a\nline-b" {
		t.Fatalf("second = %#v", got[1])
	}
}

func TestNewLocalTransportUnixRoundTrip(t *testing.T) {
	runtimeDir := t.TempDir()
	socketPath := filepath.Join(runtimeDir, "gateway.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	ep := endpoint{Network: "unix", Address: socketPath}
	raw, err := json.Marshal(ep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, endpointFilename), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	tr, err := newLocalTransport(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := tr.DoJSON(context.Background(), http.MethodGet, "/v1/health", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("out = %#v", out)
	}
}
