package chatsession

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"autozeagent.local/autozeagent/internal/contextpack"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

func (s *Service) loadHistory(ctx context.Context, sessionID kernel.SessionID, currentTask kernel.TaskID, currentUser string) ([]providerapi.Message, error) {
	// Wide fetch; token packing / compaction shrinks the provider view.
	out, err := s.loadTranscriptMessages(ctx, sessionID, currentTask, currentUser)
	if err != nil {
		return nil, err
	}
	return s.packSessionHistory(ctx, sessionID, out)
}

func (s *Service) packSessionHistory(ctx context.Context, sessionID kernel.SessionID, history []providerapi.Message) ([]providerapi.Message, error) {
	if len(history) == 0 {
		return history, nil
	}
	// Work from the full transcript for pressure + optional re-summary; inject
	// latest durable summary only after deciding whether to compact again.
	full := history
	prevSummary := ""
	if s.contextStore != nil {
		if c, err := s.contextStore.LatestCompaction(ctx, string(sessionID)); err == nil && strings.TrimSpace(c.Summary) != "" {
			prevSummary = strings.TrimSpace(c.Summary)
		}
	}

	window := s.contextWindow
	// Leave room for system + current user + tools + output inside agent pack.
	usable := contextpack.UsableWindow(window, 8_192, 0)
	budget := int64(0)
	if usable > 0 {
		// History should leave ~40% of usable for current turn + tools.
		budget = usable * 60 / 100
		if budget < 2048 {
			budget = 2048
		}
	}

	// Provider-view candidate: summary + short tail when we already have a durable summary.
	view := full
	if prevSummary != "" {
		_, tail := contextpack.SplitHeadTail(full, 2)
		view = append([]providerapi.Message{{
			Role:    providerapi.RoleSystem,
			Content: "[Prior session context — compacted]\n" + prevSummary,
		}}, stripLeadingSystem(tail)...)
	}

	raw := contextpack.EstimateMessages(view)
	est := raw
	if s.calibrator != nil {
		est = s.calibrator.Apply("", raw)
	}
	packed := contextpack.Pack(view, contextpack.PackOptions{Budget: budget})
	over := packed.OverBudget || (usable > 0 && contextpack.ShouldCompact(est, usable, packed.OverBudget))

	if s.compactionEnabled && over && len(full) > 4 {
		// Always split the full transcript so re-compact can run when the tail alone is huge.
		head, tail := contextpack.SplitHeadTail(full, 2)
		if len(head) > 0 {
			// ADR-044: extract facts before head is replaced by summary.
			if s.memory != nil {
				s.memory.OnPreCompress(ctx, string(sessionID), head)
			}
			allowLLM := true
			if s.contextStore != nil {
				allowLLM = s.contextStore.AllowLLMCompact(ctx, string(sessionID), s.now(),
					contextpack.DefaultAntiThrashWindow, contextpack.DefaultAntiThrashMax)
			}
			summary, source := s.summarizeHead(ctx, head, string(sessionID), "", allowLLM)
			if s.contextStore != nil && strings.TrimSpace(summary) != "" {
				id := "compact-" + deterministicID("session-compact", string(sessionID), summary[:min(64, len(summary))], s.now().UTC().Format(time.RFC3339Nano))
				_ = s.contextStore.InsertCompaction(ctx, contextpack.Compaction{
					ID: id, SessionID: string(sessionID), Summary: summary,
					Model: "", CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
				})
				if source == "llm" {
					slog.Info("session compacted",
						"component", "chatsession", "operation", "compact", "result", "succeeded",
						"session_id", sessionID, "source", source)
				} else if !allowLLM {
					slog.Info("session compact anti-thrash; extractive only",
						"component", "chatsession", "operation", "compact", "result", "warning",
						"session_id", sessionID)
				}
			}
			// System role so Pack L3 keeps the summary when dropping old user turns.
			view = append([]providerapi.Message{{
				Role:    providerapi.RoleSystem,
				Content: "[Prior session context — compacted]\n" + summary,
			}}, stripLeadingSystem(tail)...)
			packed = contextpack.Pack(view, contextpack.PackOptions{Budget: budget})
		}
	}
	return packed.Messages, nil
}

// summarizeHead produces a head summary via LLM (optional) or extractive fallback.
// focus is optional guidance for manual /compact; previous summary is merged when present.
func (s *Service) summarizeHead(ctx context.Context, head []providerapi.Message, sessionID, focus string, allowLLM bool) (summary, source string) {
	prevSummary := ""
	if s.contextStore != nil {
		if c, err := s.contextStore.LatestCompaction(ctx, sessionID); err == nil {
			prevSummary = strings.TrimSpace(c.Summary)
		}
	}
	if allowLLM && s.compactor != nil {
		if merger, ok := s.compactor.(interface {
			CompactSummaryWithPrevious(context.Context, []providerapi.Message, string) (string, error)
		}); ok {
			// Inject focus as a synthetic head note for CompactSummaryWithPrevious.
			sumHead := head
			if f := strings.TrimSpace(focus); f != "" {
				sumHead = append([]providerapi.Message{{
					Role:    providerapi.RoleUser,
					Content: "Compaction focus (prioritize in the summary):\n" + f,
				}}, head...)
			}
			sum, err := merger.CompactSummaryWithPrevious(ctx, sumHead, prevSummary)
			if err != nil {
				slog.Warn("chat compaction failed; extractive fallback",
					"component", "chatsession", "operation", "compact", "result", "warning", "error", err)
			} else if strings.TrimSpace(sum) != "" {
				return sum, "llm"
			}
		} else {
			sum, err := s.compactor.CompactSummary(ctx, head)
			if err != nil {
				slog.Warn("chat compaction failed; extractive fallback",
					"component", "chatsession", "operation", "compact", "result", "warning", "error", err)
			} else if strings.TrimSpace(sum) != "" {
				return sum, "llm"
			}
		}
	}
	return contextpack.ExtractiveSummary(head, 4_000), "extractive"
}

