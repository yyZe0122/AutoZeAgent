package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubModelSwitcher struct {
	config ModelConfig
	err    error
	calls  int
}

func (s *stubModelSwitcher) SelectModel(_ context.Context, ref string) (ModelConfig, error) {
	s.calls++
	if s.err != nil {
		return ModelConfig{}, s.err
	}
	s.config.Model = ref
	if s.config.Models == nil {
		s.config.Models = []string{ref}
	}
	return s.config, nil
}

func TestConfigModelEndpointReturnsSnapshot(t *testing.T) {
	api := &API{modelConfig: ModelConfig{
		Model:         "deepseek/deepseek-v4-flash",
		Models:        []string{"deepseek/deepseek-v4-flash", "deepseek/deepseek-chat"},
		ContextWindow: 65536,
	}}
	request := httptest.NewRequest(http.MethodGet, "/v1/config/model", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body ModelConfig
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "deepseek/deepseek-v4-flash" || len(body.Models) != 2 || body.ContextWindow != 65536 {
		t.Fatalf("body = %+v", body)
	}
	raw := strings.ToLower(response.Body.String())
	if strings.Contains(raw, "apikey") || strings.Contains(raw, "api_key") {
		t.Fatalf("response leaked secrets: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "context_window") {
		t.Fatalf("expected context_window in body: %s", response.Body.String())
	}
}

func TestConfigModelEndpointEmptyModels(t *testing.T) {
	api := &API{}
	request := httptest.NewRequest(http.MethodGet, "/v1/config/model", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body ModelConfig
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Models == nil {
		t.Fatal("models should be empty slice not null")
	}
}

func TestConfigModelPutSwitchesModel(t *testing.T) {
	switcher := &stubModelSwitcher{config: ModelConfig{
		Models: []string{"deepseek/deepseek-v4-flash", "deepseek/deepseek-chat"},
	}}
	api := &API{
		modelConfig: ModelConfig{
			Model:  "deepseek/deepseek-v4-flash",
			Models: []string{"deepseek/deepseek-v4-flash", "deepseek/deepseek-chat"},
		},
		modelSwitcher: switcher,
	}
	request := httptest.NewRequest(http.MethodPut, "/v1/config/model", bytes.NewBufferString(`{"model":"deepseek/deepseek-chat"}`))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body ModelConfig
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "deepseek/deepseek-chat" || switcher.calls != 1 {
		t.Fatalf("body = %+v calls = %d", body, switcher.calls)
	}
	get := httptest.NewRequest(http.MethodGet, "/v1/config/model", nil)
	getResponse := httptest.NewRecorder()
	api.ServeHTTP(getResponse, get)
	if err := json.Unmarshal(getResponse.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "deepseek/deepseek-chat" {
		t.Fatalf("persisted model = %+v", body)
	}
}

func TestConfigModelPutUnavailableWithoutSwitcher(t *testing.T) {
	api := &API{
		modelConfig:      ModelConfig{Model: "a/b", Models: []string{"a/b"}},
		modelConfigError: "model \"x\" is not in provider \"deepseek\" catalog",
	}
	request := httptest.NewRequest(http.MethodPut, "/v1/config/model", bytes.NewBufferString(`{"model":"a/c"}`))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "catalog") {
		t.Fatalf("expected load error in body: %s", response.Body.String())
	}
}

func TestConfigModelGetSurfacesLoadError(t *testing.T) {
	api := &API{
		modelConfig:      ModelConfig{Model: "a/b", Models: []string{"a/b"}},
		modelConfigError: "environment variable DEEPSEEK_API_KEY is unavailable",
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/config/model", nil)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body ModelConfig
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Ready || !strings.Contains(body.Error, "DEEPSEEK_API_KEY") {
		t.Fatalf("body = %+v", body)
	}
}

func TestConfigModelPutRejectsInvalid(t *testing.T) {
	switcher := &stubModelSwitcher{err: errors.New("model not configured")}
	api := &API{modelSwitcher: switcher}
	request := httptest.NewRequest(http.MethodPut, "/v1/config/model", bytes.NewBufferString(`{"model":"x/y"}`))
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}
