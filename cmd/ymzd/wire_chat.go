package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yyZe0122/yunmengze-agent/internal/agent"
	"github.com/yyZe0122/yunmengze-agent/internal/chatsession"
	"github.com/yyZe0122/yunmengze-agent/internal/contextpack"
	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/memory"
	"github.com/yyZe0122/yunmengze-agent/internal/modelresolve"
	"github.com/yyZe0122/yunmengze-agent/internal/modelstream"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/internal/providerruntime"
	"github.com/yyZe0122/yunmengze-agent/internal/toolpermission"
)

type chatStack struct {
	queries         *corequery.Store
	providerRuntime *providerruntime.Runtime
	modelHub        *modelstream.Hub
	contextStore    *contextpack.Store
	calibrator      *contextpack.Calibrator
	contextWindow   int64
	chatCfg         providerconfig.ChatConfig
	permService     *toolpermission.Service
	memoryManager   *memory.Manager
	agentRunner     *agent.Runner
	chatService     *chatsession.Service
}

func wireChat(
	stores coreStores,
	stack toolStack,
	layout paths.Layout,
	workingDirectory string,
) (chatStack, error) {
	var out chatStack
	out.chatCfg = stack.chatCfg

	providerRuntime, err := providerruntime.FromConfigDir(layout.ConfigDir)
	if err != nil {
		return out, err
	}
	out.providerRuntime = providerRuntime

	queries, err := corequery.New(stores.database.SQL())
	if err != nil {
		return out, err
	}
	out.queries = queries

	recordStore, err := agent.NewRecordStore(stores.database.SQL())
	if err != nil {
		return out, err
	}
	out.modelHub = modelstream.NewHub()
	contextStore, err := contextpack.NewStore(stores.database.SQL())
	if err != nil {
		return out, err
	}
	out.contextStore = contextStore
	out.calibrator = contextpack.NewCalibrator()

	if providerRuntime != nil && providerRuntime.SelectedRef() != "" {
		if resolved, resolveErr := providerconfig.ResolveModel(layout.ConfigDir, providerRuntime.SelectedRef()); resolveErr == nil && resolved != nil {
			out.contextWindow = resolved.ContextWindow
		}
	}

	maxIterations := out.chatCfg.MaxIterationsOrDefault()
	compactionEnabled := out.chatCfg.CompactionEnabled()
	permissionMode := out.chatCfg.PermissionModeOrDefault()
	stack.broker.SetPermissionMode(permissionMode)

	permStore, err := toolpermission.NewStore(stores.database.SQL())
	if err != nil {
		return out, err
	}
	permService, err := toolpermission.New(toolpermission.Config{
		Events: stores.eventStore,
		DB:     stores.database.SQL(), Store: permStore, Approvals: stores.approvalRepository,
	})
	if err != nil {
		return out, err
	}
	out.permService = permService
	stack.broker.SetPermission(toolpermission.NewGate(permService))

	if out.chatCfg.MemoryEnabled() {
		memoryStore, err := memory.NewStore(stores.database.SQL())
		if err != nil {
			return out, err
		}
		memoryManager, err := memory.New(memory.Config{
			Store: memoryStore, MaxInjectRunes: out.chatCfg.MemoryMaxInjectRunes(),
			DefaultTTL: out.chatCfg.MemoryDefaultTTL(),
		})
		if err != nil {
			return out, err
		}
		if err := memoryManager.Initialize(context.Background()); err != nil {
			return out, fmt.Errorf("memory initialize: %w", err)
		}
		out.memoryManager = memoryManager
		stack.memTools.SetBackend(memoryManager)
	}

	if providerRuntime != nil && providerRuntime.Provider() != nil && strings.TrimSpace(providerRuntime.LoadError()) == "" {
		roleEndpoints, roleErr := providerruntime.BuildRoleEndpoints(layout.ConfigDir, providerRuntime.SelectedRef())
		if roleErr != nil {
			return out, roleErr
		}
		agentRunner, err := agent.New(agent.Config{
			Provider: providerRuntime.Provider(), Broker: stack.broker, Records: recordStore,
			Model: providerRuntime.Model(), Stream: out.modelHub,
			MaxIterations: maxIterations,
			ContextWindow: out.contextWindow, Roles: roleEndpoints,
			Context: contextStore, Calibrator: out.calibrator,
		})
		if err != nil {
			return out, err
		}
		out.agentRunner = agentRunner
		stack.taskTool.SetRunner(agentRunner)
	}

	if out.agentRunner != nil {
		chatRoots := out.chatCfg.PathCeilingRoots(workingDirectory)
		if len(chatRoots) == 0 {
			chatRoots = []string{workingDirectory}
		}
		writeCeiling := out.chatCfg.AgentWriteCeiling()
		allowGit := out.chatCfg.AgentGitEnabled()
		allowProcess := out.chatCfg.AgentProcessEnabled()
		chatCfgCopy := out.chatCfg
		modelResolver := modelresolve.New(layout.ConfigDir)
		chatService, err := chatsession.New(chatsession.Config{
			DB: stores.database.SQL(), Repository: stores.kernelRepository, Approvals: stores.approvalRepository,
			Agent: out.agentRunner, Transcript: queries, WorkspaceRoots: chatRoots,
			PathGuard: stack.pathGuard, DaemonCWD: workingDirectory, ConfigDir: layout.ConfigDir, ChatConfig: &chatCfgCopy,
			AllowWriteCeiling: &writeCeiling, AllowGit: allowGit, AllowProcess: allowProcess,
			PermissionMode: permissionMode, ExtraTools: stack.mcpToolNames,
			ContextWindow: out.contextWindow, Context: contextStore, Compactor: out.agentRunner,
			MemoryCurator: out.agentRunner, CompactionEnabled: &compactionEnabled, Calibrator: out.calibrator,
			Memory: out.memoryManager, Stream: out.modelHub, ToolCalls: stack.broker,
			ModelResolver: modelResolver.AsChatResolver(),
			OnError: func(err error) {
				slog.Error("chat session failure", "component", "chatsession", "operation", "execute", "result", "failed", "error", err)
			},
		})
		if err != nil {
			return out, err
		}
		out.chatService = chatService
		slog.Info("chat workspace configured", "component", "daemon", "operation", "chat_config", "result", "succeeded",
			"ceiling_roots", chatRoots, "allow_all", out.chatCfg.WorkspaceAllowAll(),
			"agent_write_ceiling", writeCeiling, "agent_git", allowGit, "agent_process", allowProcess,
			"context_window", out.contextWindow, "max_iterations", maxIterations, "compaction_enabled", compactionEnabled,
			"permission_mode", permissionMode)
	}
	return out, nil
}
