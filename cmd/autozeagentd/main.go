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

	"autozeagent.local/autozeagent/internal/agent"
	"autozeagent.local/autozeagent/internal/agentautostart"
	"autozeagent.local/autozeagent/internal/app"
	"autozeagent.local/autozeagent/internal/applicationerror"
	"autozeagent.local/autozeagent/internal/approval"
	"autozeagent.local/autozeagent/internal/approvalsubmission"
	"autozeagent.local/autozeagent/internal/artifacts"
	"autozeagent.local/autozeagent/internal/chatsession"
	"autozeagent.local/autozeagent/internal/corequery"
	"autozeagent.local/autozeagent/internal/daemonctl"
	"autozeagent.local/autozeagent/internal/events"
	"autozeagent.local/autozeagent/internal/gateway"
	"autozeagent.local/autozeagent/internal/kernel"
	"autozeagent.local/autozeagent/internal/modelstream"
	"autozeagent.local/autozeagent/internal/planner"
	"autozeagent.local/autozeagent/internal/planningrecovery"
	"autozeagent.local/autozeagent/internal/platform/paths"
	platformsignals "autozeagent.local/autozeagent/internal/platform/signals"
	"autozeagent.local/autozeagent/internal/policy"
	"autozeagent.local/autozeagent/internal/providerconfig"
	"autozeagent.local/autozeagent/internal/providers"
	"autozeagent.local/autozeagent/internal/runexecution"
	"autozeagent.local/autozeagent/internal/scheduledtasks"
	"autozeagent.local/autozeagent/internal/scheduler"
	"autozeagent.local/autozeagent/internal/skillcatalog"
	coresqlite "autozeagent.local/autozeagent/internal/store/sqlite"
	"autozeagent.local/autozeagent/internal/tasksubmission"
	"autozeagent.local/autozeagent/internal/tools"
	"autozeagent.local/autozeagent/internal/version"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(os.Args[1:]); err != nil {
		slog.Error("daemon stopped", "component", "daemon", "operation", "run", "result", "failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("autozeagentd", flag.ContinueOnError)
	modeValue := flags.String("mode", string(paths.ModeUser), "runtime mode: user or system")
	logLevelValue := flags.String("log-level", defaultLogLevel(), "log level: debug, info, warn, or error")
	check := flags.Bool("check", false, "validate core bootstrap and print status")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Printf("autozeagentd %s commit=%s date=%s\n", version.Version, version.Commit, version.Date)
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
	if err := tools.RegisterBuiltins(toolBroker, []string{workingDirectory}, tools.ExecutorConfig{
		MaxOutputBytes: 4 << 20,
		AllowedEnv:     []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "USERPROFILE"},
	}); err != nil {
		return err
	}
	allowedCapabilities, err := plannerCapabilities(toolBroker)
	if err != nil {
		return err
	}
	migrateFrom := []string{workingDirectory, layout.DataDir}
	if clientCWD := strings.TrimSpace(os.Getenv("AUTOZEAGENT_CLIENT_CWD")); clientCWD != "" {
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
	providerRuntime, err := providerRuntimeFromConfig(layout.ConfigDir, kernelRepository, allowedCapabilities)
	if err != nil {
		return err
	}
	var planningService *planner.Service
	if providerRuntime != nil {
		planningService = providerRuntime.planning
	}
	queries, err := corequery.New(database.SQL())
	if err != nil {
		return err
	}
	approvalDecisionService, err := approvalsubmission.New(approvalsubmission.Config{
		Plans: queries, Repository: approvalRepository,
	})
	if err != nil {
		return err
	}
	recordStore, err := agent.NewRecordStore(database.SQL())
	if err != nil {
		return err
	}
	modelHub := modelstream.NewHub()
	var agentRunner *agent.Runner
	if providerRuntime != nil {
		agentRunner, err = agent.New(agent.Config{
			Provider: providerRuntime.provider, Broker: toolBroker, Records: recordStore,
			Model: providerRuntime.model, Stream: modelHub,
		})
		if err != nil {
			return err
		}
	}
	var runService *runexecution.Service
	if agentRunner != nil {
		runService, err = runexecution.New(runexecution.Config{
			DB: database.SQL(), Plans: queries, Approvals: approvalRepository, Repository: kernelRepository, Agent: agentRunner,
			OnError: func(err error) {
				slog.Error("run recovery failure", "component", "run", "operation", "recover", "result", "failed", "error", err)
			},
		})
		if err != nil {
			return err
		}
	}
	// plan mode: after plan lands, human approves (agentautostart only for plan recovery paths).
	// agent mode chat skips Planner via chatsession.
	var planningSurface agentautostart.Planner
	if planningService != nil {
		// Keep auto-start wrapper for any residual agent-mode plans; new agent submits use chat.
		if runService != nil {
			planningSurface = agentautostart.Wrap(planningService, approvalDecisionService, runService)
		} else {
			planningSurface = planningService
		}
	}
	var chatService *chatsession.Service
	if agentRunner != nil {
		chatService, err = chatsession.New(chatsession.Config{
			DB: database.SQL(), Repository: kernelRepository, Approvals: approvalRepository,
			Agent: agentRunner, Transcript: queries, WorkspaceRoots: []string{workingDirectory},
			AllowWrite: true,
			OnError: func(err error) {
				slog.Error("chat session failure", "component", "chatsession", "operation", "execute", "result", "failed", "error", err)
			},
		})
		if err != nil {
			return err
		}
	}
	taskSubmissionConfig := tasksubmission.Config{Repository: kernelRepository, Skills: skillCatalog}
	if planningSurface != nil {
		taskSubmissionConfig.Planner = planningSurface
	}
	if chatService != nil {
		taskSubmissionConfig.Chat = chatsession.AsTaskChat(chatService)
	}
	taskSubmissionService, err := tasksubmission.New(taskSubmissionConfig)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}
	schedulerStore, err := scheduler.NewStore(database.SQL())
	if err != nil {
		return err
	}
	schedulerRunner, err := scheduledtasks.New(scheduledtasks.Config{
		Client: schedulerStore, Submissions: taskSubmissionService,
		Owner: fmt.Sprintf("autozeagentd/%s/%d", hostname, os.Getpid()),
		OnError: func(err error) {
			slog.Error("scheduler failure", "component", "scheduler", "operation", "run", "result", "failed", "error", err)
		},
	})
	if err != nil {
		return err
	}
	backgroundRunners := []app.BackgroundRunner{schedulerRunner}
	if runService != nil {
		backgroundRunners = append(backgroundRunners, runService)
	}
	if planningSurface != nil {
		planningRecoveryRunner, err := planningrecovery.New(planningrecovery.Config{
			Repository: kernelRepository,
			Planner:    planningSurface,
			OnError: func(err error) {
				slog.Error("planning recovery failure", "component", "planner", "operation", "recover", "result", "failed", "error", err)
			},
		})
		if err != nil {
			return err
		}
		backgroundRunners = append(backgroundRunners, planningRecoveryRunner)
	}
	core, err := app.New(app.Config{
		Name:              "autozeagentd",
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

	var runStarter gateway.RunStarter
	if runService != nil {
		runStarter = runService
	}
	modelConfig, err := gatewayModelConfig(layout.ConfigDir, providerRuntime)
	if err != nil {
		return err
	}
	var modelSwitcher gateway.ModelSwitcher
	if providerRuntime != nil {
		providerRuntime.agent = agentRunner
		modelSwitcher = providerRuntime
	}
	gatewayAPI, err := gateway.NewAPI(gateway.APIConfig{
		Queries: queries, TaskSubmissions: taskSubmissionService,
		ApprovalDecisions: approvalDecisionService, RunStarts: runStarter, TaskControls: runService, Jobs: schedulerStore,
		Core: core, Events: eventStore, Skills: skillCatalog, ModelConfig: modelConfig, ModelSwitcher: modelSwitcher,
		ModelStream: modelHub,
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
		{Path: filepath.Join(workingDirectory, ".autozeagent", "skills"), Source: skillcatalog.SourceProject},
	})
}

type configuredProviderRuntime struct {
	mu                  sync.Mutex
	configDir           string
	provider            providerapi.Provider
	model               string
	selectedRef         string
	planning            *planner.Service
	agent               *agent.Runner
	allowedCapabilities map[string]policy.RiskLevel
}

func plannerCapabilities(broker *tools.Broker) (map[string]policy.RiskLevel, error) {
	capabilities := make(map[string]policy.RiskLevel)
	for _, definition := range broker.Definitions() {
		risk := policy.RiskLevel(definition.Risk)
		if strings.TrimSpace(definition.Name) == "" || !risk.Valid() {
			return nil, fmt.Errorf("tool %q has invalid planner risk %q", definition.Name, definition.Risk)
		}
		capabilities[definition.Name] = risk
	}
	if len(capabilities) == 0 {
		return nil, errors.New("planner requires at least one registered tool")
	}
	return capabilities, nil
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
	return gateway.ModelConfig{Model: selected, Models: models}, nil
}

func providerRuntimeFromConfig(configDir string, repository planner.TaskRepository, capabilities map[string]policy.RiskLevel) (*configuredProviderRuntime, error) {
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
	plannerEngine, err := planner.New(planner.Config{Provider: provider, Model: configured.ModelID, AllowedCapabilities: capabilities})
	if err != nil {
		return nil, fmt.Errorf("configure planner: %w", err)
	}
	service, err := planner.NewService(repository, plannerEngine)
	if err != nil {
		return nil, fmt.Errorf("configure planning service: %w", err)
	}
	selected := configured.ProviderID + "/" + configured.ModelID
	return &configuredProviderRuntime{
		configDir: configDir,
		provider:  provider, model: configured.ModelID, selectedRef: selected,
		planning: service, allowedCapabilities: capabilities,
	}, nil
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
	if r.planning != nil {
		if plannerEngine := r.planning.Planner(); plannerEngine != nil {
			if err := plannerEngine.SetProvider(provider); err != nil {
				return gateway.ModelConfig{}, err
			}
			if err := plannerEngine.SetModel(resolved.ModelID); err != nil {
				return gateway.ModelConfig{}, err
			}
		}
	}
	if r.agent != nil {
		if err := r.agent.SetProvider(provider); err != nil {
			return gateway.ModelConfig{}, err
		}
		if err := r.agent.SetModel(resolved.ModelID); err != nil {
			return gateway.ModelConfig{}, err
		}
	}
	r.provider = provider
	r.model = resolved.ModelID
	r.selectedRef = resolved.ProviderID + "/" + resolved.ModelID
	slog.Info("model switched", "component", "daemon", "operation", "select_model", "result", "succeeded",
		"model", r.selectedRef, "config_path", writtenPath)
	return gatewayModelConfig(r.configDir, r)
}
