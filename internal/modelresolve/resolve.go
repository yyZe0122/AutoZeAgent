// Package modelresolve resolves per-run model pins (O4 / H7-ready).
// Prefer session.PreferredModel or job pin over daemon main without rewriting global config.
package modelresolve

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/yyZe0122/yunmengze-agent/internal/agent"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
	"github.com/yyZe0122/yunmengze-agent/internal/providers"
)

// Endpoint is a resolved provider + wire model id for one run.
type Endpoint struct {
	Ref           string
	Provider      agent.StreamingProvider
	Model         string
	ContextWindow int64
	MaxTokens     int64
}

// Resolver builds and caches endpoints for selection refs (provider/model).
// Fail closed on invalid refs; callers fall back to daemon main.
type Resolver struct {
	configDir string
	mu        sync.Mutex
	cache     map[string]*Endpoint
}

// New returns a resolver for configDir (ConfigDir only).
func New(configDir string) *Resolver {
	return &Resolver{
		configDir: strings.TrimSpace(configDir),
		cache:     make(map[string]*Endpoint),
	}
}

// Resolve returns an endpoint for pin, or (nil, nil) when pin is empty.
// On resolution failure returns (nil, err) — caller should fall back to main.
func (r *Resolver) Resolve(pin string) (*Endpoint, error) {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return nil, nil
	}
	if r == nil || r.configDir == "" {
		return nil, fmt.Errorf("modelresolve: config dir is required")
	}
	r.mu.Lock()
	if ep, ok := r.cache[pin]; ok {
		r.mu.Unlock()
		return ep, nil
	}
	r.mu.Unlock()

	resolved, err := providerconfig.ResolveModel(r.configDir, pin)
	if err != nil {
		return nil, err
	}
	p, err := providers.NewConfigured(*resolved)
	if err != nil {
		return nil, err
	}
	// providerapi.Provider includes Stream and satisfies agent.StreamingProvider.
	stream, ok := p.(agent.StreamingProvider)
	if !ok || stream == nil {
		return nil, fmt.Errorf("modelresolve: provider %T does not support streaming", p)
	}
	ep := &Endpoint{
		Ref:           pin,
		Provider:      stream,
		Model:         resolved.ModelID,
		ContextWindow: resolved.ContextWindow,
		MaxTokens:     resolved.MaxTokens,
	}
	r.mu.Lock()
	r.cache[pin] = ep
	r.mu.Unlock()
	return ep, nil
}

// ResolveOrFallback resolves pin; on empty or error logs and returns nil (use main).
func (r *Resolver) ResolveOrFallback(pin string) *Endpoint {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return nil
	}
	ep, err := r.Resolve(pin)
	if err != nil {
		slog.Warn("model pin resolve failed; using main",
			"component", "modelresolve", "operation", "resolve", "result", "fallback",
			"pin", pin, "error", err)
		return nil
	}
	return ep
}
