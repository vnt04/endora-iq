// Package ai provides the high-level Client that wraps an AI provider and
// realizes the four SRS §17 operations (Generate, Analyze, Review, Explain) at
// the application layer.
//
// Providers expose only a single Chat method; this Client builds a
// provider-agnostic ChatRequest from the active configuration (model,
// temperature, max tokens) and returns the textual response. The four named
// methods are thin, identically-shaped wrappers over one private complete
// helper — they differ only in intent at the call site, which keeps prompt
// construction in the feature packages rather than here.
package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/providers"
)

// Client is a configured AI client bound to a single provider.
type Client struct {
	provider providers.Provider
	cfg      config.Config
}

// New builds a Client by resolving the provider named in cfg via the provider
// registry. Returns an error if the provider is unknown or fails to construct.
func New(cfg config.Config) (*Client, error) {
	p, err := providers.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("ai: new client: %w", err)
	}
	return &Client{provider: p, cfg: cfg}, nil
}

// NewWithProvider builds a Client around an explicit provider. Intended for
// tests using a fake provider.
func NewWithProvider(p providers.Provider, cfg config.Config) *Client {
	return &Client{provider: p, cfg: cfg}
}

// Provider returns the underlying provider.
func (c *Client) Provider() providers.Provider { return c.provider }

// Config returns the client's configuration.
func (c *Client) Config() config.Config { return c.cfg }

// complete runs a single-turn completion with the given system and user
// prompts and returns the trimmed response content.
func (c *Client) complete(ctx context.Context, system, user string) (string, error) {
	if c == nil || c.provider == nil {
		return "", fmt.Errorf("ai: client has no provider")
	}
	req := providers.ChatRequest{
		Model:       c.cfg.Model,
		System:      system,
		Messages:    []providers.Message{{Role: providers.RoleUser, Content: user}},
		Temperature: c.cfg.Temperature,
		MaxTokens:   c.cfg.MaxTokens,
	}
	resp, err := c.provider.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("ai: chat: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// Generate produces new content (e.g. test scripts) from the prompts. SRS §17.
func (c *Client) Generate(ctx context.Context, system, user string) (string, error) {
	return c.complete(ctx, system, user)
}

// Analyze produces an analysis (e.g. audit narrative) from the prompts. SRS §17.
func (c *Client) Analyze(ctx context.Context, system, user string) (string, error) {
	return c.complete(ctx, system, user)
}

// Review produces recommendations (e.g. collection review) from the prompts.
// SRS §17.
func (c *Client) Review(ctx context.Context, system, user string) (string, error) {
	return c.complete(ctx, system, user)
}

// Explain produces an explanation (e.g. failure root-cause prose) from the
// prompts. SRS §17.
func (c *Client) Explain(ctx context.Context, system, user string) (string, error) {
	return c.complete(ctx, system, user)
}
