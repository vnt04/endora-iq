package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFormatUserError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string // substring expected in the result
	}{
		{"nil", nil, ""},
		{"missing api key", fmt.Errorf("wrap: %w", ErrMissingAPIKey), "API key"},
		{"no provider", fmt.Errorf("wrap: %w", ErrNoProvider), "provider"},
		{"unsupported format", fmt.Errorf("wrap: %w", ErrUnsupportedFormat), "unsupported input"},
		{"not implemented", fmt.Errorf("wrap: %w", ErrNotImplemented), "not implemented"},
		{"generic", errors.New("boom"), "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatUserError(tt.err)
			if tt.err == nil {
				if got != "" {
					t.Fatalf("expected empty string for nil, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("FormatUserError = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestEndpointCount(t *testing.T) {
	var nilCol *Collection
	if nilCol.EndpointCount() != 0 {
		t.Error("nil collection should count 0")
	}
	c := &Collection{Endpoints: []Endpoint{{Method: "GET"}, {Method: "POST"}}}
	if c.EndpointCount() != 2 {
		t.Errorf("EndpointCount = %d, want 2", c.EndpointCount())
	}
	if (&Collection{}).EndpointCount() != 0 {
		t.Error("empty collection should count 0")
	}
}

func TestHasFailures(t *testing.T) {
	var nilReport *NewmanReport
	if nilReport.HasFailures() {
		t.Error("nil report should have no failures")
	}

	if (&NewmanReport{Failed: 3}).HasFailures() != true {
		t.Error("Failed>0 should report failures")
	}

	// Failed count zero, but a failed assertion present.
	r := &NewmanReport{
		Executions: []Execution{
			{Assertions: []Assertion{{Failed: false}, {Failed: true}}},
		},
	}
	if !r.HasFailures() {
		t.Error("a failed assertion should report failures")
	}

	clean := &NewmanReport{
		Executions: []Execution{
			{Assertions: []Assertion{{Failed: false}}},
		},
	}
	if clean.HasFailures() {
		t.Error("all-passing report should report no failures")
	}
}
