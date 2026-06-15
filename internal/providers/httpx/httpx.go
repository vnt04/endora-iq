// Package httpx is a tiny shared HTTP helper for the AI provider subpackages.
//
// Providers implement transport with net/http + encoding/json only (no vendor
// SDKs), so the JSON request/response plumbing they all share lives here:
// marshalling a body, setting headers, performing the request with a single
// retry on transient failures (429/5xx), checking the status code, and
// decoding the response.
//
// Security: this package never logs request headers (which carry API keys), and
// any response body echoed back inside an error is first passed through
// [redact.String] so secrets that a server may reflect are masked.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apilens/apilens/internal/core/redact"
)

const (
	// DefaultTimeout is applied by [NewClient] when given a non-positive value.
	DefaultTimeout = 120 * time.Second

	// maxErrSnippet bounds how much of a (redacted) error response body is
	// included in a status error.
	maxErrSnippet = 512

	// retryBackoff is the base pause before the single retry; the wait scales
	// linearly with the attempt number.
	retryBackoff = 500 * time.Millisecond

	// maxAttempts is the total number of attempts: one try plus one retry.
	maxAttempts = 2
)

// NewClient returns an *http.Client configured with the given timeout. A
// non-positive timeout falls back to [DefaultTimeout].
func NewClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// DoJSON marshals body to JSON, performs an HTTP request to url, and decodes a
// 2xx JSON response into out (which may be nil to ignore the body).
//
// The provided headers are set on the request; Content-Type and Accept default
// to application/json. A single retry is attempted on HTTP 429 and 5xx
// responses and on transient transport errors, with a short backoff that honors
// ctx cancellation. Non-2xx responses yield an error containing the status code
// and a truncated, redacted snippet of the response body. Header values (API
// keys) are never logged or echoed.
func DoJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any, out any) error {
	if client == nil {
		client = NewClient(0)
	}

	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("httpx: marshal request body: %w", err)
		}
		payload = b
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		retry, err := doOnce(ctx, client, method, url, headers, payload, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry || attempt == maxAttempts {
			return err
		}
		if waitErr := backoff(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("httpx: %s request failed after %d attempts", method, maxAttempts)
}

// doOnce performs a single attempt. The returned bool reports whether the
// failure is retryable.
func doOnce(ctx context.Context, client *http.Client, method, url string, headers map[string]string, payload []byte, out any) (retry bool, err error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return false, fmt.Errorf("httpx: build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Context cancellation/deadline is terminal and must not be retried.
		if ctx.Err() != nil {
			return false, fmt.Errorf("httpx: %s request canceled: %w", method, ctx.Err())
		}
		// Sanitize: the underlying *url.Error embeds the full URL, which for
		// some providers carries the API key as a query parameter.
		return true, fmt.Errorf("httpx: %s request failed: %s", method, redact.String(err.Error()))
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return isRetryable(resp.StatusCode), fmt.Errorf("httpx: read response body: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return isRetryable(resp.StatusCode), statusError(method, resp.StatusCode, respBody)
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return false, fmt.Errorf("httpx: decode response (status %d): %w", resp.StatusCode, err)
		}
	}
	return false, nil
}

// isRetryable reports whether a status code warrants the single retry.
func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// statusError builds an error for a non-2xx response, embedding a redacted,
// truncated snippet of the body for debugging.
func statusError(method string, status int, body []byte) error {
	snippet := Truncate(redact.String(string(body)), maxErrSnippet)
	if snippet == "" {
		return fmt.Errorf("httpx: %s returned status %d", method, status)
	}
	return fmt.Errorf("httpx: %s returned status %d: %s", method, status, snippet)
}

// backoff sleeps for a short, attempt-scaled duration, returning early with the
// context error if ctx is canceled first.
func backoff(ctx context.Context, attempt int) error {
	t := time.NewTimer(time.Duration(attempt) * retryBackoff)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Truncate trims whitespace and shortens s to at most max bytes, appending a
// marker when truncation occurred. A non-positive max returns the trimmed
// string unchanged. Shared by providers to bound the ChatResponse.Raw snippet.
func Truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
