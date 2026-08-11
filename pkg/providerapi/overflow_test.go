package providerapi

import (
	"errors"
	"testing"
)

func TestIsContextOverflow(t *testing.T) {
	if !IsContextOverflow(errors.New("context length exceeded")) {
		t.Fatal("plain message")
	}
	if !IsContextOverflow(&ProviderError{
		Provider: "openai", Kind: ErrorInvalidRequest,
		Err: errors.New("This model's maximum context length is 128000 tokens"),
	}) {
		t.Fatal("provider error wrap")
	}
	if IsContextOverflow(errors.New("rate limit exceeded")) {
		t.Fatal("rate limit is not overflow")
	}
	if IsContextOverflow(nil) {
		t.Fatal("nil")
	}
}
