// Package fake provides a deterministic providers.Provider test double.
//
// It is a regular (non _test.go) package so it can be imported by the tests of
// any feature package: build an ai.Client with ai.NewWithProvider(fake.New(...),
// cfg) and assert against the recorded request without needing an API key or
// network access.
package fake

import (
	"context"

	"github.com/apilens/apilens/internal/providers"
)

const providerName = "fake"

// Provider is a deterministic [providers.Provider]. It records the last request
// and the number of calls, and returns canned content (or a canned error).
//
// Two response modes are supported:
//   - Resp: a single fixed response returned for every call.
//   - Responses: a per-call sequence; the Nth call returns the Nth entry,
//     clamping to the final entry once exhausted. Takes precedence over Resp
//     when non-empty.
//
// The zero value is usable and returns an empty response. The type is not safe
// for concurrent use; it is intended for single-goroutine tests.
type Provider struct {
	// Resp is the content returned by Chat when Responses is empty.
	Resp string
	// Err, when non-nil, is returned by every Chat call (before recording).
	Err error
	// LastReq is the most recent request passed to Chat.
	LastReq providers.ChatRequest
	// Calls counts how many times Chat has been invoked.
	Calls int
	// Responses, when non-empty, supplies canned responses per call.
	Responses []string
}

// New returns a Provider that replies with resp for every call.
func New(resp string) *Provider {
	return &Provider{Resp: resp}
}

// NewSequence returns a Provider that yields the given responses across
// successive Chat calls, in order, clamping to the last entry once exhausted.
func NewSequence(responses ...string) *Provider {
	return &Provider{Responses: responses}
}

// NewError returns a Provider whose Chat always fails with err.
func NewError(err error) *Provider {
	return &Provider{Err: err}
}

// Name reports the provider name ("fake").
func (p *Provider) Name() string { return providerName }

// Chat records the request, increments the call count, and returns the next
// canned response (or the configured error).
func (p *Provider) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	p.Calls++
	p.LastReq = req
	if p.Err != nil {
		return providers.ChatResponse{}, p.Err
	}

	content := p.Resp
	if len(p.Responses) > 0 {
		idx := p.Calls - 1
		if idx >= len(p.Responses) {
			idx = len(p.Responses) - 1
		}
		content = p.Responses[idx]
	}
	return providers.ChatResponse{
		Content: content,
		Model:   req.Model,
	}, nil
}
