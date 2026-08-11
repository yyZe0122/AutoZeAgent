package chatsession

import (
	"context"
	"fmt"

	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/memory"
)

func (s *Service) RefreshMemory(sessionID kernel.SessionID) {
	if s == nil || s.memory == nil {
		return
	}
	s.memory.InvalidateSnapshot(string(sessionID))
}

// ForgetMemory deletes one memory entry by id (TUI/gateway write path via Manager).
func (s *Service) ForgetMemory(ctx context.Context, entryID string) error {
	if s == nil || s.memory == nil {
		return fmt.Errorf("memory is unavailable")
	}
	return s.memory.Forget(ctx, entryID)
}

// PromoteMemory promotes a session entry to user-global curated.
func (s *Service) PromoteMemory(ctx context.Context, entryID string) (memory.Entry, error) {
	if s == nil || s.memory == nil {
		return memory.Entry{}, fmt.Errorf("memory is unavailable")
	}
	return s.memory.Promote(ctx, entryID)
}
