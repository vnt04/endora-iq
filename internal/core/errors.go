package core

import "errors"

// Sentinel errors shared across ApiLens. Wrap these with fmt.Errorf("...: %w",
// err) at call sites so callers can match with errors.Is.
var (
	// ErrNotImplemented marks a feature entry point that has no implementation
	// yet. Returned by stub Run/Analyze functions during phased development.
	ErrNotImplemented = errors.New("not implemented")

	// ErrUnsupportedFormat is returned by the parser when an input file is
	// neither a recognized Postman collection, OpenAPI spec, nor Newman report.
	ErrUnsupportedFormat = errors.New("unsupported format")

	// ErrNoProvider is returned when no AI provider is registered under the
	// configured provider name.
	ErrNoProvider = errors.New("no provider registered")

	// ErrMissingAPIKey is returned when an AI operation is attempted without the
	// provider's API key being available in the environment.
	ErrMissingAPIKey = errors.New("missing API key")
)

// FormatUserError converts an error into a concise, user-facing message
// suitable for printing to a terminal. It maps known sentinel errors to
// actionable guidance and falls back to the underlying error text otherwise.
func FormatUserError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrMissingAPIKey):
		return "missing API key: set the provider's API key environment variable before running AI commands"
	case errors.Is(err, ErrNoProvider):
		return "unknown AI provider: check the 'provider' setting in apilens.yaml"
	case errors.Is(err, ErrUnsupportedFormat):
		return "unsupported input: expected a Postman collection, OpenAPI spec, or Newman report"
	case errors.Is(err, ErrNotImplemented):
		return "this feature is not implemented yet"
	default:
		return err.Error()
	}
}
