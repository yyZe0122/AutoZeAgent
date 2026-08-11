// Package providerconfig loads the selected model and provider from AutoZeAgent JSON configuration.
package providerconfig

import (
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
	Schema string `json:"$schema,omitempty"`
	Model  string `json:"model"`
	// Models maps optional role → provider/model (ADR-045). Unset role falls back to Model.
	// Allowed keys: subagent, compact. Unknown keys are rejected.
	Models   map[string]string   `json:"models,omitempty"`
	Provider map[string]Provider `json:"provider"`
	// Chat configures agent-mode session chat workspace grants (optional).
	Chat *ChatConfig `json:"chat,omitempty"`
	// MCP configures stdio MCP servers (optional; ADR-040).
	MCP *MCPConfig `json:"mcp,omitempty"`
}

// Model role names for optional models map (ADR-045).
const (
	RoleMain     = "main"
	RoleSubagent = "subagent"
	RoleCompact  = "compact"
)

// AllowedModelRoles are keys permitted under top-level "models" (excluding main).
var AllowedModelRoles = map[string]struct{}{
	RoleSubagent: {},
	RoleCompact:  {},
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

// Permission modes for chat.permission.mode (ADR-043).
const (
	PermissionModePreauth = "preauth"
	PermissionModeAsk     = "ask"
	PermissionModeAuto    = "auto"
)

// ChatConfig is the optional session chat workspace section of autozeagent.json.
// Tab agent (build) vs plan (read-only) selects tools; this block sets roots,
// agent write ceiling, and optional packing/loop limits (ADR-041).
type ChatConfig struct {
	// Roots are absolute workspace paths for default chat tool grants (legacy; still valid).
	// Empty with no workspace.allow → client_cwd / daemon fallback (ADR-046).
	Roots []string `json:"roots,omitempty"`
	// Workspace controls default session root and path ceiling (ADR-046).
	Workspace *ChatWorkspaceConfig `json:"workspace,omitempty"`
	// AllowWrite is a ceiling for agent (build) mode write tools.
	// nil / omitted → true (agent may write). false → agent is also read-only.
	// Plan mode is always read-only regardless of this field.
	AllowWrite *bool `json:"allow_write,omitempty"`
	// Tools opt-in high-risk agent tools (default all off). Plan mode never receives these.
	Tools *ChatToolsConfig `json:"tools,omitempty"`
	// Compaction controls session head summarization (ADR-041). Omit → enabled.
	Compaction *ChatCompactionConfig `json:"compaction,omitempty"`
	// MaxIterations caps agent tool-loop iterations per chat run (1–64). Omit → 16.
	MaxIterations int `json:"max_iterations,omitempty"`
	// Permission controls tool-call interactive approval (ADR-043). Omit → preauth.
	Permission *ChatPermissionConfig `json:"permission,omitempty"`
	// Memory controls in-process layered memory (ADR-044). Omit → enabled defaults.
	Memory *ChatMemoryConfig `json:"memory,omitempty"`
}

// Workspace default modes (chat.workspace.default).
const (
	WorkspaceDefaultClientCWD = "client_cwd"
	WorkspaceDefaultDaemonCWD = "daemon_cwd"
)

// ChatWorkspaceConfig is optional chat.workspace (ADR-046).
type ChatWorkspaceConfig struct {
	// Default is client_cwd (omit) | daemon_cwd | absolute path.
	Default string `json:"default,omitempty"`
	// Allow are extra absolute roots always on the path ceiling.
	Allow []string `json:"allow,omitempty"`
	// AllowAll disables path-root containment (local single-user only).
	AllowAll bool `json:"allow_all,omitempty"`
}

// ChatMemoryConfig is the optional chat.memory object (ADR-044 productization).
type ChatMemoryConfig struct {
	// Enabled defaults true when Memory is nil or Enabled is nil.
	Enabled *bool `json:"enabled,omitempty"`
	// MaxInjectRunes caps frozen system memory block (default 2000).
	MaxInjectRunes int `json:"max_inject_runes,omitempty"`
	// InjectMode is session_start (frozen snapshot). Other values reserved.
	InjectMode string `json:"inject_mode,omitempty"`
	// SessionSearch enables transcript FTS tool (default true).
	SessionSearch *bool `json:"session_search,omitempty"`
}

// ChatPermissionConfig is the optional chat.permission object.
type ChatPermissionConfig struct {
	// Mode is preauth (default) | ask | auto (reserved; treated as preauth).
	Mode string `json:"mode,omitempty"`
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

// MaxIterationsOrDefault returns MaxIterations or 16 when unset/zero.
func (c ChatConfig) MaxIterationsOrDefault() int {
	if c.MaxIterations == 0 {
		return 16
	}
	return c.MaxIterations
}

// MemoryEnabled reports whether in-process memory is on (default true).
func (c ChatConfig) MemoryEnabled() bool {
	if c.Memory == nil || c.Memory.Enabled == nil {
		return true
	}
	return *c.Memory.Enabled
}

// MemoryMaxInjectRunes returns inject cap or 2000.
func (c ChatConfig) MemoryMaxInjectRunes() int {
	if c.Memory == nil || c.Memory.MaxInjectRunes <= 0 {
		return 2000
	}
	return c.Memory.MaxInjectRunes
}

// MemorySessionSearchEnabled reports whether session_search is on (default true).
func (c ChatConfig) MemorySessionSearchEnabled() bool {
	if c.Memory == nil || c.Memory.SessionSearch == nil {
		return true
	}
	return *c.Memory.SessionSearch
}

// PermissionModeOrDefault returns chat.permission.mode or preauth.
// Unknown / auto currently resolve to preauth until interactive auto is defined.
func (c ChatConfig) PermissionModeOrDefault() string {
	if c.Permission == nil {
		return PermissionModePreauth
	}
	switch strings.ToLower(strings.TrimSpace(c.Permission.Mode)) {
	case "", PermissionModePreauth:
		return PermissionModePreauth
	case PermissionModeAsk:
		return PermissionModeAsk
	case PermissionModeAuto:
		// Reserved: fail closed like preauth for now.
		return PermissionModePreauth
	default:
		return PermissionModePreauth
	}
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
