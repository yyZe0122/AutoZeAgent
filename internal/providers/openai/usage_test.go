package openai

import "testing"

func TestUsageFromCachedPrompt(t *testing.T) {
	u := usageFrom(tokenUsage{
		PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
		PromptTokensDetails: &struct {
			CachedTokens int64 `json:"cached_tokens"`
		}{CachedTokens: 40},
	})
	if int64(u.CacheReadTokens) != 40 {
		t.Fatalf("cache read = %d", u.CacheReadTokens)
	}
	if int64(u.InputTokens) != 60 {
		t.Fatalf("uncached input = %d", u.InputTokens)
	}
	rate, ok := u.CacheHitRate()
	if !ok || rate != 0.4 {
		t.Fatalf("rate=%v ok=%v", rate, ok)
	}
}
