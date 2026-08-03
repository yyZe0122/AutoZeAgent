// Package providerconfig loads the selected model and provider from AutoZeAgent JSON configuration.
package providerconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	Filename      = "autozeagent.json"
	LocalFilename = "autozeagent.local.json"

	ProtocolOpenAIChat        = "openai-chat"
	ProtocolOpenAIResponses   = "openai-responses"
	ProtocolAnthropicMessages = "anthropic-messages"
	ProtocolGeminiGenerate    = "gemini-generate-content"
)

type File struct {
	Schema   string              `json:"$schema,omitempty"`
	Model    string              `json:"model"`
	Provider map[string]Provider `json:"provider"`
	// Chat configures agent-mode session chat workspace grants (optional).
	Chat *ChatConfig `json:"chat,omitempty"`
	// MCP configures stdio MCP servers (optional; ADR-040).
	MCP *MCPConfig `json:"mcp,omitempty"`
}

// MCPConfig is the optional MCP servers section of autozeagent.json.
type MCPConfig struct {
	Servers map[string]MCPServer `json:"servers,omitempty"`
}

// MCPServer is one stdio MCP server process.
type MCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ChatConfig is the optional session chat workspace section of autozeagent.json.
// Tab agent (build) vs plan (read-only) selects tools; this block sets roots,
// agent write ceiling, and optional packing/loop limits (ADR-041).
type ChatConfig struct {
	// Roots are absolute workspace paths for default chat tool grants.
	// Empty → daemon uses its working directory.
	Roots []string `json:"roots,omitempty"`
	// AllowWrite is a ceiling for agent (build) mode write tools.
	// nil / omitted → true (agent may write). false → agent is also read-only.
	// Plan mode is always read-only regardless of this field.
	AllowWrite *bool `json:"allow_write,omitempty"`
	// Tools opt-in high-risk agent tools (default all off). Plan mode never receives these.
	Tools *ChatToolsConfig `json:"tools,omitempty"`
	// Compaction controls session head summarization (ADR-041). Omit → enabled.
	Compaction *ChatCompactionConfig `json:"compaction,omitempty"`
	// MaxIterations caps agent tool-loop iterations per chat run (1–64). Omit → 8.
	MaxIterations int `json:"max_iterations,omitempty"`
}

// ChatToolsConfig is the optional chat.tools allowlist for agent mode only.
type ChatToolsConfig struct {
	// Git enables git_status / git_diff / git_add / git_commit path-scoped grants (default false).
	Git bool `json:"git,omitempty"`
	// Process enables process_exec path-scoped grants (default false).
	Process bool `json:"process,omitempty"`
}

// ChatCompactionConfig is the optional chat.compaction object.
type ChatCompactionConfig struct {
	// Enabled defaults true when Compaction is nil or Enabled is nil.
	Enabled *bool `json:"enabled,omitempty"`
}

// AgentWriteCeiling reports whether agent mode may receive write grants.
func (c ChatConfig) AgentWriteCeiling() bool {
	if c.AllowWrite == nil {
		return true
	}
	return *c.AllowWrite
}

// AgentGitEnabled reports whether agent mode may receive git_* grants (default false).
func (c ChatConfig) AgentGitEnabled() bool {
	return c.Tools != nil && c.Tools.Git
}

// AgentProcessEnabled reports whether agent mode may receive process_exec grants (default false).
func (c ChatConfig) AgentProcessEnabled() bool {
	return c.Tools != nil && c.Tools.Process
}

// CompactionEnabled reports whether LLM/extractive session compaction is on.
// Default true when chat.compaction is omitted or enabled is omitted.
func (c ChatConfig) CompactionEnabled() bool {
	if c.Compaction == nil || c.Compaction.Enabled == nil {
		return true
	}
	return *c.Compaction.Enabled
}

// MaxIterationsOrDefault returns MaxIterations or 8 when unset/zero.
func (c ChatConfig) MaxIterationsOrDefault() int {
	if c.MaxIterations == 0 {
		return 8
	}
	return c.MaxIterations
}

type Provider struct {
	Protocol string           `json:"protocol,omitempty"`
	Type     string           `json:"type,omitempty"`
	Options  ProviderOptions  `json:"options"`
	Models   map[string]Model `json:"models,omitempty"`
}

