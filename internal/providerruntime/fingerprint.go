package providerruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
)

// Fingerprint returns a stable signature for reload skip detection.
// Secrets (API key, headers) are hashed; never log the raw fingerprint as it
// still encodes secret material indirectly.
func Fingerprint(resolved *providerconfig.Resolved) string {
	if resolved == nil {
		return ""
	}
	temp := ""
	if resolved.Temperature != nil {
		temp = fmt.Sprintf("%g", *resolved.Temperature)
	}
	return strings.Join([]string{
		resolved.ProviderID,
		resolved.ModelID,
		resolved.Protocol,
		resolved.BaseURL,
		resolved.CompletionPath,
		resolved.ModelsPath,
		resolved.ResponseFormat,
		temp,
		fmt.Sprintf("%d", resolved.MaxTokens),
		fmt.Sprintf("%d", resolved.ContextWindow),
		resolved.ReasoningEffort,
		resolved.AnthropicVersion,
		secretDigest(resolved.APIKey, resolved.Headers),
	}, "\x1e")
}

func secretDigest(apiKey string, headers map[string]string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(apiKey))
	_, _ = h.Write([]byte{0})
	if len(headers) == 0 {
		return hex.EncodeToString(h.Sum(nil)[:16])
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(headers[k]))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}
