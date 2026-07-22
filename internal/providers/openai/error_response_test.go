package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStatusErrorIncludesStandardProviderEnvelope(t *testing.T) {
	provider, err := New(Config{Name: "deepseek", BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"error": {
				"message": "This response_format type is unavailable now",
				"type": "invalid_request_error",
				"code": "invalid_request_error",
				"param": "response_format"
			}
		}`)),
	}

	message := provider.statusError(response).Error()
	for _, expected := range []string{
		"This response_format type is unavailable now",
		"type=invalid_request_error",
		"code=invalid_request_error",
		"param=response_format",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("status error %q does not contain %q", message, expected)
		}
	}
}

func TestProviderStatusMessageIgnoresUnknownEnvelopeFields(t *testing.T) {
	err := providerStatusMessage(strings.NewReader(`{
		"error": {
			"message": "bad request",
			"type": "invalid_request_error",
			"secret": "must-not-appear"
		},
		"debug": "also-must-not-appear"
	}`))
	message := err.Error()
	if !strings.Contains(message, "bad request") {
		t.Fatalf("message = %q", message)
	}
	if strings.Contains(message, "must-not-appear") {
		t.Fatalf("unknown provider fields leaked into error: %q", message)
	}
}

func TestProviderStatusMessageFallsBackForUnsafeBodies(t *testing.T) {
	const fallback = "provider rejected the request"
	tests := []struct {
		name string
		body io.Reader
	}{
		{name: "nil", body: nil},
		{name: "not json", body: strings.NewReader("upstream proxy failure")},
		{name: "missing message", body: strings.NewReader(`{"error":{"type":"invalid_request_error"}}`)},
		{name: "oversized", body: strings.NewReader(strings.Repeat("x", int(maxProviderErrorBytes)+1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if message := providerStatusMessage(test.body).Error(); message != fallback {
				t.Fatalf("message = %q, want %q", message, fallback)
			}
		})
	}
}

func TestProviderStatusMessageBoundsAndNormalizesFields(t *testing.T) {
	longMessage := strings.Repeat("界", 600)
	longType := strings.Repeat("t", 120)
	body := `{"error":{"message":"` + longMessage + `\n  detail","type":"` + longType + `","code":429}}`

	message := providerStatusMessage(strings.NewReader(body)).Error()
	if strings.Contains(message, "\n") || strings.Contains(message, "  ") {
		t.Fatalf("message whitespace was not normalized: %q", message)
	}
	if !strings.Contains(message, "...") || !strings.Contains(message, "code=429") {
		t.Fatalf("message was not bounded or scalar code was omitted: %q", message)
	}
}
