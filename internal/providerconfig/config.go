// Package providerconfig loads the selected model and provider from AutoZeAgent JSON configuration.
package providerconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Name            string   `json:"name,omitempty"`
	ResponseFormat  string   `json:"responseFormat,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxTokens       int64    `json:"maxTokens,omitempty"`
	ReasoningEffort string   `json:"reasoningEffort,omitempty"`
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
	ReasoningEffort  string
	AnthropicVersion string
	Headers          map[string]string
}

// Load uses project-local configuration first, then project configuration, then user configuration.
func Load(configDir, workingDirectory string) (*Resolved, error) {
	candidates := []string{
		filepath.Join(workingDirectory, LocalFilename),
		filepath.Join(workingDirectory, Filename),
		filepath.Join(configDir, Filename),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect provider config %s: %w", candidate, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("provider config is not a regular file: %s", candidate)
		}
		resolved, err := loadFile(candidate)
		if err != nil {
			return nil, err
		}
		return &resolved, nil
	}
	return nil, nil
}

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
