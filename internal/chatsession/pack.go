package chatsession

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/contextpack"
	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/sessiontodo"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

func (s *Service) buildContextView(
	ctx context.Context,
	sessionID kernel.SessionID,
	currentTask kernel.TaskID,
	currentUser string,
	prefix []providerapi.Message,
	window, maxOut int64,
	model string,
) (contextpack.ContextView, error) {
	history, ids, err := s.loadTranscriptTail(ctx, sessionID, currentTask, currentUser)
	if err != nil {
		return contextpack.ContextView{}, err
	}
	return s.assembleContextView(ctx, sessionID, prefix, history, ids, currentUser, window, maxOut, model)
}

func (s *Service) assembleContextView(
	ctx context.Context,
	sessionID kernel.SessionID,
	prefix, history []providerapi.Message,
	historyIDs []string,
	currentUser string,
	window, maxOut int64,
	model string,
) (contextpack.ContextView, error) {
	maxOut = contextpack.ClampMaxOutput(maxOut)
	usable := contextpack.UsableWindow(window, maxOut, 0)
	budget := contextpack.HistoryBudget(usable)

	prev, throughID := s.latestCompaction(ctx, string(sessionID))
	work := history
	workIDs := historyIDs
	if throughID != "" {
		if cut := indexAfterID(historyIDs, throughID); cut >= 0 && cut <= len(history) {
			work = history[cut:]
			workIDs = historyIDs[cut:]
		}
		// through id slid out of the tail window: keep the whole tail.
		// Do not keep-2 (that drops the middle between through and the last 500).
	} else if prev != "" && len(history) > 4 {
		_, tail := contextpack.SplitHeadTail(history, 2)
		work = stripLeadingSystem(tail)
		workIDs = nil
	}

	est := contextpack.EstimateMessages(work)
	if s.calibrator != nil && strings.TrimSpace(model) != "" {
		est = s.calibrator.Apply(model, est)
	}
	probe := contextpack.Pack(work, contextpack.PackOptions{Budget: budget, Model: model})
	over := probe.OverBudget || (usable > 0 && contextpack.ShouldCompact(est, usable, probe.OverBudget))

	summary := prev
	if s.compactionEnabled && over && len(history) > 4 {
		head, tail := contextpack.SplitHeadTail(history, 2)
		if len(head) > 0 {
			if s.memory != nil {
				s.memory.OnPreCompress(ctx, string(sessionID), head)
			}
			allowLLM := true
			if s.contextStore != nil {
				allowLLM = s.contextStore.AllowLLMCompact(ctx, string(sessionID), s.now(),
					contextpack.DefaultAntiThrashWindow, contextpack.DefaultAntiThrashMax)
			}
			sum, source := s.summarizeHead(ctx, head, string(sessionID), "", allowLLM)
			if strings.TrimSpace(sum) != "" {
				summary = sum
				through := lastHistoryID(historyIDs, head, history)
				s.persistCompaction(ctx, sessionID, sum, through, s.compactionModel(model), source, allowLLM)
				work = stripLeadingSystem(tail)
			}
		}
	}

	ephemeral := make([]providerapi.Message, 0, 2)
	if block := s.todoEphemeral(ctx, sessionID); block != "" {
		ephemeral = append(ephemeral, providerapi.Message{Role: providerapi.RoleSystem, Content: block})
	}
	if strings.TrimSpace(currentUser) != "" {
		ephemeral = append(ephemeral, providerapi.Message{Role: providerapi.RoleUser, Content: currentUser})
	}
	view := contextpack.Build(contextpack.BuildInput{
		Prefix:    prefix,
		History:   work,
		Ephemeral: ephemeral,
		Summary:   summary,
	}, contextpack.BuildOptions{Budget: budget, Model: model})
	_ = workIDs
	return view, nil
}

// packSessionHistory remains for tests: packs prior turns only (no Prefix/Ephemeral).
func (s *Service) packSessionHistory(ctx context.Context, sessionID kernel.SessionID, history []providerapi.Message) ([]providerapi.Message, error) {
	view, err := s.assembleContextView(ctx, sessionID, nil, history, nil, "", s.contextWindow, s.maxOutputTokens, "")
	if err != nil {
		return nil, err
	}
	out := append(append([]providerapi.Message(nil), view.Summary...), view.Tail...)
	if len(out) == 0 {
		return history, nil
	}
	return out, nil
}

func (s *Service) todoEphemeral(ctx context.Context, sessionID kernel.SessionID) string {
	if s == nil || s.todos == nil || strings.TrimSpace(string(sessionID)) == "" {
		return ""
	}
	items, err := s.todos.List(ctx, string(sessionID))
	if err != nil || len(items) == 0 {
		return ""
	}
	return sessiontodo.CompactBlock(items)
}

func (s *Service) latestCompaction(ctx context.Context, sessionID string) (summary, throughID string) {
	if s.contextStore == nil {
		return "", ""
	}
	c, err := s.contextStore.LatestCompaction(ctx, sessionID)
	if err != nil || strings.TrimSpace(c.Summary) == "" {
		return "", ""
	}
	return strings.TrimSpace(c.Summary), strings.TrimSpace(c.ThroughMessageID)
}

