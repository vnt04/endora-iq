package docs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// execLookPath is the seam used to discover external tools. It defaults to
// exec.LookPath but is overridable in tests so unit tests never depend on (or
// accidentally invoke) npx/redoc/postman-to-openapi.
var execLookPath = exec.LookPath

// hasCommand reports whether an executable named `name` is resolvable on PATH.
func hasCommand(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := execLookPath(name)
	return err == nil
}

// toolError is a typed error capturing a failed external tool invocation so
// callers can decide to fall back. The combined stdout+stderr is retained for
// diagnostics.
type toolError struct {
	name   string
	args   []string
	output string
	err    error
}

func (e *toolError) Error() string {
	out := strings.TrimSpace(e.output)
	if out != "" {
		return fmt.Sprintf("tool %q %s failed: %v: %s", e.name, strings.Join(e.args, " "), e.err, out)
	}
	return fmt.Sprintf("tool %q %s failed: %v", e.name, strings.Join(e.args, " "), e.err)
}

func (e *toolError) Unwrap() error { return e.err }

// runTool executes an external binary directly (no shell), capturing combined
// output. The binary and arguments are passed as a discrete arg slice, so
// untrusted path values cannot be interpreted as shell syntax (no command
// injection). On failure it returns a *toolError.
func runTool(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &toolError{name: name, args: args, output: string(out), err: err}
	}
	return nil
}
