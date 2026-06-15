// Package datatest implements SRS Feature F: data-driven testing. It runs a
// collection against a CSV/JSON/JSONL dataset (via Newman) with optional
// parallelism, bail, and delay. This feature does not require an AI client.
//
// The package lives in internal/testing per SRS §18 but is named datatest to
// avoid shadowing the standard library's testing package.
//
// Security: newman is always exec'd directly with an argument slice (never via
// a shell), so user-supplied collection/dataset paths cannot be interpreted as
// shell syntax. No data is sent to an LLM, so no redaction is required here.
package datatest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/parser"
)

// defaultOutDir is the final fallback when neither Options.OutDir nor
// cfg.ReportDir is set.
const defaultOutDir = "reports"

// Options configures a data-driven run; it mirrors the `apilens test` flags.
type Options struct {
	CollectionPath string // --collection
	DataPath       string // --data: CSV | JSON | JSONL
	OutDir         string // --out-dir: directory for reports (default reports)
	Parallel       int    // --parallel: number of parallel executions
	Bail           bool   // --bail: stop on first failure
	DelayMs        int    // --delay: delay between requests in milliseconds
}

// shardResult captures one newman invocation's outcome for summary aggregation.
type shardResult struct {
	shard      int                // 1-based shard index (0 for a single run)
	jsonReport string             // path to that shard's JSON report
	report     *core.NewmanReport // parsed report (nil if unparseable)
	runErr     error              // error from the newman process, if any
}

