package tools

import (
	"context"
	"net/netip"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"8.8.8.8", false},
		{"::1", true},
		{"fc00::1", true},
	}
	for _, tc := range cases {
		ip, err := netip.ParseAddr(tc.ip)
		if err != nil {
			t.Fatalf("%s: %v", tc.ip, err)
		}
		if got := isBlockedIP(ip); got != tc.blocked {
			t.Fatalf("%s blocked=%v want %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestValidateHTTPGetURLHostLiterals(t *testing.T) {
	ctx := context.Background()
	for _, host := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "localhost", "metadata.google.internal"} {
		if err := validateHTTPGetURLHost(ctx, host); err == nil {
			t.Fatalf("expected block for %s", host)
		}
	}
}
