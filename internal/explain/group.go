package explain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// Caps that keep individual group records (and thus prompts/markdown) bounded.
const (
	maxEndpointsPerGroup  = 12
	maxAssertionsPerGroup = 12
	signatureMaxWords     = 12
)

// failureGroup is a cluster of related failed assertions. Failures are clustered
// by their error name plus a normalized signature of the error message, so that
// the same underlying problem reported across many endpoints (with differing
// numbers/values) collapses into one analyzable unit.
//
// Fields hold raw (un-redacted) data; redaction is applied at render/prompt time.
type failureGroup struct {
	ErrorName     string   // assertion error name (e.g. "AssertionError")
	Signature     string   // normalized message signature used as the cluster key
	SampleMessage string   // first raw error message seen in the cluster
	Count         int      // number of failed assertions in the cluster
	Endpoints     []string // distinct execution labels ("METHOD URL -> HTTP n")
	Assertions    []string // distinct failed assertion names
}

// messageSignature collapses an error message to a stable signature for
// clustering: it lower-cases the text, replaces quoted substrings with "?",
// replaces digit runs with "#", drops other punctuation, and keeps the leading
// words. Messages that differ only in concrete values (codes, ids, fields)
// therefore share a signature.
func messageSignature(msg string) string {
	s := strings.ToLower(strings.TrimSpace(msg))
	if s == "" {
		return ""
	}
	s = reSigQuoted.ReplaceAllString(s, "?")
	s = reSigDigits.ReplaceAllString(s, "#")
	s = reSigNonWord.ReplaceAllString(s, " ")
	fields := strings.Fields(s)
	if len(fields) > signatureMaxWords {
		fields = fields[:signatureMaxWords]
	}
	return strings.Join(fields, " ")
}

var (
	reSigQuoted  = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	reSigDigits  = regexp.MustCompile(`\d+`)
	reSigNonWord = regexp.MustCompile(`[^a-z0-9#? ]+`)
)

// groupFailures walks the report's executions and clusters every failed
// assertion. The returned groups are ordered by descending failure count, then
// by error name and signature, for deterministic output.
func groupFailures(report *core.NewmanReport) []failureGroup {
	if report == nil {
		return nil
	}

	type accumulator struct {
		group       failureGroup
		endpointSet map[string]bool
		assertSet   map[string]bool
	}

	order := make([]string, 0)
	byKey := make(map[string]*accumulator)

	for _, ex := range report.Executions {
		label := executionLabel(ex)
		for _, a := range ex.Assertions {
			if !a.Failed {
				continue
			}
			sig := messageSignature(a.ErrorMessage)
			key := strings.ToLower(strings.TrimSpace(a.ErrorName)) + "\x00" + sig

			acc := byKey[key]
			if acc == nil {
				acc = &accumulator{
					group: failureGroup{
						ErrorName:     strings.TrimSpace(a.ErrorName),
						Signature:     sig,
						SampleMessage: a.ErrorMessage,
					},
					endpointSet: make(map[string]bool),
					assertSet:   make(map[string]bool),
				}
				byKey[key] = acc
				order = append(order, key)
			}

			acc.group.Count++
			if acc.group.SampleMessage == "" && a.ErrorMessage != "" {
				acc.group.SampleMessage = a.ErrorMessage
			}
			addCapped(&acc.group.Endpoints, acc.endpointSet, label, maxEndpointsPerGroup)
			addCapped(&acc.group.Assertions, acc.assertSet, strings.TrimSpace(a.Name), maxAssertionsPerGroup)
		}
	}

	groups := make([]failureGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, byKey[key].group)
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		if groups[i].ErrorName != groups[j].ErrorName {
			return groups[i].ErrorName < groups[j].ErrorName
		}
		return groups[i].Signature < groups[j].Signature
	})
	return groups
}

// addCapped appends value to list (deduped via seen) until limit entries exist.
// Empty values are ignored.
func addCapped(list *[]string, seen map[string]bool, value string, limit int) {
	if value == "" || seen[value] {
		return
	}
	seen[value] = true
	if len(*list) < limit {
		*list = append(*list, value)
	}
}

// executionLabel renders a human-readable identifier for an execution:
// "METHOD URL -> HTTP <status>", falling back to the execution name.
func executionLabel(ex core.Execution) string {
	method := strings.ToUpper(strings.TrimSpace(ex.Method))
	label := strings.TrimSpace(method + " " + strings.TrimSpace(ex.URL))
	if label == "" {
		label = strings.TrimSpace(ex.Name)
	}
	if label != "" && ex.Status > 0 {
		label = fmt.Sprintf("%s -> HTTP %d", label, ex.Status)
	}
	return label
}
