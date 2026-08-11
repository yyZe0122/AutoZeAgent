package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

func TestAutomaticResponseFormatFallsBackAndCachesByModel(t *testing.T) {
	var mutex sync.Mutex
	var formats []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		formats = append(formats, payload.ResponseFormat.Type)
		mutex.Unlock()
		if payload.ResponseFormat.Type == ResponseFormatJSONSchema {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"This response_format type is unavailable now","type":"invalid_request_error","code":"invalid_request_error"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"{\"answer\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	request := structuredResponseRequest()
	request.Model = "deepseek-v4-flash"
	for range 2 {
		if _, err := provider.Complete(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}

	mutex.Lock()
	defer mutex.Unlock()
	want := []string{ResponseFormatJSONSchema, ResponseFormatJSONObject, ResponseFormatJSONObject}
	if len(formats) != len(want) {
		t.Fatalf("formats = %v, want %v", formats, want)
	}
	for index := range want {
		if formats[index] != want[index] {
			t.Fatalf("formats = %v, want %v", formats, want)
		}
	}
}

func TestAutomaticResponseFormatDoesNotCacheFailedFallback(t *testing.T) {
	var mutex sync.Mutex
	var formats []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		formats = append(formats, payload.ResponseFormat.Type)
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if payload.ResponseFormat.Type == ResponseFormatJSONSchema {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"response_format json_schema is unsupported","type":"invalid_request_error"}}`))
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"temporary failure","type":"server_error"}}`))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	request := structuredResponseRequest()
	request.Model = "fallback-fails"
	for range 2 {
		if _, err := provider.Complete(context.Background(), request); err == nil {
			t.Fatal("expected provider error")
		}
	}

	mutex.Lock()
	defer mutex.Unlock()
	want := []string{ResponseFormatJSONSchema, ResponseFormatJSONObject, ResponseFormatJSONSchema, ResponseFormatJSONObject}
	if len(formats) != len(want) {
		t.Fatalf("formats = %v, want %v", formats, want)
	}
	for index := range want {
		if formats[index] != want[index] {
			t.Fatalf("formats = %v, want %v", formats, want)
		}
	}
}

func TestExplicitJSONSchemaDoesNotFallBack(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"response_format json_schema is unsupported","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, ResponseFormat: ResponseFormatJSONSchema})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), providerapi.CompletionRequest{
		Model: "test-model", Messages: []providerapi.Message{{Role: providerapi.RoleUser, Content: "JSON"}},
		ResponseSchema: structuredResponseRequest().ResponseSchema,
	})
	if err == nil {
		t.Fatal("expected provider error")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
