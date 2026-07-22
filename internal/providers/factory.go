package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"autozeagent.local/autozeagent/internal/providerconfig"
	"autozeagent.local/autozeagent/internal/providers/anthropic"
	"autozeagent.local/autozeagent/internal/providers/gemini"
	"autozeagent.local/autozeagent/internal/providers/openai"
	"autozeagent.local/autozeagent/internal/providers/openairesponses"
	"autozeagent.local/autozeagent/pkg/providerapi"
)

// NewConfigured builds the protocol adapter selected by the resolved JSON configuration.
func NewConfigured(config providerconfig.Resolved) (providerapi.Provider, error) {
	var apiKeyRef string
	var resolver providerapi.SecretResolver
	if config.APIKey != "" {
		apiKeyRef = "provider:" + config.ProviderID
		resolver = staticSecretResolver{reference: apiKeyRef, value: config.APIKey}
	}
	var provider providerapi.Provider
	var err error
	switch config.Protocol {
	case providerconfig.ProtocolOpenAIChat:
		provider, err = openai.New(openai.Config{
			Name: config.ProviderID, BaseURL: config.BaseURL, APIKeyRef: apiKeyRef, Resolver: resolver,
			CompletionPath: config.CompletionPath, ModelsPath: config.ModelsPath, ResponseFormat: config.ResponseFormat, Headers: config.Headers,
		})
	case providerconfig.ProtocolAnthropicMessages:
		provider, err = anthropic.New(anthropic.Config{
			Name: config.ProviderID, BaseURL: config.BaseURL, APIKeyRef: apiKeyRef, Resolver: resolver,
			MessagesPath: config.CompletionPath, ModelsPath: config.ModelsPath, AnthropicVersion: config.AnthropicVersion, Headers: config.Headers,
		})
	case providerconfig.ProtocolOpenAIResponses:
		provider, err = openairesponses.New(openairesponses.Config{
			Name: config.ProviderID, BaseURL: config.BaseURL, APIKeyRef: apiKeyRef, Resolver: resolver,
			ResponsesPath: config.CompletionPath, ModelsPath: config.ModelsPath, Headers: config.Headers,
		})
	case providerconfig.ProtocolGeminiGenerate:
		provider, err = gemini.New(gemini.Config{
			Name: config.ProviderID, BaseURL: config.BaseURL, APIKeyRef: apiKeyRef, Resolver: resolver,
			GeneratePath: config.CompletionPath, ModelsPath: config.ModelsPath, Headers: config.Headers,
		})
	default:
		return nil, fmt.Errorf("unsupported provider protocol %q", config.Protocol)
	}
	if err != nil {
		return nil, err
	}
	if config.MaxTokens == 0 && config.Temperature == nil && strings.TrimSpace(config.ReasoningEffort) == "" {
		return provider, nil
	}
	return &modelConfiguredProvider{
		Provider: provider, maxTokens: config.MaxTokens,
		temperature: config.Temperature, reasoningEffort: strings.TrimSpace(config.ReasoningEffort),
	}, nil
}

type modelConfiguredProvider struct {
	providerapi.Provider
	maxTokens       int64
	temperature     *float64
	reasoningEffort string
}

func (p *modelConfiguredProvider) Complete(ctx context.Context, request providerapi.CompletionRequest) (providerapi.CompletionResponse, error) {
	return p.Provider.Complete(ctx, p.apply(request))
}

func (p *modelConfiguredProvider) Stream(ctx context.Context, request providerapi.CompletionRequest, handler providerapi.StreamHandler) error {
	return p.Provider.Stream(ctx, p.apply(request), handler)
}

func (p *modelConfiguredProvider) apply(request providerapi.CompletionRequest) providerapi.CompletionRequest {
	if p.maxTokens > 0 && (request.MaxOutputTokens <= 0 || request.MaxOutputTokens > p.maxTokens) {
		request.MaxOutputTokens = p.maxTokens
	}
	if request.Temperature == nil && p.temperature != nil {
		value := *p.temperature
		request.Temperature = &value
	}
	if strings.TrimSpace(request.ReasoningEffort) == "" {
		request.ReasoningEffort = p.reasoningEffort
	}
	return request
}

type staticSecretResolver struct {
	reference string
	value     string
}

func (r staticSecretResolver) ResolveSecret(_ context.Context, reference string) (string, error) {
	if reference == "" || reference != r.reference {
		return "", errors.New("secret reference is unavailable")
	}
	return r.value, nil
}
