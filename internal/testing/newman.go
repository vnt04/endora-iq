package datatest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// errNewmanNotFound is returned when neither `newman` nor `npx` can be located
// on PATH. Its message is intentionally actionable for end users.
var errNewmanNotFound = errors.New(
	"newman is required to run data-driven tests but was not found on PATH; " +
		"install it with `npm install -g newman` (or make `npx` available so it can be fetched on demand)")

// runner describes how to invoke newman: the resolved executable plus any
// argument prelude that must precede the newman subcommand (used for the
// `npx --yes newman ...` fallback).
type runner struct {
	bin string   // absolute path to the resolved executable (newman or npx)
	pre []string // args inserted before the newman subcommand (e.g. ["--yes","newman"])
}

// resolveRunner locates newman, preferring a directly installed binary and
// falling back to `npx --yes newman`. It returns errNewmanNotFound when neither
// is available so the caller can surface a friendly install hint.
func resolveRunner() (runner, error) {
	if p, err := exec.LookPath("newman"); err == nil {
		return runner{bin: p}, nil
	}
	if p, err := exec.LookPath("npx"); err == nil {
		return runner{bin: p, pre: []string{"--yes", "newman"}}, nil
	}
	return runner{}, errNewmanNotFound
}

// reportPaths returns the JSON and HTML reporter export paths for a run. A shard
// index <= 0 denotes a single (non-parallel) run and yields report.{json,html};
// a positive index yields report-<n>.{json,html} so parallel shards never clash.
func reportPaths(outDir string, shard int) (jsonPath, htmlPath string) {
	if shard <= 0 {
		return filepath.Join(outDir, "report.json"), filepath.Join(outDir, "report.html")
	}
	return filepath.Join(outDir, fmt.Sprintf("report-%d.json", shard)),
		filepath.Join(outDir, fmt.Sprintf("report-%d.html", shard))
}

// buildNewmanArgs assembles the newman subcommand argument slice (everything
// after the runner prelude). It is pure and side-effect free so it can be unit
// tested without invoking newman.
//
// The base form is:
//
//	run <collection> --reporters cli,json,html
//	  --reporter-json-export <jsonReport> --reporter-html-export <htmlReport>
//
// Optional flags are appended when requested: --iteration-data (when a data file
// is supplied), --bail (Options.Bail), and --delay-request (Options.DelayMs>0).
func buildNewmanArgs(opts Options, dataFile, jsonReport, htmlReport string) []string {
	args := []string{
		"run", opts.CollectionPath,
		"--reporters", "cli,json,html",
		"--reporter-json-export", jsonReport,
		"--reporter-html-export", htmlReport,
	}
	if strings.TrimSpace(dataFile) != "" {
		args = append(args, "--iteration-data", dataFile)
	}
	if opts.Bail {
		args = append(args, "--bail")
	}
	if opts.DelayMs > 0 {
		args = append(args, "--delay-request", strconv.Itoa(opts.DelayMs))
	}
	return args
}

// runNewman executes one newman process with the given subcommand args. It execs
// the resolved binary directly with an argument slice (never via a shell) to
// avoid command injection, and honors ctx cancellation. newman's own CLI output
// is streamed to the parent stdout/stderr.
func runNewman(ctx context.Context, rnr runner, args []string) error {
	// Copy the prelude into a fresh slice before appending so the runner's
	// backing array is never mutated across concurrent shard invocations.
	full := append(append([]string{}, rnr.pre...), args...)

	cmd := exec.CommandContext(ctx, rnr.bin, full...)
	// Parallel shards each stream to the shared terminal, so multi-shard output
	// interleaves by design; the per-shard JSON/HTML reports are the durable,
	// unambiguous record. Do not "fix" this by buffering — it would hide live
	// progress for long runs.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", filepath.Base(rnr.bin), err)
	}
	return nil
}
