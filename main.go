// Command apilens is the ApiLens CLI: an AI quality layer on top of existing
// API tooling (Postman, Newman, OpenAPI). All behavior is wired in package cmd.
package main

import (
	"github.com/apilens/apilens/cmd"

	// Register AI providers for their init() side effects so they are
	// resolvable via the provider registry at runtime. Each subpackage calls
	// providers.Register from its init function.
	_ "github.com/apilens/apilens/internal/providers/anthropic"
	_ "github.com/apilens/apilens/internal/providers/gemini"
	_ "github.com/apilens/apilens/internal/providers/openai"
	_ "github.com/apilens/apilens/internal/providers/openrouter"
)

func main() {
	cmd.Execute()
}
