package agent

import (
	"fmt"
	"unicode/utf8"

	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

// DefaultMaxToolResultRunes is the max tool-result Content length sent to the
// provider. Persisted agent_run_records keep the full text (ADR-030).
const DefaultMaxToolResultRunes = 32 * 1024

const trimMarkerFmt = "\n...[trimmed %d runes for provider context]...\n"

// trimMessagesForProvider returns a shallow-copied message list with oversized
// tool (and plain assistant) Content shortened for the provider request.
// The input slice and its messages are not modified.
func trimMessagesForProvider(messages []providerapi.Message, maxToolResultRunes int) []providerapi.Message {
	if maxToolResultRunes <= 0 {
		maxToolResultRunes = DefaultMaxToolResultRunes
	}
	if len(messages) == 0 {
		return nil
	}
	out := make([]providerapi.Message, len(messages))
	for i, msg := range messages {
		out[i] = cloneMessage(msg)
		switch msg.Role {
		case providerapi.RoleTool:
			out[i].Content = trimRunes(msg.Content, maxToolResultRunes)
		case providerapi.RoleAssistant:
			// Only trim plain assistant text; keep tool-call rounds intact.
			if len(msg.ToolCalls) == 0 {
				out[i].Content = trimRunes(msg.Content, maxToolResultRunes)
			}
		}
	}
	return out
}

func trimRunes(content string, maxRunes int) string {
	if maxRunes < 16 {
		maxRunes = 16
	}
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	// Provisional marker using upper bound on omitted count (digit width).
	marker := fmt.Sprintf(trimMarkerFmt, len(runes))
	markerLen := utf8.RuneCountInString(marker)
	if markerLen >= maxRunes {
		return string(runes[:maxRunes])
	}
	keep := maxRunes - markerLen
	head := keep / 2
	tail := keep - head
	if head < 1 {
		head = 1
	}
	if tail < 1 {
		tail = 1
	}
	if head+tail >= len(runes) {
		return content
	}
	omitted := len(runes) - head - tail
	marker = fmt.Sprintf(trimMarkerFmt, omitted)
	markerLen = utf8.RuneCountInString(marker)
	// Digit width may shrink; reclaim budget into head if possible.
	if head+markerLen+tail < maxRunes {
		extra := maxRunes - head - markerLen - tail
		head += extra
	}
	if head+markerLen+tail > maxRunes {
		overflow := head + markerLen + tail - maxRunes
		if tail > overflow {
			tail -= overflow
		} else {
			overflow -= tail
			tail = 0
			if head > overflow {
				head -= overflow
			} else {
				head = 0
			}
		}
	}
	if head+tail >= len(runes) || head < 0 || tail < 0 {
		return string(runes[:maxRunes])
	}
	return string(runes[:head]) + marker + string(runes[len(runes)-tail:])
}