// Run is the feature entry point. It validates inputs, loads the dataset,
// resolves a newman runner, and executes one or more newman processes (sharded
// when Options.Parallel > 1), then prints a terminal summary. The client is
// always nil for this feature; AI is not used.
func Run(ctx context.Context, cfg config.Config, _ *ai.Client, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if strings.TrimSpace(opts.CollectionPath) == "" {
		return fmt.Errorf("test: collection path is required (--collection)")
	}
	if err := assertFile(opts.CollectionPath); err != nil {
		return fmt.Errorf("test: collection %q: %w", opts.CollectionPath, err)
	}
	if strings.TrimSpace(opts.DataPath) == "" {
		return fmt.Errorf("test: data path is required (--data)")
	}

	rows, err := parser.LoadDataset(opts.DataPath)
	if err != nil {
		return fmt.Errorf("test: load dataset %q: %w", opts.DataPath, err)
	}
	srcFormat := parser.DetectFormat(opts.DataPath)

	outDir := resolveOutDir(opts.OutDir, cfg.ReportDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("test: create out dir %q: %w", outDir, err)
	}

	rnr, err := resolveRunner()
	if err != nil {
		// Friendly, non-crashing install guidance (errNewmanNotFound).
		return fmt.Errorf("test: %w", err)
	}

	// Temp dir for generated iteration-data files (shards / normalized JSONL).
	tmpDir, err := os.MkdirTemp("", "apilens-data-*")
	if err != nil {
		return fmt.Errorf("test: create temp data dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if opts.Parallel > 1 {
		return runParallel(ctx, rnr, opts, outDir, tmpDir, rows, srcFormat)
	}
	return runSingle(ctx, rnr, opts, outDir, tmpDir, rows, srcFormat)
}

// runSingle executes one newman process for the whole dataset.
func runSingle(ctx context.Context, rnr runner, opts Options, outDir, tmpDir string, rows []map[string]string, srcFormat string) error {
	dataFile := opts.DataPath
	// newman cannot read JSONL directly; normalize it to a JSON array file.
	if srcFormat == "jsonl" {
		df, err := writeDataFile(tmpDir, rows, "json")
		if err != nil {
			return fmt.Errorf("test: normalize jsonl dataset: %w", err)
		}
		dataFile = df
	}

	jsonReport, htmlReport := reportPaths(outDir, 0)
	args := buildNewmanArgs(opts, dataFile, jsonReport, htmlReport)

	runErr := runNewman(ctx, rnr, args)
	if ctx.Err() != nil {
		return fmt.Errorf("test: %w", ctx.Err())
	}

	report, perr := parser.ParseNewman(jsonReport)
	if perr != nil {
		// A non-zero newman exit is expected when assertions fail, but only
		// fatal when it left no parseable report behind.
		if runErr != nil {
			return fmt.Errorf("test: newman run failed and produced no parseable report: %w", runErr)
		}
		return fmt.Errorf("test: parse report %q: %w", jsonReport, perr)
	}

	printSummary([]shardResult{{report: report}}, outDir)
	if opts.Bail && report.Failed > 0 {
		return fmt.Errorf("test: %d assertion(s) failed", report.Failed)
	}
	return nil
}

// runParallel shards the dataset and runs one newman process per shard
// concurrently (bounded by Options.Parallel, since shardRows caps the shard
// count at Parallel), then aggregates per-shard reports.
func runParallel(ctx context.Context, rnr runner, opts Options, outDir, tmpDir string, rows []map[string]string, srcFormat string) error {
	shards := shardRows(rows, opts.Parallel)
	dataFmt := newmanDataFormat(srcFormat)
	results := make([]shardResult, len(shards))

	var wg sync.WaitGroup
	for i := range shards {
		dataFile, err := writeDataFile(tmpDir, shards[i], dataFmt)
		if err != nil {
			return fmt.Errorf("test: write shard %d data: %w", i+1, err)
		}
		jsonReport, htmlReport := reportPaths(outDir, i+1)
		results[i].shard = i + 1
		results[i].jsonReport = jsonReport
		args := buildNewmanArgs(opts, dataFile, jsonReport, htmlReport)

		wg.Add(1)
		// Each goroutine writes only its own results[idx], so distinct indices
		// require no additional synchronization.
		go func(idx int, a []string) {
			defer wg.Done()
			results[idx].runErr = runNewman(ctx, rnr, a)
		}(i, args)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return fmt.Errorf("test: %w", ctx.Err())
	}

	fatal, totalFailed := aggregate(results)
	if fatal != nil {
		return fatal
	}

	printSummary(results, outDir)
	if opts.Bail && totalFailed > 0 {
		return fmt.Errorf("test: %d assertion(s) failed across %d shard(s)", totalFailed, len(results))
	}
	return nil
}

// aggregate parses every shard report into results, returning the first fatal
// error (a shard that produced no parseable report) and the total failed-
// assertion count across all parseable shards.
func aggregate(results []shardResult) (fatal error, totalFailed int) {
	for i := range results {
		report, perr := parser.ParseNewman(results[i].jsonReport)
		if perr != nil {
			if fatal == nil {
				if results[i].runErr != nil {
					fatal = fmt.Errorf("test: shard %d failed and produced no parseable report: %w",
						results[i].shard, results[i].runErr)
				} else {
					fatal = fmt.Errorf("test: shard %d: parse report %q: %w",
						results[i].shard, results[i].jsonReport, perr)
				}
			}
			continue
		}
		results[i].report = report
		totalFailed += report.Failed
	}
	return fatal, totalFailed
}

// printSummary prints a concise human-readable summary to stdout. Per-shard
// lines are only printed for multi-shard (parallel) runs.
func printSummary(results []shardResult, outDir string) {
	name := ""
	total, passed, failed := 0, 0, 0
	for _, r := range results {
		if r.report == nil {
			continue
		}
		if name == "" {
			name = r.report.CollectionName
		}
		total += r.report.Total
		passed += r.report.Passed
		failed += r.report.Failed
	}
	if strings.TrimSpace(name) == "" {
		name = "(unknown collection)"
	}

	fmt.Printf("ApiLens data-driven test - %s\n", name)
	if len(results) > 1 {
		for _, r := range results {
			if r.report == nil {
				fmt.Printf("  Shard %d: no report produced\n", r.shard)
				continue
			}
			fmt.Printf("  Shard %d: %d/%d assertions passed, %d failed\n",
				r.shard, r.report.Passed, r.report.Total, r.report.Failed)
		}
	}
	fmt.Printf("  Total:   %d assertions, %d passed, %d failed\n", total, passed, failed)
	fmt.Printf("  Reports: %s\n", outDir)
}

// resolveOutDir picks the reports directory: explicit Options.OutDir, then
// cfg.ReportDir, then the built-in default.
func resolveOutDir(optsDir, cfgDir string) string {
	if d := strings.TrimSpace(optsDir); d != "" {
		return d
	}
	if d := strings.TrimSpace(cfgDir); d != "" {
		return d
	}
	return defaultOutDir
}

// assertFile reports an error when path does not exist or is a directory.
func assertFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("expected a file but found a directory")
	}
	return nil
}
