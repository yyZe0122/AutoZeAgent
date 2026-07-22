package providers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"autozeagent.local/autozeagent/pkg/providerapi"
)

type Entry struct {
	Priority int
	Provider providerapi.Provider
}

type Router struct {
	providers []providerapi.Provider
}

func NewRouter(entries []Entry) (*Router, error) {
	if len(entries) == 0 {
		return nil, errors.New("at least one provider is required")
	}
	ordered := append([]Entry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	providers := make([]providerapi.Provider, 0, len(ordered))
	names := make(map[string]struct{}, len(ordered))
	for _, entry := range ordered {
		if entry.Provider == nil || strings.TrimSpace(entry.Provider.Name()) == "" {
			return nil, errors.New("provider and provider name are required")
		}
		if _, exists := names[entry.Provider.Name()]; exists {
			return nil, fmt.Errorf("duplicate provider name %q", entry.Provider.Name())
		}
		names[entry.Provider.Name()] = struct{}{}
		providers = append(providers, entry.Provider)
	}
	return &Router{providers: providers}, nil
}

func (r *Router) Name() string { return "provider-router" }

func (r *Router) Complete(ctx context.Context, request providerapi.CompletionRequest) (providerapi.CompletionResponse, error) {
	var lastErr error
	for _, provider := range r.providers {
		response, err := provider.Complete(ctx, request)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !providerapi.IsRetryable(err) || ctx.Err() != nil {
			return providerapi.CompletionResponse{}, err
		}
	}
	return providerapi.CompletionResponse{}, lastErr
}

func (r *Router) Stream(ctx context.Context, request providerapi.CompletionRequest, handler providerapi.StreamHandler) error {
	if handler == nil {
		return errors.New("stream handler is required")
	}
	var lastErr error
	for _, provider := range r.providers {
		emitted := false
		err := provider.Stream(ctx, request, func(event providerapi.StreamEvent) error {
			emitted = true
			return handler(event)
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if emitted || !providerapi.IsRetryable(err) || ctx.Err() != nil {
			return err
		}
	}
	return lastErr
}

func (r *Router) Health(ctx context.Context) providerapi.HealthStatus {
	started := time.Now()
	for _, provider := range r.providers {
		status := provider.Health(ctx)
		if status.Healthy {
			return providerapi.HealthStatus{Healthy: true, CheckedAt: started.UTC(), Latency: time.Since(started)}
		}
		if ctx.Err() != nil {
			return providerapi.HealthStatus{CheckedAt: started.UTC(), Latency: time.Since(started), ErrorKind: status.ErrorKind}
		}
	}
	return providerapi.HealthStatus{CheckedAt: started.UTC(), Latency: time.Since(started), ErrorKind: providerapi.ErrorUnavailable}
}
