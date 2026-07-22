package providerhttp

import (
	"net/url"
	"testing"
)

func TestEndpointPreservesQueryAndAvoidsDuplicateV1(t *testing.T) {
	base, err := url.Parse("https://example.test/root/v1")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Endpoint(base, "/v1/chat/completions?api-version=2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.String(), "https://example.test/root/v1/chat/completions?api-version=2026-01-01"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}
