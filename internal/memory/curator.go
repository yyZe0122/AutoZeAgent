package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

// FactCaller runs a short no-tool aux completion for memory curation (H1-lite).
// Implementations typically use models.compact (or main) via agent.Runner.
type FactCaller interface {
	ProposeMemoryFacts(ctx context.Context, userText, assistantText string, maxFacts int) (string, error)
}

// CuratorConfig bounds post-turn LLM fact extraction.
type CuratorConfig struct {
	Enabled   bool
	MaxFacts  int
	TimeoutMS int
	Caller    FactCaller
}

// CurateTurn extracts short durable facts after a successful chat turn.
// Failures are logged only; never returns an error that should fail the main run.
func (m *Manager) CurateTurn(ctx context.Context, sessionID, userText, assistantText string, cfg CuratorConfig) {
	if m == nil || !cfg.Enabled || cfg.Caller == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	userText = strings.TrimSpace(userText)
	assistantText = strings.TrimSpace(assistantText)
	if sessionID == "" || userText == "" {
		return
	}
	// Skip trivial turns (no user content worth curating).
	if utf8.RuneCountInString(userText) < 8 && utf8.RuneCountInString(assistantText) < 20 {
		return
	}
	maxFacts := cfg.MaxFacts
	if maxFacts <= 0 {
		maxFacts = 3
	}
	if maxFacts > 8 {
		maxFacts = 8
	}
	timeoutMS := cfg.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 15_000
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	raw, err := cfg.Caller.ProposeMemoryFacts(cctx, userText, assistantText, maxFacts)
	if err != nil {
		slog.Warn("memory curator aux failed",
			"component", "memory", "operation", "curator", "result", "warning",
			"session_id", sessionID, "error", err)
		return
	}
	facts := parseCuratorFacts(raw, maxFacts)
	if len(facts) == 0 {
		slog.Info("memory curator no facts",
			"component", "memory", "operation", "curator", "result", "succeeded",
			"session_id", sessionID, "facts", 0)
		return
	}
	written := 0
	for _, fact := range facts {
		if err := m.RememberKind(cctx, sessionID, fact, SourceCurator, []string{"curator"}, KindSession, 5, ""); err != nil {
			slog.Warn("memory curator write failed",
				"component", "memory", "operation", "curator", "result", "warning",
				"session_id", sessionID, "error", err)
			continue
		}
		written++
	}
	slog.Info("memory curator wrote facts",
		"component", "memory", "operation", "curator", "result", "succeeded",
		"session_id", sessionID, "facts", written)
}

// CuratorSystemPrompt instructs the aux model to emit JSON only.
const CuratorSystemPrompt = `You extract durable user preferences and stable facts for a local coding agent memory.
Return ONLY a JSON array of strings (0 to N items). No markdown fences, no commentary.
Each string must be one short fact in the user's language (max ~120 chars).
Include only preferences, constraints, environment facts, or decisions that should survive later turns.
Omit task-specific progress, tool logs, code dumps, secrets, and one-off questions.
If nothing durable, return [].`

// BuildCuratorUserPrompt formats the turn for aux extraction.
func BuildCuratorUserPrompt(userText, assistantText string, maxFacts int) string {
	if maxFacts <= 0 {
		maxFacts = 3
	}
	u := truncateRunes(strings.TrimSpace(userText), 1_200)
	a := truncateRunes(strings.TrimSpace(assistantText), 1_200)
	return fmt.Sprintf("Max facts: %d\n\nUser:\n%s\n\nAssistant:\n%s\n", maxFacts, u, a)
}

func parseCuratorFacts(raw string, maxFacts int) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Strip common markdown fences.
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(raw), "json") {
			raw = strings.TrimSpace(raw[4:])
		}
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		// Fallback: one fact per non-empty line, strip bullets.
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "- ")
			line = strings.TrimPrefix(line, "* ")
			line = strings.TrimSpace(line)
			if line == "" || line == "[]" || strings.EqualFold(line, "none") {
				continue
			}
			// Drop pure JSON brackets lines.
			if line == "[" || line == "]" {
				continue
			}
			list = append(list, line)
		}
	}
	out := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, fact := range list {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			continue
		}
		fact = truncateRunes(fact, 200)
		key := strings.ToLower(fact)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, fact)
		if len(out) >= maxFacts {
			break
		}
	}
	return out
}
