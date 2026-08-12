package providerruntime

import (
	"testing"

	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
)

func TestFingerprintIncludesAPIKeyHash(t *testing.T) {
	base := &providerconfig.Resolved{
		ProviderID: "p", ModelID: "m", Protocol: "openai-compatible",
		BaseURL: "https://example.com", APIKey: "sk-old",
	}
	a := Fingerprint(base)
	base.APIKey = "sk-new"
	b := Fingerprint(base)
	if a == "" || a == b {
		t.Fatalf("fingerprint should change with API key: %q vs %q", a, b)
	}
	if contains(a, "sk-old") || contains(b, "sk-new") {
		t.Fatal("fingerprint must not contain raw API key")
	}
}

func TestFingerprintIncludesHeaders(t *testing.T) {
	r := &providerconfig.Resolved{
		ProviderID: "p", ModelID: "m", Protocol: "openai-compatible",
		BaseURL: "https://example.com", APIKey: "k",
		Headers: map[string]string{"X-A": "1"},
	}
	a := Fingerprint(r)
	r.Headers["X-A"] = "2"
	b := Fingerprint(r)
	if a == b {
		t.Fatal("header change should change fingerprint")
	}
}

func TestFingerprintStableHeaderOrder(t *testing.T) {
	a := Fingerprint(&providerconfig.Resolved{
		ProviderID: "p", ModelID: "m", Protocol: "x", BaseURL: "u", APIKey: "k",
		Headers: map[string]string{"b": "2", "a": "1"},
	})
	b := Fingerprint(&providerconfig.Resolved{
		ProviderID: "p", ModelID: "m", Protocol: "x", BaseURL: "u", APIKey: "k",
		Headers: map[string]string{"a": "1", "b": "2"},
	})
	if a != b {
		t.Fatalf("order should not matter: %q vs %q", a, b)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || len(s) > 0 && stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
