package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yyZe0122/yunmengze-agent/internal/agent"
	"github.com/yyZe0122/yunmengze-agent/internal/app"
	"github.com/yyZe0122/yunmengze-agent/internal/applicationerror"
	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/artifacts"
	"github.com/yyZe0122/yunmengze-agent/internal/chatsession"
	"github.com/yyZe0122/yunmengze-agent/internal/contextpack"
	"github.com/yyZe0122/yunmengze-agent/internal/corequery"
	"github.com/yyZe0122/yunmengze-agent/internal/daemonctl"
	"github.com/yyZe0122/yunmengze-agent/internal/events"
	"github.com/yyZe0122/yunmengze-agent/internal/gateway"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	"github.com/yyZe0122/yunmengze-agent/internal/memory"
	"github.com/yyZe0122/yunmengze-agent/internal/modelstream"
	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
	platformsignals "github.com/yyZe0122/yunmengze-agent/internal/platform/signals"
	"github.com/yyZe0122/yunmengze-agent/internal/policy"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/internal/providers"
	"github.com/yyZe0122/yunmengze-agent/internal/scheduledtasks"
	"github.com/yyZe0122/yunmengze-agent/internal/scheduler"
	"github.com/yyZe0122/yunmengze-agent/internal/skillcatalog"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/internal/taskcontrol"
	"github.com/yyZe0122/yunmengze-agent/internal/tasksubmission"
	"github.com/yyZe0122/yunmengze-agent/internal/toolpermission"
	"github.com/yyZe0122/yunmengze-agent/internal/tools"
	"github.com/yyZe0122/yunmengze-agent/internal/version"
	"github.com/yyZe0122/yunmengze-agent/pkg/providerapi"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(os.Args[1:]); err != nil {
		slog.Error("daemon stopped", "component", "daemon", "operation", "run", "result", "failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("ymzd", flag.ContinueOnError)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	logLevelValue := flags.String("log-level", defaultLogLevel(), "log level: debug, info, warn, or error")
	check := flags.Bool("check", false, "validate core bootstrap and print status")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Printf("ymzd %s commit=%s date=%s\n", version.Version, version.Commit, version.Date)
		return nil
	}

	mode, err := paths.ParseMode(*modeValue)
	if err != nil {
		return err
	}
	layout, err := paths.Resolve(mode)
	if err != nil {
		return err
	}
	logLevel, err := parseLogLevel(*logLevelValue)
	if err != nil {
		return err
	}
	if _, err := configureLogging(layout.LogDir, logLevel, string(mode)); err != nil {
		return err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	skillCatalog, skillDiagnostics := discoverSkillCatalog(layout, workingDirectory)
	for _, diagnostic := range skillDiagnostics {
		slog.Warn("skill discovery diagnostic", "component", "skills", "operation", "discover", "result", "warning", "error", diagnostic)
	}

	database, err := coresqlite.Open(context.Background(), filepath.Join(layout.DataDir, "core.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	eventStore, err := events.NewStore(database.SQL())
	if err != nil {
		return err
	}
	kernelRepository, err := kernel.NewRepository(database.SQL())
	if err != nil {
		return err
	}
	approvalRepository, err := approval.NewRepository(database.SQL())
	if err != nil {
		return err
	}
	artifactStore, err := artifacts.NewStore(database.SQL(), filepath.Join(layout.DataDir, "artifacts"))
	if err != nil {
		return err
	}
	toolBroker, err := tools.NewBroker(tools.Config{
		DB: database.SQL(), Approvals: approvalRepository,
		Policy: policy.NewEvaluator(policy.DefaultConfig()), Artifacts: artifactStore,
	})
	if err != nil {
		return err
	}
	migrateFrom := []string{workingDirectory, layout.DataDir}
	if clientCWD := strings.TrimSpace(os.Getenv("YMZ_CLIENT_CWD")); clientCWD != "" {
		migrateFrom = append([]string{clientCWD}, migrateFrom...)
	}
	ensureResult, err := providerconfig.EnsureConfig(layout.ConfigDir, migrateFrom...)
	if err != nil {
		return fmt.Errorf("ensure provider config: %w", err)
	}
	switch {
	case ensureResult.Migrated:
		slog.Info("provider config migrated", "component", "daemon", "operation", "ensure_config", "result", "succeeded",
			"config_path", ensureResult.Path, "source", ensureResult.Source)
	case ensureResult.Created:
		slog.Info("provider config template created", "component", "daemon", "operation", "ensure_config", "result", "succeeded",
			"config_path", ensureResult.Path)
	default:
		slog.Info("provider config ready", "component", "daemon", "operation", "ensure_config", "result", "succeeded",
			"config_path", ensureResult.Path)
	}
	// PathGuard ceiling from chat.workspace / chat.roots (ADR-046).
	chatCfgForTools, err := providerconfig.LoadChat(layout.ConfigDir)
	if err != nil {
		return fmt.Errorf("load chat config: %w", err)
	}
	pathRoots := chatCfgForTools.PathCeilingRoots(workingDirectory)
	if len(pathRoots) == 0 && !chatCfgForTools.WorkspaceAllowAll() {
		pathRoots = []string{workingDirectory}
	}
	pathGuard, err := tools.RegisterBuiltinsWithOptions(toolBroker, pathRoots, chatCfgForTools.WorkspaceAllowAll(), tools.ExecutorConfig{
		MaxOutputBytes: 4 << 20,
		AllowedEnv:     []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "USERPROFILE"},
	})
	if err != nil {
		return err
	}
	if chatCfgForTools.WorkspaceAllowAll() {
		slog.Warn("chat.workspace.allow_all enabled: path containment disabled",
			"component", "daemon", "operation", "path_guard", "result", "warning")
	}
	taskTool, err := tools.RegisterTaskTool(toolBroker, database.SQL(), nil)
	if err != nil {
		return err
	}
	// Memory tools registered early; backend attached after memory.Manager is created.
	memTools, err := tools.RegisterMemoryTools(toolBroker, nil)
	if err != nil {
		return err
	}
	mcpConfig, err := providerconfig.LoadMCP(layout.ConfigDir)
	if err != nil {
		return fmt.Errorf("load mcp config: %w", err)
	}
	mcpRegistry, mcpToolNames, err := tools.RegisterMCP(context.Background(), toolBroker, mcpConfig)
	if err != nil {
		return err
	}
	if mcpRegistry != nil {
		defer mcpRegistry.Close()
	}
	if len(mcpToolNames) > 0 {
		slog.Info("mcp tools ready", "component", "daemon", "operation", "mcp_register", "result", "succeeded",
			"tools", len(mcpToolNames))
	}
	providerRuntime, err := providerRuntimeFromConfig(layout.ConfigDir)
	if err != nil {
		return err
	}
	queries, err := corequery.New(database.SQL())
	if err != nil {
		return err
	}
	recordStore, err := agent.NewRecordStore(database.SQL())
	if err != nil {
		return err
	}
	modelHub := modelstream.NewHub()
	contextStore, err := contextpack.NewStore(database.SQL())
	if err != nil {
		return err
	}
	tokenCalibrator := contextpack.NewCalibrator()
	var contextWindow int64
	if providerRuntime != nil {
		if resolved, resolveErr := providerconfig.ResolveModel(layout.ConfigDir, providerRuntime.selectedRef); resolveErr == nil && resolved != nil {
			contextWindow = resolved.ContextWindow
		}
	}
	chatCfg, chatErr := providerconfig.LoadChat(layout.ConfigDir)
	if chatErr != nil {
		return fmt.Errorf("load chat config: %w", chatErr)
	}
	maxIterations := chatCfg.MaxIterationsOrDefault()
	compactionEnabled := chatCfg.CompactionEnabled()
	permissionMode := chatCfg.PermissionModeOrDefault()
	toolBroker.SetPermissionMode(permissionMode)

	// Tool-call permission service (ADR-043); gate attached for ask mode waits.
	permStore, err := toolpermission.NewStore(database.SQL())
	if err != nil {
		return err
	}
	permService, err := toolpermission.New(toolpermission.Config{
		DB: database.SQL(), Store: permStore, Approvals: approvalRepository,
	})
	if err != nil {
		return err
	}
	toolBroker.SetPermission(toolpermission.NewGate(permService))

	// In-process memory (ADR-044 productization).
	var memoryManager *memory.Manager
	if chatCfg.MemoryEnabled() {
		memoryStore, err := memory.NewStore(database.SQL())
		if err != nil {
			return err
		}
		memoryManager, err = memory.New(memory.Config{
			Store: memoryStore, MaxInjectRunes: chatCfg.MemoryMaxInjectRunes(),
		})
		if err != nil {
			return err
		}
		if err := memoryManager.Initialize(context.Background()); err != nil {
			return fmt.Errorf("memory initialize: %w", err)
		}
		defer func() { _ = memoryManager.Shutdown(context.Background()) }()
		memTools.SetBackend(memoryManager)
	}

	var agentRunner *agent.Runner
	if providerRuntime != nil {
		roleEndpoints, roleErr := buildRoleEndpoints(layout.ConfigDir, providerRuntime.selectedRef)
		if roleErr != nil {
			return roleErr
		}
		agentRunner, err = agent.New(agent.Config{
			Provider: providerRuntime.provider, Broker: toolBroker, Records: recordStore,
			Model: providerRuntime.model, Stream: modelHub,
			MaxIterations: maxIterations,
			ContextWindow: contextWindow, Roles: roleEndpoints,
			Context: contextStore, Calibrator: tokenCalibrator,
		})
		if err != nil {
			return err
		}
		taskTool.SetRunner(agentRunner)
	}
	// Chat first so ControlTask can interrupt in-flight chat runs (pause/cancel).
	var chatService *chatsession.Service
	if agentRunner != nil {
		chatRoots := chatCfg.PathCeilingRoots(workingDirectory)
		if len(chatRoots) == 0 {
			chatRoots = []string{workingDirectory}
		}
		writeCeiling := chatCfg.AgentWriteCeiling()
		allowGit := chatCfg.AgentGitEnabled()
		allowProcess := chatCfg.AgentProcessEnabled()
		chatCfgCopy := chatCfg
		chatService, err = chatsession.New(chatsession.Config{
			DB: database.SQL(), Repository: kernelRepository, Approvals: approvalRepository,
			Agent: agentRunner, Transcript: queries, WorkspaceRoots: chatRoots,
			PathGuard: pathGuard, DaemonCWD: workingDirectory, ChatConfig: &chatCfgCopy,
			AllowWriteCeiling: &writeCeiling, AllowGit: allowGit, AllowProcess: allowProcess,
			PermissionMode: permissionMode, ExtraTools: mcpToolNames,
			ContextWindow: contextWindow, Context: contextStore, Compactor: agentRunner,
			CompactionEnabled: &compactionEnabled, Calibrator: tokenCalibrator,
			Memory: memoryManager, Stream: modelHub, ToolCalls: toolBroker,
			OnError: func(err error) {
				slog.Error("chat session failure", "component", "chatsession", "operation", "execute", "result", "failed", "error", err)
			},
		})
		if err != nil {
			return err
		}
		slog.Info("chat workspace configured", "component", "daemon", "operation", "chat_config", "result", "succeeded",
			"ceiling_roots", chatRoots, "allow_all", chatCfg.WorkspaceAllowAll(),
			"agent_write_ceiling", writeCeiling, "agent_git", allowGit, "agent_process", allowProcess,
			"context_window", contextWindow, "max_iterations", maxIterations, "compaction_enabled", compactionEnabled,
			"permission_mode", permissionMode)
	}
	// Task control (pause/resume/cancel); chat interrupt when chat is configured.
	// Assign chat only when non-nil so ChatInterrupter is a true nil interface (not typed nil).
	var chatInterrupt taskcontrol.ChatInterrupter
	if chatService != nil {
		chatInterrupt = chatService
	}
	taskControl, err := taskcontrol.New(taskcontrol.Config{
		DB: database.SQL(), Approvals: approvalRepository, Repository: kernelRepository, Chat: chatInterrupt,
	})
	if err != nil {
		return err
	}
	taskSubmissionConfig := tasksubmission.Config{Repository: kernelRepository, Skills: skillCatalog}
	if chatService != nil {
		taskSubmissionConfig.Chat = chatsession.AsTaskChat(chatService)
	}
	taskSubmissionService, err := tasksubmission.New(taskSubmissionConfig)
	if err != nil {
		return err
	}
	schedulerStore, err := scheduler.NewStore(database.SQL())
	if err != nil {
		return err
	}
	var backgroundRunners []app.BackgroundRunner
	jobRunner, err := scheduledtasks.New(scheduledtasks.Config{
		Client:      schedulerStore,
		Submissions: taskSubmissionService,
		Owner:       "ymzd",
		OnError: func(err error) {
			slog.Error("scheduled job runner failure", "component", "scheduledtasks", "operation", "poll", "result", "failed", "error", err)
		},
	})
	if err != nil {
		return err
	}
	backgroundRunners = append(backgroundRunners, jobRunner)
	core, err := app.New(app.Config{
		Name:              "ymzd",
		Version:           version.Version,
		Runtime:           layout,
		BackgroundRunners: backgroundRunners,
	})
	if err != nil {
		return err
	}
	if *check {
		if err := queries.Check(context.Background()); err != nil {
			return fmt.Errorf("check core database: %w", err)
		}
		if err := schedulerStore.Ping(context.Background()); err != nil {
			return fmt.Errorf("check scheduler store: %w", err)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(core.Status()); err != nil {
			return err
		}
		slog.Info("daemon check completed", "component", "daemon", "operation", "check", "result", "succeeded")
		return nil
	}

	modelConfig, err := gatewayModelConfig(layout.ConfigDir, providerRuntime)
	if err != nil {
		return err
	}
	var modelSwitcher gateway.ModelSwitcher
	if providerRuntime != nil {
		providerRuntime.agent = agentRunner
		providerRuntime.chat = chatService
		modelSwitcher = providerRuntime
	}
	var mcpStatus gateway.MCPStatusProvider
	if mcpRegistry != nil {
		mcpStatus = mcpStatusAdapter{registry: mcpRegistry}
	}
	var sessionCompact gateway.SessionCompactor
	if chatService != nil {
		sessionCompact = gateway.SessionCompactFunc(func(ctx context.Context, sessionID kernel.SessionID, focus string) (gateway.SessionCompactResult, error) {
			r, err := chatService.ForceCompact(ctx, sessionID, focus)
			if err != nil {
				return gateway.SessionCompactResult{}, err
			}
			return gateway.SessionCompactResult{
				SessionID: r.SessionID, Summary: r.Summary, Source: r.Source, CompactionID: r.CompactionID,
			}, nil
		})
	}
	var memoryControl gateway.MemoryControlService
	if chatService != nil && memoryManager != nil {
		memoryControl = memoryControlAdapter{chat: chatService}
	}
	gatewayAPI, err := gateway.NewAPI(gateway.APIConfig{
		Queries: queries, TaskSubmissions: taskSubmissionService,
		TaskControls: taskControl, Jobs: schedulerStore,
		Core: core, Events: eventStore, Skills: skillCatalog, ModelConfig: modelConfig, ModelSwitcher: modelSwitcher,
		ModelStream: modelHub, MCP: mcpStatus, SessionCompact: sessionCompact,
		ToolPermissions: gateway.ToolPermissionAdapter{
			Service:   permService,
			TrustPath: toolpermission.DefaultTrustPath(layout.ConfigDir),
		},
		MemoryControl: memoryControl,
	})
	if err != nil {
		return err
	}
	gatewayRunner, err := gateway.NewLocalRunner(gateway.LocalRunnerConfig{
		RuntimeDir: layout.RuntimeDir,
		Handler:    gatewayAPI,
		OnError: func(err error) {
			slog.Error("gateway failure", "component", "gateway", "operation", "serve", "result", "failed", "error", err)
		},
	})
	if err != nil {
		return err
	}
	defer gatewayRunner.Close()
	if err := daemonctl.WritePID(layout.RuntimeDir); err != nil {
		return err
	}
	defer func() {
		if err := daemonctl.RemovePID(layout.RuntimeDir); err != nil {
			slog.Warn("remove daemon pid file", "component", "daemon", "operation", "stop", "result", "warning", "error", err)
		}
	}()
	if err := core.AddBackgroundRunner(gatewayRunner); err != nil {
		return err
	}

	status := core.Status()
	slog.Info("daemon started", "component", "daemon", "operation", "start", "result", "succeeded", "version", status.Version, "mode", status.Runtime.Mode, "workspace_root", workingDirectory)
	ctx, cancel := platformsignals.NotifyContext(context.Background())
	defer cancel()
	if err := core.Run(ctx); err != nil {
		return err
	}
	slog.Info("daemon stopped", "component", "daemon", "operation", "run", "result", "succeeded")
	return nil
}

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

type configuredProviderRuntime struct {
	mu          sync.Mutex
	configDir   string
	provider    providerapi.Provider
	model       string
	selectedRef string
	agent       *agent.Runner
	chat        *chatsession.Service
}

func gatewayModelConfig(configDir string, runtime *configuredProviderRuntime) (gateway.ModelConfig, error) {
	selected, refs, err := providerconfig.ListModelRefs(configDir)
	if err != nil {
		return gateway.ModelConfig{}, err
	}
	if runtime != nil && strings.TrimSpace(runtime.selectedRef) != "" {
		selected = runtime.selectedRef
	} else if configured, loadErr := providerconfig.Load(configDir); loadErr == nil && configured != nil {
		selected = configured.ProviderID + "/" + configured.ModelID
	} else if runtime != nil && strings.TrimSpace(runtime.model) != "" {
		selected = runtime.model
	}
	models := make([]string, 0, len(refs)+1)
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	add(selected)
	for _, ref := range refs {
		add(ref.ID)
	}
	cfg := gateway.ModelConfig{Model: selected, Models: models}
	if selected != "" {
		if resolved, resolveErr := providerconfig.ResolveModel(configDir, selected); resolveErr == nil && resolved != nil {
			cfg.ContextWindow = resolved.ContextWindow
		}
	}
	return cfg, nil
}

func providerRuntimeFromConfig(configDir string) (*configuredProviderRuntime, error) {
	configured, err := providerconfig.Load(configDir)
	if err != nil {
		// Incomplete template (e.g. missing {env:…}) must not block gateway; model switch stays unavailable.
		slog.Warn("provider config not loaded", "component", "daemon", "operation", "load_config", "result", "warning",
			"config_dir", configDir, "error", err)
		return nil, nil
	}
	if configured == nil {
		return nil, nil
	}
	provider, err := providers.NewConfigured(*configured)
	if err != nil {
		return nil, fmt.Errorf("configure provider: %w", err)
	}
	selected := configured.ProviderID + "/" + configured.ModelID
	return &configuredProviderRuntime{
		configDir: configDir,
		provider:  provider, model: configured.ModelID, selectedRef: selected,
	}, nil
}

// buildRoleEndpoints resolves optional models.subagent / models.compact (ADR-045).
// Unset roles are omitted so agent falls back to main.
func buildRoleEndpoints(configDir, mainRef string) (map[string]agent.RoleEndpoint, error) {
	_, roles, err := providerconfig.LoadModelRoles(configDir)
	if err != nil {
		return nil, fmt.Errorf("load model roles: %w", err)
	}
	if len(roles) == 0 {
		return nil, nil
	}
	// Cache adapters by full provider/model ref.
	cache := map[string]agent.RoleEndpoint{}
	out := make(map[string]agent.RoleEndpoint, len(roles))
	for role, ref := range roles {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == mainRef {
			// Same as main: no separate endpoint needed (fallback is main).
			continue
		}
		if ep, ok := cache[ref]; ok {
			out[role] = ep
			continue
		}
		resolved, err := providerconfig.ResolveModel(configDir, ref)
		if err != nil {
			return nil, fmt.Errorf("resolve models.%s: %w", role, err)
		}
		provider, err := providers.NewConfigured(*resolved)
		if err != nil {
			return nil, fmt.Errorf("configure models.%s: %w", role, err)
		}
		ep := agent.RoleEndpoint{
			Provider: provider, Model: resolved.ModelID, ContextWindow: resolved.ContextWindow,
		}
		cache[ref] = ep
		out[role] = ep
		slog.Info("model role configured", "component", "daemon", "operation", "model_roles", "result", "succeeded",
			"role", role, "model", ref, "context_window", resolved.ContextWindow)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (r *configuredProviderRuntime) SelectModel(_ context.Context, ref string) (gateway.ModelConfig, error) {
	if r == nil {
		return gateway.ModelConfig{}, applicationerror.Wrap(applicationerror.CodeUnavailable, false, errors.New("provider runtime is not configured"))
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return gateway.ModelConfig{}, errors.New("model is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ref == r.selectedRef {
		return gatewayModelConfig(r.configDir, r)
	}
	resolved, err := providerconfig.ResolveModel(r.configDir, ref)
	if err != nil {
		return gateway.ModelConfig{}, err
	}
	provider, err := providers.NewConfigured(*resolved)
	if err != nil {
		return gateway.ModelConfig{}, err
	}
	writtenPath, err := providerconfig.WriteSelectedModel(r.configDir, ref)
	if err != nil {
		return gateway.ModelConfig{}, err
	}
	if r.agent != nil {
		if err := r.agent.SetProvider(provider); err != nil {
			return gateway.ModelConfig{}, err
		}
		if err := r.agent.SetModel(resolved.ModelID); err != nil {
			return gateway.ModelConfig{}, err
		}
		r.agent.SetContextWindow(resolved.ContextWindow)
	}
	if r.chat != nil {
		r.chat.SetContextWindow(resolved.ContextWindow)
	}
	r.provider = provider
	r.model = resolved.ModelID
	r.selectedRef = resolved.ProviderID + "/" + resolved.ModelID
	slog.Info("model switched", "component", "daemon", "operation", "select_model", "result", "succeeded",
		"model", r.selectedRef, "context_window", resolved.ContextWindow, "config_path", writtenPath)
	return gatewayModelConfig(r.configDir, r)
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