type ProviderOptions struct {
	BaseURL          string            `json:"baseURL"`
	APIKey           string            `json:"apiKey,omitempty"`
	CompletionPath   string            `json:"completionPath,omitempty"`
	ModelsPath       string            `json:"modelsPath,omitempty"`
	ResponseFormat   string            `json:"responseFormat,omitempty"`
	AnthropicVersion string            `json:"anthropicVersion,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
}

type Model struct {
	Name           string   `json:"name,omitempty"`
	ResponseFormat string   `json:"responseFormat,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	MaxTokens      int64    `json:"maxTokens,omitempty"`
	// ContextWindow is the model context length in tokens (not maxTokens output cap).
	// Omit or 0 when unknown.
	ContextWindow   int64  `json:"contextWindow,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

type Resolved struct {
	Source           string
	ProviderID       string
	Protocol         string
	ModelID          string
	BaseURL          string
	APIKey           string
	CompletionPath   string
	ModelsPath       string
	ResponseFormat   string
	Temperature      *float64
	MaxTokens        int64
	ContextWindow    int64
	ReasoningEffort  string
	AnthropicVersion string
	Headers          map[string]string
}

// ModelRef is a configured model identifier in provider/model form (no secrets).
type ModelRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ListModelRefs returns the selected model plus every provider/model key from the
// first resolvable file under configDir. API keys and other secrets are never included.
func ListModelRefs(configDir string) (selected string, models []ModelRef, err error) {
	path, err := findConfigPath(configDir)
	if err != nil {
		return "", nil, err
	}
	if path == "" {
		return "", nil, nil
	}
	return listModelRefsFromFile(path)
}

func listModelRefsFromFile(path string) (string, []ModelRef, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("open provider config %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var config File
	if err := decoder.Decode(&config); err != nil {
		return "", nil, fmt.Errorf("decode provider config %s: %w", path, err)
	}
	if err := requireEOF(decoder); err != nil {
		return "", nil, fmt.Errorf("decode provider config %s: %w", path, err)
	}
	selected := strings.TrimSpace(config.Model)
	seen := make(map[string]struct{})
	var models []ModelRef
	add := func(id, name string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		models = append(models, ModelRef{ID: id, Name: strings.TrimSpace(name)})
	}
	if selected != "" {
		add(selected, "")
	}
	providerIDs := make([]string, 0, len(config.Provider))
	for providerID := range config.Provider {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	for _, providerID := range providerIDs {
		provider := config.Provider[providerID]
		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			ref := providerID + "/" + modelID
			add(ref, provider.Models[modelID].Name)
		}
	}
	return selected, models, nil
}

// Load reads provider configuration only from configDir
// (autozeagent.local.json, then autozeagent.json). Project directories are not searched.
func Load(configDir string) (*Resolved, error) {
	path, err := findConfigPath(configDir)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	resolved, err := loadFile(path)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// LoadChat reads the optional chat section from the first resolvable config file.
// Missing file or missing chat block returns a zero ChatConfig (empty roots; agent write allowed).
// Secrets are not resolved; only structural validation runs.
func LoadChat(configDir string) (ChatConfig, error) {
	path, err := findConfigPath(configDir)
	if err != nil {
		return ChatConfig{}, err
	}
	if path == "" {
		return ChatConfig{}, nil
	}
	file, err := decodeConfigFile(path)
	if err != nil {
		return ChatConfig{}, err
	}
	if file.Chat == nil {
		return ChatConfig{}, nil
	}
	chat := *file.Chat
	if err := chat.validate(); err != nil {
		return ChatConfig{}, err
	}
	return chat, nil
}

// LoadMCP reads the optional mcp section. Missing file or mcp block returns zero config.
// Env values may use {env:VAR} / {file:...} and are resolved relative to the config directory.
func LoadMCP(configDir string) (MCPConfig, error) {
	path, err := findConfigPath(configDir)
	if err != nil {
		return MCPConfig{}, err
	}
	if path == "" {
		return MCPConfig{}, nil
	}
	file, err := decodeConfigFile(path)
	if err != nil {
		return MCPConfig{}, err
	}
	if file.MCP == nil {
		return MCPConfig{}, nil
	}
	mcp := *file.MCP
	if err := mcp.validate(); err != nil {
		return MCPConfig{}, err
	}
	baseDir := filepath.Dir(path)
	for name, server := range mcp.Servers {
		env := make(map[string]string, len(server.Env))
		for k, v := range server.Env {
			resolved, err := resolveValue(v, baseDir)
			if err != nil {
				return MCPConfig{}, fmt.Errorf("mcp server %q env %q: %w", name, k, err)
			}
			env[k] = resolved
		}
		server.Env = env
		mcp.Servers[name] = server
	}
	return mcp, nil
}

func (c MCPConfig) validate() error {
	for name, server := range c.Servers {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("mcp server name is required")
		}
		if !ValidMCPServerName(name) {
			return fmt.Errorf("mcp server name %q must match [a-zA-Z0-9_-]+", name)
		}
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("mcp server %q: command is required", name)
		}
	}
	return nil
}

// ValidMCPServerName matches tool name safe fragment for mcp_<server>_*.
func ValidMCPServerName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// EffectiveRoots returns configured absolute roots, or fallback when roots are empty.
func (c ChatConfig) EffectiveRoots(fallback string) []string {
	var out []string
	for _, root := range c.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		out = append(out, root)
	}
	if len(out) > 0 {
		return out
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return nil
	}
	return []string{fallback}
}

func (c ChatConfig) validate() error {
	for i, root := range c.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return fmt.Errorf("chat.roots[%d] must be an absolute path", i)
		}
	}
	if c.MaxIterations != 0 && (c.MaxIterations < 1 || c.MaxIterations > 64) {
		return fmt.Errorf("chat.max_iterations must be between 1 and 64 (or omit for default 8)")
	}
	return nil
}

// ResolveModel loads configuration and resolves provider/model for the given ref.
// ref must be provider/model. The selected top-level model field is overridden.
func ResolveModel(configDir, ref string) (*Resolved, error) {
	ref = strings.TrimSpace(ref)
	providerID, modelID, ok := strings.Cut(ref, "/")
	providerID, modelID = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	if !ok || providerID == "" || modelID == "" {
		return nil, errors.New("model must use provider/model format")
	}
	path, err := findConfigPath(configDir)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("provider config not found")
	}
	resolved, err := loadFileWithModel(path, providerID, modelID)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// WriteSelectedModel updates the top-level model field in the first resolvable
// config file under configDir. Other fields and secrets are preserved.
func WriteSelectedModel(configDir, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	providerID, modelID, ok := strings.Cut(ref, "/")
	providerID, modelID = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	if !ok || providerID == "" || modelID == "" {
		return "", errors.New("model must use provider/model format")
	}
	selected := providerID + "/" + modelID
	path, err := findConfigPath(configDir)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("provider config not found")
	}
	if err := validateModelInFile(path, providerID, modelID); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read provider config %s: %w", path, err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", fmt.Errorf("decode provider config %s: %w", path, err)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	document["model"] = encoded
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", err
	}
	updated = append(updated, '\n')
	info, err := os.Stat(path)
	mode := os.FileMode(0o600)
	if err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".autozeagent-model-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(updated); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("replace provider config %s: %w", path, err)
	}
	cleanup = false
	return path, nil
}

func findConfigPath(configDir string) (string, error) {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return "", errors.New("config directory is required")
	}
	candidates := []string{
		filepath.Join(configDir, LocalFilename),
		filepath.Join(configDir, Filename),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect provider config %s: %w", candidate, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("provider config is not a regular file: %s", candidate)
		}
		return candidate, nil
	}
	return "", nil
}

// EnsureResult describes how provider config was made available under configDir.
type EnsureResult struct {
	Path     string // active config path under configDir (may be empty only on error)
	Created  bool   // wrote the default template
	Migrated bool   // copied from a project/legacy path
	Source   string // migration source path when Migrated
}

// EnsureConfig prepares configDir for provider loading:
//  1. MkdirAll configDir
//  2. If ConfigDir already has a config file, use it
//  3. Else copy the first existing file from migrateFromDirs (each dir checked for
//     autozeagent.local.json then autozeagent.json) into ConfigDir as LocalFilename
//  4. Else write a default template (env-based keys, no secrets) as Filename
func EnsureConfig(configDir string, migrateFromDirs ...string) (EnsureResult, error) {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return EnsureResult{}, errors.New("config directory is required")
	}
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return EnsureResult{}, fmt.Errorf("create config directory %s: %w", configDir, err)
	}
	existing, err := findConfigPath(configDir)
	if err != nil {
		return EnsureResult{}, err
	}
	if existing != "" {
		return EnsureResult{Path: existing}, nil
	}
	for _, dir := range migrateFromDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, name := range []string{LocalFilename, Filename} {
			src := filepath.Join(dir, name)
			info, statErr := os.Stat(src)
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				return EnsureResult{}, fmt.Errorf("inspect migrate source %s: %w", src, statErr)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			dst := filepath.Join(configDir, LocalFilename)
			if err := copyFile(src, dst, 0o600); err != nil {
				return EnsureResult{}, fmt.Errorf("migrate provider config %s → %s: %w", src, dst, err)
			}
			return EnsureResult{Path: dst, Migrated: true, Source: src}, nil
		}
	}
	dst := filepath.Join(configDir, Filename)
	if err := os.WriteFile(dst, []byte(defaultConfigTemplate), 0o600); err != nil {
		return EnsureResult{}, fmt.Errorf("write default provider config %s: %w", dst, err)
	}
	return EnsureResult{Path: dst, Created: true}, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".autozeagent-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// defaultConfigTemplate is written when ConfigDir has no config and nothing to migrate.
// Keys use {env:…}; no literal secrets.
const defaultConfigTemplate = `{
  "model": "deepseek/deepseek-chat",
  "provider": {
    "deepseek": {
      "type": "openai-compatible",
      "options": {
        "baseURL": "https://api.deepseek.com",
        "apiKey": "{env:DEEPSEEK_API_KEY}"
      },
      "models": {
        "deepseek-chat": {
          "name": "DeepSeek Chat"
        }
      }
    }
  }
}
`

func loadFile(path string) (Resolved, error) {
	file, err := os.Open(path)
	if err != nil {
		return Resolved{}, fmt.Errorf("open provider config %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var config File
	if err := decoder.Decode(&config); err != nil {
		return Resolved{}, fmt.Errorf("decode provider config %s: %w", path, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Resolved{}, fmt.Errorf("decode provider config %s: %w", path, err)
	}
	providerID, modelID, ok := strings.Cut(strings.TrimSpace(config.Model), "/")
	providerID, modelID = strings.TrimSpace(providerID), strings.TrimSpace(modelID)
	if !ok || providerID == "" || modelID == "" {
		return Resolved{}, errors.New("provider config model must use provider/model format")
	}
	return resolveFromFile(path, config, providerID, modelID)
}

func loadFileWithModel(path, providerID, modelID string) (Resolved, error) {
	config, err := decodeConfigFile(path)
	if err != nil {
		return Resolved{}, err
	}
	return resolveFromFile(path, config, providerID, modelID)
}

func validateModelInFile(path, providerID, modelID string) error {
	config, err := decodeConfigFile(path)
	if err != nil {
		return err
	}
	provider, ok := config.Provider[providerID]
	if !ok {
		return fmt.Errorf("selected provider %q is not configured", providerID)
	}
	if len(provider.Models) > 0 {
		if _, modelConfigured := provider.Models[modelID]; !modelConfigured {
			return fmt.Errorf("model %q is not configured for provider %q", modelID, providerID)
		}
	}
	return nil
}

func decodeConfigFile(path string) (File, error) {
	file, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("open provider config %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var config File
	if err := decoder.Decode(&config); err != nil {
		return File{}, fmt.Errorf("decode provider config %s: %w", path, err)
	}
	if err := requireEOF(decoder); err != nil {
		return File{}, fmt.Errorf("decode provider config %s: %w", path, err)
	}
	return config, nil
}

func resolveFromFile(path string, config File, providerID, modelID string) (Resolved, error) {
	if providerID == "" || modelID == "" {
		return Resolved{}, errors.New("provider config model must use provider/model format")
	}
	provider, ok := config.Provider[providerID]
	if !ok {
		return Resolved{}, fmt.Errorf("selected provider %q is not configured", providerID)
	}
	protocol, err := resolveProtocol(provider.Protocol, provider.Type)
	if err != nil {
		return Resolved{}, fmt.Errorf("provider %q: %w", providerID, err)
	}
	modelConfig, modelConfigured := provider.Models[modelID]
	if len(provider.Models) > 0 && !modelConfigured {
		return Resolved{}, fmt.Errorf("model %q is not configured for provider %q", modelID, providerID)
	}
	baseURL := strings.TrimSpace(provider.Options.BaseURL)
	if baseURL == "" {
		return Resolved{}, fmt.Errorf("provider %q baseURL is required", providerID)
	}
	apiKey, err := resolveValue(provider.Options.APIKey, filepath.Dir(path))
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve provider %q apiKey: %w", providerID, err)
	}
	responseFormat := strings.TrimSpace(modelConfig.ResponseFormat)
	if responseFormat == "" {
		responseFormat = strings.TrimSpace(provider.Options.ResponseFormat)
	}
	if protocol != ProtocolOpenAIChat && responseFormat != "" {
		return Resolved{}, fmt.Errorf("provider %q responseFormat is only supported by openai-chat", providerID)
	}
	if modelConfig.Temperature != nil && (*modelConfig.Temperature < 0 || *modelConfig.Temperature > 2) {
		return Resolved{}, fmt.Errorf("model %q temperature must be between 0 and 2", modelID)
	}
	if modelConfig.MaxTokens < 0 {
		return Resolved{}, fmt.Errorf("model %q maxTokens must not be negative", modelID)
	}
	if modelConfig.ContextWindow < 0 {
		return Resolved{}, fmt.Errorf("model %q contextWindow must not be negative", modelID)
	}
	reasoningEffort := strings.TrimSpace(modelConfig.ReasoningEffort)
	if reasoningEffort != "" && protocol != ProtocolOpenAIChat && protocol != ProtocolOpenAIResponses {
		return Resolved{}, fmt.Errorf("model %q reasoningEffort is only supported by OpenAI-compatible protocols", modelID)
	}
	headers, err := resolveValues(provider.Options.Headers, filepath.Dir(path))
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve provider %q headers: %w", providerID, err)
	}
	return Resolved{
		Source: path, ProviderID: providerID, Protocol: protocol, ModelID: modelID,
		BaseURL: baseURL, APIKey: apiKey,
		CompletionPath:  strings.TrimSpace(provider.Options.CompletionPath),
		ModelsPath:      strings.TrimSpace(provider.Options.ModelsPath),
		ResponseFormat:  responseFormat,
		Temperature:     modelConfig.Temperature,
		MaxTokens:       modelConfig.MaxTokens,
		ContextWindow:   modelConfig.ContextWindow,
		ReasoningEffort: reasoningEffort,

		AnthropicVersion: strings.TrimSpace(provider.Options.AnthropicVersion),
		Headers:          headers,
	}, nil
}

func resolveProtocol(protocol, providerType string) (string, error) {
	protocol = strings.TrimSpace(protocol)
	providerType = strings.TrimSpace(providerType)
	if protocol == "" && providerType == "" {
		return ProtocolOpenAIChat, nil
	}
	resolvedProtocol, err := normalizeProtocol(protocol)
	if err != nil && protocol != "" {
		return "", err
	}
	resolvedType, err := normalizeProtocol(providerType)
	if err != nil && providerType != "" {
		return "", fmt.Errorf("unsupported type %q", providerType)
	}
	if resolvedProtocol != "" && resolvedType != "" && resolvedProtocol != resolvedType {
		return "", fmt.Errorf("protocol %q conflicts with type %q", protocol, providerType)
	}
	if resolvedProtocol != "" {
		return resolvedProtocol, nil
	}
	return resolvedType, nil
}

func normalizeProtocol(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case ProtocolOpenAIChat, "openai-chat-completions", "chat-completions", "openai-compatible", "openai-compat", "ollama", "lmstudio", "llamacpp", "vllm", "litellm":
		return ProtocolOpenAIChat, nil
	case ProtocolOpenAIResponses, "openai", "responses":
		return ProtocolOpenAIResponses, nil
	case ProtocolAnthropicMessages, "anthropic", "anthropic-compatible", "claude":
		return ProtocolAnthropicMessages, nil
	case ProtocolGeminiGenerate, "gemini", "google", "google-generative-ai":
		return ProtocolGeminiGenerate, nil
	default:
		return "", fmt.Errorf("unsupported protocol %q", value)
	}
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("configuration contains multiple JSON values")
		}
		return err
	}
	return nil
}

func resolveValue(value, configDirectory string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if inner, ok := placeholder(value, "env"); ok {
		resolved, exists := os.LookupEnv(inner)
		if !exists || strings.TrimSpace(resolved) == "" {
			return "", fmt.Errorf("environment variable %s is unavailable", inner)
		}
		return strings.TrimSpace(resolved), nil
	}
	if inner, ok := placeholder(value, "file"); ok {
		path := inner
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDirectory, path)
		}
		content, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return "", err
		}
		resolved := strings.TrimSpace(string(content))
		if resolved == "" {
			return "", errors.New("referenced file is empty")
		}
		return resolved, nil
	}
	return value, nil
}

func resolveValues(values map[string]string, configDirectory string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(values))
	for name, value := range values {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("header name is required")
		}
		resolvedValue, err := resolveValue(value, configDirectory)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", name, err)
		}
		resolved[name] = resolvedValue
	}
	return resolved, nil
}

func placeholder(value, kind string) (string, bool) {
	prefix := "{" + kind + ":"
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, "}") {
		return "", false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, prefix), "}"))
	return inner, inner != ""
}
