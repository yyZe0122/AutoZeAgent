package providerapi

import (
	"errors"
	"strings"
)

// IsContextOverflow reports whether err looks like a model context-window overflow.
// Used for compact-and-retry (P6b); matches common OpenAI/Anthropic/Gemini/Ollama phrasing.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	var pe *ProviderError
	msg := err.Error()
	if errors.As(err, &pe) && pe.Err != nil {
		msg = pe.Err.Error() + " " + msg
	}
	lower := strings.ToLower(msg)
	markers := []string{
		"context length exceeded",
		"context_length_exceeded",
		"maximum context length",
		"max context length",
		"request_too_large",
		"request too large",
		"input exceeds the maximum number of tokens",
		"input token count exceeds the maximum",
		"input is too long for the model",
		"prompt is too long",
		"too many tokens",
		"token limit",
		"context window",
		"exceeds the context window",
		"ollama error: context length exceeded",
		"this model's maximum context length",
		"reduce the length of the messages",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
