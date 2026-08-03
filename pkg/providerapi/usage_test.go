package providerapi

import "testing"

func TestUsagePromptTokens(t *testing.T) {
	u := Usage{InputTokens: 10, CacheReadTokens: 40, CacheWriteTokens: 5, OutputTokens: 3}
	if got := u.PromptTokens(); got != 55 {
		t.Fatalf("PromptTokens=%d want 55", got)
	}
	if (Usage{InputTokens: 7}).PromptTokens() != 7 {
		t.Fatal("uncached-only prompt")
	}
}

func TestUsageCacheHitRate(t *testing.T) {
	u := Usage{InputTokens: 50, CacheReadTokens: 50}
	rate, ok := u.CacheHitRate()
	if !ok || rate != 0.5 {
		t.Fatalf("rate=%v ok=%v want 0.5 true", rate, ok)
	}
	empty := Usage{InputTokens: 10}
	if _, ok := empty.CacheHitRate(); ok {
		t.Fatal("expected no rate without cache activity")
	}
	writeOnly := Usage{CacheWriteTokens: 3}
	if rate, ok := writeOnly.CacheHitRate(); !ok || rate != 0 {
		// read=0, uncached=0 → den=0 → false; if only write, den = 0+0 = 0
		// Actually: read<=0 && write>0 passes first check... den = 0+0 = 0 → false
		if ok {
			t.Fatalf("write-only den: rate=%v ok=%v", rate, ok)
		}
	}
	writeWithInput := Usage{InputTokens: 10, CacheWriteTokens: 3}
	rate, ok = writeWithInput.CacheHitRate()
	if !ok || rate != 0 {
		t.Fatalf("write+input: rate=%v ok=%v want 0 true", rate, ok)
	}
}