func (s *Service) persistCompaction(ctx context.Context, sessionID kernel.SessionID, summary, through, model, source string, allowLLM bool) {
	if s.contextStore == nil || strings.TrimSpace(summary) == "" {
		return
	}
	id := "compact-" + deterministicID("session-compact", string(sessionID), summary[:min(64, len(summary))], s.now().UTC().Format(time.RFC3339Nano))
	_ = s.contextStore.InsertCompaction(ctx, contextpack.Compaction{
		ID: id, SessionID: string(sessionID), Summary: summary,
		ThroughMessageID: through, Model: model,
		CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	})
	if source == "llm" {
		slog.Info("session compacted",
			"component", "chatsession", "operation", "compact", "result", "succeeded",
			"session_id", sessionID, "source", source, "model", model)
	} else if !allowLLM {
		slog.Info("session compact anti-thrash; extractive only",
			"component", "chatsession", "operation", "compact", "result", "warning",
			"session_id", sessionID)
	}
}

func indexAfterID(ids []string, through string) int {
	if through == "" || len(ids) == 0 {
		return -1
	}
	for i, id := range ids {
		if id == through {
			return i + 1
		}
	}
	return -1
}

func lastHistoryID(ids []string, head, full []providerapi.Message) string {
	if len(ids) == 0 || len(head) == 0 || len(ids) != len(full) {
		return ""
	}
	last := head[len(head)-1]
	for i := range full {
		if sameTranscriptPoint(full[i], last) {
			if i < len(ids) {
				return ids[i]
			}
			break
		}
	}
	idx := len(head) - 1
	if idx >= 0 && idx < len(ids) {
		return ids[idx]
	}
	return ""
}

func sameTranscriptPoint(a, b providerapi.Message) bool {
	if a.Role != b.Role {
		return false
	}
	if a.ToolCallID != "" || b.ToolCallID != "" {
		return a.ToolCallID == b.ToolCallID
	}
	return a.Content == b.Content
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
	history, ids, err := s.loadTranscriptTail(ctx, sessionID, "", "")
	if err != nil {
		return CompactResult{}, classify(err)
	}
	if len(history) < 2 {
		return CompactResult{SessionID: string(sessionID), Source: "skipped"},
			fmt.Errorf("%w: not enough history to compact", ErrInvalidRequest)
	}
	head, tail := contextpack.SplitHeadTail(history, 2)
	if len(head) == 0 {
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
	through := lastHistoryID(ids, head, history)
	id := "compact-" + deterministicID("session-compact", string(sessionID), summary[:min(64, len(summary))], s.now().UTC().Format(time.RFC3339Nano), "force", focus)
	if err := s.contextStore.InsertCompaction(ctx, contextpack.Compaction{
		ID: id, SessionID: string(sessionID), Summary: summary,
		ThroughMessageID: through, Model: s.compactionModel(""),
		CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return CompactResult{}, err
	}
	_ = tail
	slog.Info("session force compacted",
		"component", "chatsession", "operation", "force_compact", "result", "succeeded",
		"session_id", sessionID, "source", source)
	return CompactResult{
		SessionID: string(sessionID), Summary: summary, Source: source, CompactionID: id,
	}, nil
}

func (s *Service) loadTranscriptTail(ctx context.Context, sessionID kernel.SessionID, currentTask kernel.TaskID, currentUser string) ([]providerapi.Message, []string, error) {
	msgs, err := s.transcript.SessionTranscriptTail(ctx, sessionID, 500)
	if err != nil {
		return nil, nil, err
	}
	out := make([]providerapi.Message, 0, len(msgs))
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		if msg.TaskID == currentTask && msg.Role == "user" && strings.HasPrefix(msg.ID, "task-user:") {
			continue
		}
		if msg.TaskID == currentTask && strings.TrimSpace(msg.Content) == strings.TrimSpace(currentUser) && msg.Role == "user" {
			continue
		}
		mapped, ok := mapTranscript(msg)
		if !ok {
			continue
		}
		out = append(out, mapped)
		ids = append(ids, msg.ID)
	}
	return out, ids, nil
}

func mapTranscript(msg corequery.TranscriptMessage) (providerapi.Message, bool) {
	switch strings.ToLower(msg.Role) {
	case "user":
		if strings.TrimSpace(msg.Content) == "" {
			return providerapi.Message{}, false
		}
		return providerapi.Message{Role: providerapi.RoleUser, Content: msg.Content}, true
	case "assistant":
		m := providerapi.Message{Role: providerapi.RoleAssistant, Content: msg.Content, Thinking: msg.Thinking}
		for _, tc := range msg.ToolCalls {
			m.ToolCalls = append(m.ToolCalls, providerapi.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
		}
		return m, true
	case "tool":
		return providerapi.Message{
			Role: providerapi.RoleTool, Content: msg.Content, ToolCallID: msg.ToolCallID,
		}, true
	default:
		return providerapi.Message{}, false
	}
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
