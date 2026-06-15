// Package openrouter implements the ApiLens AI provider for OpenRouter's
// OpenAI-compatible Chat Completions API (https://openrouter.ai/api/v1). It
// reuses the OpenAI request/response wire shapes (redeclared here so the package
// stays independent of the openai package) and adds OpenRouter's recommended
// HTTP-Referer and X-Title attribution headers. Importing this package for its
// side effects registers the "openrouter" provider.
package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/providers"
	"github.com/apilens/apilens/internal/providers/httpx"
)

const (
	providerName   = "openrouter"
	defaultBaseURL = "https://openrouter.ai/api/v1"
	chatPath       = "/chat/completions"
	refererHeader  = "https://github.com/apilens/apilens"
	titleHeader    = "ApiLens"
	rawSnippetMax  = 8192
)

func init() {
	providers.Register(providerName, New)
}

// New constructs an OpenRouter provider from cfg.
func New(cfg config.Config) (providers.Provider, error) {
	return &Provider{cfg: cfg, client: httpx.NewClient(cfg.Timeout)}, nil
}

// Provider talks to the OpenRouter Chat Completions API.
type Provider struct {
	cfg    config.Config
	client *http.Client
}

// Name returns the provider's registry name.
func (p *Provider) Name() string { return providerName }

// OpenAI-compatible wire shapes (kept local to avoid importing the openai pkg).
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type choice struct {
	Message message `json:"message"`
}

type chatResponse struct {
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

// Chat runs a single OpenRouter Chat Completions request.
func (p *Provider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if err := p.cfg.RequireAPIKey(); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: %w", providerName, err)
	}

	body := chatRequest{
		Model:       req.Model,
		Messages:    buildMessages(req.System, req.Messages),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	headers := map[string]string{
		"Authorization": "Bearer " + p.cfg.APIKey,
		"HTTP-Referer":  refererHeader,
		"X-Title":       titleHeader,
	}
	url := baseURL(p.cfg.BaseURL) + chatPath

	var raw json.RawMessage
	if err := httpx.DoJSON(ctx, p.client, http.MethodPost, url, headers, body, &raw); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: chat: %w", providerName, err)
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: decode response: %w", providerName, err)
	}

	content := ""
	if len(out.Choices) > 0 {
		content = out.Choices[0].Message.Content
	}
	model := out.Model
	if model == "" {
		model = req.Model
	}
	return providers.ChatResponse{
		Content:      content,
		Model:        model,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		Raw:          httpx.Truncate(string(raw), rawSnippetMax),
	}, nil
}

// buildMessages folds the system prompt (when present) into a leading
// system-role message ahead of the conversation turns.
func buildMessages(system string, msgs []providers.Message) []message {
	out := make([]message, 0, len(msgs)+1)
	if strings.TrimSpace(system) != "" {
		out = append(out, message{Role: "system", Content: system})
	}
	for _, m := range msgs {
		out = append(out, message{Role: roleString(m.Role), Content: m.Content})
	}
	return out
}

func roleString(r providers.Role) string {
	switch r {
	case providers.RoleAssistant:
		return "assistant"
	case providers.RoleSystem:
		return "system"
	default:
		return "user"
	}
}

// baseURL returns the configured base URL override or the OpenRouter default,
// without a trailing slash.
func baseURL(override string) string {
	if s := strings.TrimSpace(override); s != "" {
		return strings.TrimRight(s, "/")
	}
	return defaultBaseURL
}