// ForceCompact runs a durable session head summary (manual /compact [focus]).
// Bypasses anti-thrash and pressure gates; still respects CompactionEnabled.
func (s *Service) ForceCompact(ctx context.Context, sessionID kernel.SessionID, focus string) (CompactResult, error) {
	if ctx == nil {
		return CompactResult{}, fmt.Errorf("%w: context is required", ErrInvalidRequest)
	}
	sessionID = kernel.SessionID(strings.TrimSpace(string(sessionID)))
	if sessionID == "" {
		return CompactResult{}, fmt.Errorf("%w: session id is required", ErrInvalidRequest)
	}
	if !s.compactionEnabled {
		return CompactResult{SessionID: string(sessionID), Source: "skipped"},
			fmt.Errorf("%w: compaction is disabled", ErrUnavailable)
	}
	if s.contextStore == nil {
		return CompactResult{}, fmt.Errorf("%w: context store unavailable", ErrUnavailable)
	}
	// Verify session exists via transcript load (empty is ok if session row exists later).
	history, err := s.loadTranscriptMessages(ctx, sessionID, "", "")
	if err != nil {
		return CompactResult{}, classify(err)
	}
	if len(history) < 2 {
		return CompactResult{SessionID: string(sessionID), Source: "skipped"},
			fmt.Errorf("%w: not enough history to compact", ErrInvalidRequest)
	}
	head, tail := contextpack.SplitHeadTail(history, 2)
	if len(head) == 0 {
		// Still force a summary of everything except a minimal tail.
		if len(history) > 2 {
			head = history[:len(history)-2]
			tail = history[len(history)-2:]
		} else {
			return CompactResult{SessionID: string(sessionID), Source: "skipped"},
				fmt.Errorf("%w: not enough history to compact", ErrInvalidRequest)
		}
	}
	summary, source := s.summarizeHead(ctx, head, string(sessionID), focus, true)
	if strings.TrimSpace(summary) == "" {
		return CompactResult{SessionID: string(sessionID), Source: "skipped"},
			fmt.Errorf("%w: empty compact summary", ErrUnavailable)
	}
	id := "compact-" + deterministicID("session-compact", string(sessionID), summary[:min(64, len(summary))], s.now().UTC().Format(time.RFC3339Nano), "force", focus)
	if err := s.contextStore.InsertCompaction(ctx, contextpack.Compaction{
		ID: id, SessionID: string(sessionID), Summary: summary,
		Model: "", CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return CompactResult{}, err
	}
	_ = tail // durable only; next loadHistory rebuilds provider view
	slog.Info("session force compacted",
		"component", "chatsession", "operation", "force_compact", "result", "succeeded",
		"session_id", sessionID, "source", source)
	return CompactResult{
		SessionID: string(sessionID), Summary: summary, Source: source, CompactionID: id,
	}, nil
}

// loadTranscriptMessages maps durable transcript to provider messages (no packing).
func (s *Service) loadTranscriptMessages(ctx context.Context, sessionID kernel.SessionID, currentTask kernel.TaskID, currentUser string) ([]providerapi.Message, error) {
	msgs, err := s.transcript.SessionTranscript(ctx, sessionID, corequery.TranscriptOptions{
		Page: corequery.Page{Limit: 500},
	})
	if err != nil {
		return nil, err
	}
	out := make([]providerapi.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg.TaskID == currentTask && msg.Role == "user" && strings.HasPrefix(msg.ID, "task-user:") {
			continue
		}
		if msg.TaskID == currentTask && strings.TrimSpace(msg.Content) == strings.TrimSpace(currentUser) && msg.Role == "user" {
			continue
		}
		switch strings.ToLower(msg.Role) {
		case "user":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			out = append(out, providerapi.Message{Role: providerapi.RoleUser, Content: msg.Content})
		case "assistant":
			m := providerapi.Message{Role: providerapi.RoleAssistant, Content: msg.Content, Thinking: msg.Thinking}
			for _, tc := range msg.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, providerapi.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
			}
			out = append(out, m)
		case "tool":
			out = append(out, providerapi.Message{
				Role: providerapi.RoleTool, Content: msg.Content, ToolCallID: msg.ToolCallID,
			})
		}
	}
	return out, nil
}

func stripLeadingSystem(msgs []providerapi.Message) []providerapi.Message {
	i := 0
	for i < len(msgs) && msgs[i].Role == providerapi.RoleSystem {
		i++
	}
	if i == 0 {
		return msgs
	}
	return msgs[i:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
