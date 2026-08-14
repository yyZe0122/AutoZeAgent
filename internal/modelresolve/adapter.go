package modelresolve

import "github.com/yyZe0122/yunmengze-agent/internal/chatsession"

// AsChatResolver adapts Resolver to chatsession.ModelPinResolver.
func (r *Resolver) AsChatResolver() chatsession.ModelPinResolver {
	if r == nil {
		return nil
	}
	return chatResolver{r: r}
}

type chatResolver struct {
	r *Resolver
}

func (c chatResolver) ResolveOrFallback(pin string) *chatsession.ModelPin {
	ep := c.r.ResolveOrFallback(pin)
	if ep == nil {
		return nil
	}
	return &chatsession.ModelPin{
		Ref: ep.Ref, Provider: ep.Provider, Model: ep.Model,
		ContextWindow: ep.ContextWindow, MaxTokens: ep.MaxTokens,
	}
}

func (c chatResolver) ResolveStrict(pin string) (*chatsession.ModelPin, error) {
	ep, err := c.r.Resolve(pin)
	if err != nil {
		return nil, err
	}
	if ep == nil {
		return nil, nil
	}
	return &chatsession.ModelPin{
		Ref: ep.Ref, Provider: ep.Provider, Model: ep.Model,
		ContextWindow: ep.ContextWindow, MaxTokens: ep.MaxTokens,
	}, nil
}
