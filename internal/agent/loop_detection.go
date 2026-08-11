package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

const (
	loopDetectionWindowSize = 10
	loopDetectionMaxRepeats = 5
)

// toolStepSignature hashes tool names+arguments for one assistant tool batch.
// Empty batch returns "".
func toolStepSignature(calls []providerapi.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	h := sha256.New()
	for _, tc := range calls {
		_, _ = h.Write([]byte(strings.TrimSpace(tc.Name)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strings.TrimSpace(tc.Arguments)))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hasRepeatedToolSteps reports whether any signature appears more than maxRepeats
// times within the last windowSize step signatures (Crush-style loop detection).
func hasRepeatedToolSteps(sigs []string, windowSize, maxRepeats int) bool {
	if windowSize < 1 || maxRepeats < 1 {
		return false
	}
	if len(sigs) < windowSize {
		return false
	}
	window := sigs[len(sigs)-windowSize:]
	counts := make(map[string]int, len(window))
	for _, sig := range window {
		if sig == "" {
			continue
		}
		counts[sig]++
		if counts[sig] > maxRepeats {
			return true
		}
	}
	return false
}
