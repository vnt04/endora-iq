// Package providers defines the minimal AI provider abstraction used by
// ApiLens and a registry through which concrete providers self-register.
//
// The interface is intentionally tiny: a provider only knows how to run a
// single chat completion ([Provider.Chat]). All feature-specific behavior
// (test generation, auditing, failure analysis, ...) lives one layer up in
// package ai, which builds task prompts and calls Chat. Concrete providers
// (anthropic, openai, gemini, openrouter) live in subpackages and register
// themselves from an init function, so importing a subpackage is enough to make
// it available via [New].
package providers

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/config"
)

// Role identifies the author of a chat [Message].
type Role string

// Chat message roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in a chat conversation.
type Message struct {
	Role    Role
	Content string
}

// ChatRequest is a provider-agnostic chat completion request. System is the
// system prompt (carried separately because some providers require it out of
// band); Messages is the ordered conversation.
type ChatRequest struct {
	Model       string
	System      string
	Messages    []Message
	Temperature float64
	MaxTokens   int
}

// ChatResponse is a provider-agnostic chat completion response. Raw holds the
// undecoded provider response body for debugging and is never required by
// callers.
type ChatResponse struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
	Raw          string
}

// Provider is the minimal AI provider contract. Implementations must be safe
// for concurrent use and must honor ctx cancellation/deadlines.
type Provider interface {
	// Name returns the provider's registry name (e.g. "anthropic").
	Name() string
	// Chat runs a single chat completion.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// Factory constructs a Provider from configuration. Registered by each concrete
// provider subpackage.
type Factory func(cfg config.Config) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register associates a factory with a provider name. It is intended to be
// called from a provider subpackage's init function. Empty names or nil
// factories are ignored (such a registration is a programming error that
// surfaces later as ErrNoProvider rather than a panic).
func Register(name string, f Factory) {
	if name == "" || f == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = f
}

// Registered returns the sorted names of all registered providers.
func Registered() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// New looks up the factory registered under cfg.Provider and builds a Provider.
// It returns core.ErrNoProvider (wrapped) when the provider is not registered.
func New(cfg config.Config) (Provider, error) {
	registryMu.RLock()
	f, ok := registry[cfg.Provider]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", core.ErrNoProvider, cfg.Provider)
	}
	return f(cfg)
}
