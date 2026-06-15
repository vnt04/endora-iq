// Package gemini implements the ApiLens AI provider for Google's Gemini
// generateContent API (POST {base}/models/{model}:generateContent?key=...).
// Importing this package for its side effects registers the "gemini" provider.
//
// Transport is net/http + encoding/json only; no vendor SDK. The system prompt
// maps to systemInstruction, conversation turns map to contents[] with roles
// "user"/"model".
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/providers"
	"github.com/apilens/apilens/internal/providers/httpx"
)

const (
	providerName   = "gemini"
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	rawSnippetMax  = 8192
)

func init() {
	providers.Register(providerName, New)
}

// New constructs a Gemini provider from cfg.
func New(cfg config.Config) (providers.Provider, error) {
	return &Provider{cfg: cfg, client: httpx.NewClient(cfg.Timeout)}, nil
}

// Provider talks to the Gemini generateContent API.
type Provider struct {
	cfg    config.Config
	client *http.Client
}

// Name returns the provider's registry name.
func (p *Provider) Name() string { return providerName }

type part struct {
	Text string `json:"text"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type systemInstruction struct {
	Parts []part `json:"parts"`
}

type generationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type chatRequest struct {
	SystemInstruction *systemInstruction `json:"systemInstruction,omitempty"`
	Contents          []content          `json:"contents"`
	GenerationConfig  generationConfig   `json:"generationConfig"`
}

type candidate struct {
	Content content `json:"content"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

type chatResponse struct {
	Candidates    []candidate   `json:"candidates"`
	UsageMetadata usageMetadata `json:"usageMetadata"`
	ModelVersion  string        `json:"modelVersion"`
}

// Chat runs a single generateContent request.
func (p *Provider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if err := p.cfg.RequireAPIKey(); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: %w", providerName, err)
	}

	body := chatRequest{
		Contents: toContents(req.Messages),
		GenerationConfig: generationConfig{
			Temperature:     req.Temperature,
			MaxOutputTokens: req.MaxTokens,
		},
	}
	if s := strings.TrimSpace(req.System); s != "" {
		body.SystemInstruction = &systemInstruction{Parts: []part{{Text: req.System}}}
	}

	// The API key is a query parameter, not a header.
	reqURL := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		baseURL(p.cfg.BaseURL), url.PathEscape(req.Model), url.QueryEscape(p.cfg.APIKey))

	var raw json.RawMessage
	if err := httpx.DoJSON(ctx, p.client, http.MethodPost, reqURL, nil, body, &raw); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: chat: %w", providerName, err)
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("%s: decode response: %w", providerName, err)
	}

	model := out.ModelVersion
	if model == "" {
		model = req.Model
	}
	return providers.ChatResponse{
		Content:      extractText(out.Candidates),
		Model:        model,
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
		Raw:          httpx.Truncate(string(raw), rawSnippetMax),
	}, nil
}

// toContents maps conversation turns onto Gemini contents[] with roles
// "user"/"model".
func toContents(msgs []providers.Message) []content {
	out := make([]content, 0, len(msgs))
	for _, m := range msgs {
		role := "user"
		if m.Role == providers.RoleAssistant {
			role = "model"
		}
		out = append(out, content{Role: role, Parts: []part{{Text: m.Content}}})
	}
	return out
}

// extractText concatenates the text of the first candidate's parts
// (candidates[0].content.parts[i].text).
func extractText(candidates []candidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, pt := range candidates[0].Content.Parts {
		sb.WriteString(pt.Text)
	}
	return sb.String()
}

// baseURL returns the configured base URL override or the Gemini default,
// without a trailing slash.
func baseURL(override string) string {
	if s := strings.TrimSpace(override); s != "" {
		return strings.TrimRight(s, "/")
	}
	return defaultBaseURL
}
