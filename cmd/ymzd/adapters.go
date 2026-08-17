package main

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/yyZe0122/yunmengze-agent/internal/chatsession"
	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/gateway"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/internal/skillcatalog"
	"github.com/yyZe0122/yunmengze-agent/internal/skillmaintain"
	"github.com/yyZe0122/yunmengze-agent/internal/tools"
)

func discoverSkillCatalog(layout paths.Layout, workingDirectory string) (*skillcatalog.Catalog, []skillcatalog.Diagnostic) {
	configSource := skillcatalog.SourceUser
	if layout.Mode == paths.ModeSystem {
		configSource = skillcatalog.SourceSystem
	}
	return skillcatalog.Discover([]skillcatalog.Root{
		{Path: filepath.Join(layout.ConfigDir, "skills"), Source: configSource},
		{Path: filepath.Join(workingDirectory, ".yunmengze", "skills"), Source: skillcatalog.SourceProject},
	})
}

// mcpStatusAdapter maps tools.MCPRegistry to gateway.MCPStatusProvider.
type mcpStatusAdapter struct {
	registry *tools.MCPRegistry
}

func (a mcpStatusAdapter) MCPStatus() gateway.MCPStatus {
	st := a.registry.Status()
	return gateway.MCPStatus{
		Enabled: st.Enabled,
		Total:   st.Total,
		OK:      st.OK,
		Error:   st.Error,
		Tools:   st.Tools,
	}
}

// chatCommandsAdapter exposes chat.commands for GET /v1/config/commands (O3).
type chatCommandsAdapter struct {
	cfg providerconfig.ChatConfig
}

func (a chatCommandsAdapter) ChatCommands() []gateway.ChatCommandItem {
	list := a.cfg.CommandList()
	if len(list) == 0 {
		return nil
	}
	out := make([]gateway.ChatCommandItem, 0, len(list))
	for _, item := range list {
		out = append(out, gateway.ChatCommandItem{
			ID: item.ID, Description: item.Description, Template: item.Template,
		})
	}
	return out
}

// memoryControlAdapter maps chatsession memory ops to gateway.MemoryControlService.
type memoryControlAdapter struct {
	chat *chatsession.Service
}

func (a memoryControlAdapter) RefreshMemory(sessionID string) {
	if a.chat == nil {
		return
	}
	a.chat.RefreshMemory(kernel.SessionID(sessionID))
}

func (a memoryControlAdapter) ForgetMemory(ctx context.Context, entryID string) error {
	if a.chat == nil {
		return errors.New("memory is unavailable")
	}
	return a.chat.ForgetMemory(ctx, entryID)
}

func (a memoryControlAdapter) PromoteMemory(ctx context.Context, entryID string) (corequery.MemoryEntry, error) {
	if a.chat == nil {
		return corequery.MemoryEntry{}, errors.New("memory is unavailable")
	}
	e, err := a.chat.PromoteMemory(ctx, entryID)
	if err != nil {
		return corequery.MemoryEntry{}, err
	}
	return corequery.MemoryEntry{
		ID: e.ID, SessionID: e.SessionID, Content: e.Content, Source: e.Source,
		Tags: e.Tags, Kind: e.Kind, Priority: e.Priority, ExpiresAt: e.ExpiresAt,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}, nil
}

type skillControlAdapter struct {
	svc *skillmaintain.Service
}

func (a skillControlAdapter) ApplySkillDraft(ctx context.Context, skillID, actor string) error {
	if a.svc == nil {
		return errors.New("skill control is unavailable")
	}
	_, err := a.svc.ApplyDraft(ctx, skillID, actor)
	return err
}

func (a skillControlAdapter) RejectSkillDraft(ctx context.Context, skillID, actor string) error {
	if a.svc == nil {
		return errors.New("skill control is unavailable")
	}
	return a.svc.RejectDraft(ctx, skillID, actor)
}

func (a skillControlAdapter) SkillUsage(ctx context.Context) (map[string]gateway.SkillUsageView, error) {
	if a.svc == nil {
		return map[string]gateway.SkillUsageView{}, nil
	}
	raw, err := a.svc.UsageMap(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]gateway.SkillUsageView, len(raw))
	for id, u := range raw {
		out[id] = gateway.SkillUsageView{LastUsedAt: u.LastUsedAt, ArchivedAt: u.ArchivedAt}
	}
	return out, nil
}

type sessionPrefsAdapter struct {
	repo *kernel.Repository
}

func (a sessionPrefsAdapter) SetPreferredModel(ctx context.Context, sessionID kernel.SessionID, model string) error {
	if a.repo == nil {
		return errors.New("session repository unavailable")
	}
	return a.repo.SetSessionPreferredModel(ctx, sessionID, model)
}

func (a sessionPrefsAdapter) SetPermissionStance(ctx context.Context, sessionID kernel.SessionID, stance string) error {
	if a.repo == nil {
		return errors.New("session repository unavailable")
	}
	return a.repo.SetSessionPermissionStance(ctx, sessionID, stance)
}
