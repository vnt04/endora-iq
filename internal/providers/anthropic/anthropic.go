// Package anthropic implements the ApiLens AI provider for Anthropic's Messages
// API (https://api.anthropic.com/v1/messages). Importing this package for its
// side effects registers the "anthropic" provider with the providers registry.
//
// Transport is net/http + encoding/json only; no vendor SDK. Feature logic does
// not live here — this package only maps a providers.ChatRequest onto the
// Messages API wire format and back.
package anthropic

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
	providerName   = "anthropic"
	defaultBaseURL = "https://api.anthropic.com"
	messagesPath   = "/v1/messages"
	apiVersion     = "2023-06-01"
	rawSnippetMax  = 8192
)

func init() {
	providers.Register(providerName, New)
}

// New constructs an Anthropic provider from cfg.
func New(cfg config.Config) (providers.Provider, error) {
	return &Provider{cfg: cfg, client: httpx.NewClient(cfg.Timeout)}, nil
}

// Provider talks to the Anthropic Messages API.
type Provider struct {
	cfg    config.Config
	client *http.Client
}

// Name returns the provider's registry name.
func (p *Provider) Name() string { return providerName }

// message is one entry in the Messages API messages[] array.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type chatResponse struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   usage          `json:"usage"`
}

// Chat runs a single completion against the Messages API.
func (p *Provider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if err := p.cfg.RequireAPIKey(); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: %w", providerName, err)
	}

	body := chatRequest{
		Model:       req.Model,
		System:      req.System,
		Messages:    toMessages(req.Messages),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	headers := map[string]string{
		"x-api-key":         p.cfg.APIKey,
		"anthropic-version": apiVersion,
	}
	url := baseURL(p.cfg.BaseURL) + messagesPath

	var raw json.RawMessage
	if err := httpx.DoJSON(ctx, p.client, http.MethodPost, url, headers, body, &raw); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: chat: %w", providerName, err)
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: decode response: %w", providerName, err)
	}

	model := out.Model
	if model == "" {
		model = req.Model
	}
	return providers.ChatResponse{
		Content:      extractText(out.Content),
		Model:        model,
		InputTokens:  out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
		Raw:          httpx.Truncate(string(raw), rawSnippetMax),
	}, nil
}

// toMessages maps the provider-agnostic messages onto Anthropic roles
// (user/assistant). The system prompt is carried out of band on ChatRequest.
func toMessages(msgs []providers.Message) []message {
	out := make([]message, 0, len(msgs))
	for _, m := range msgs {
		role := "user"
		if m.Role == providers.RoleAssistant {
			role = "assistant"
		}
		out = append(out, message{Role: role, Content: m.Content})
	}
	return out
}

// extractText concatenates the text of all text content blocks (content[i].text),
// which covers the common single-block case while tolerating multi-block replies.
func extractText(blocks []contentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "" || b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// baseURL returns the configured base URL override or the Anthropic default,
// without a trailing slash.
func baseURL(override string) string {
	if s := strings.TrimSpace(override); s != "" {
		return strings.TrimRight(s, "/")
	}
	return defaultBaseURL
}
