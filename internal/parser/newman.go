package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// ParseNewmanReport parses the JSON output of Newman's `json` reporter at path
// into a [core.NewmanReport]. It reads the `run.executions[]` array, mapping
// each execution and its assertions, and aggregates Total/Passed/Failed. Missing
// or partial fields are tolerated.
func ParseNewmanReport(path string) (*core.NewmanReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read newman report %q: %w", path, err)
	}

	var nf newmanFile
	if err := json.Unmarshal(data, &nf); err != nil {
		return nil, fmt.Errorf("decode newman report: %w: %w", err, core.ErrUnsupportedFormat)
	}

	report := &core.NewmanReport{
		CollectionName: nf.Collection.Info.Name,
	}

	total, failed := 0, 0
	for _, ex := range nf.Run.Executions {
		exec := core.Execution{
			Name:           ex.Item.Name,
			Method:         strings.ToUpper(strings.TrimSpace(ex.Request.Method)),
			URL:            postmanURLString(ex.Request.URL),
			Status:         ex.Response.Code,
			ResponseTimeMs: ex.Response.ResponseTime,
		}
		for _, a := range ex.Assertions {
			as := core.Assertion{
				Name:   a.Assertion,
				Failed: a.Error != nil,
			}
			if a.Error != nil {
				as.ErrorName = a.Error.Name
				as.ErrorMessage = a.Error.Message
			}
			exec.Assertions = append(exec.Assertions, as)
			total++
			if as.Failed {
				failed++
			}
		}
		report.Executions = append(report.Executions, exec)
	}

	// Prefer counts derived from the executions; fall back to run-level stats
	// when no per-assertion detail is present.
	if total == 0 && nf.Run.Stats.Assertions.Total > 0 {
		total = nf.Run.Stats.Assertions.Total
		failed = nf.Run.Stats.Assertions.Failed
	}
	if failed > total {
		failed = total
	}
	report.Total = total
	report.Failed = failed
	report.Passed = total - failed

	return report, nil
}

// --- Newman JSON reporter wire types -----------------------------------------

type newmanFile struct {
	Collection struct {
		Info struct {
			Name string `json:"name"`
		} `json:"info"`
	} `json:"collection"`
	Run newmanRun `json:"run"`
}

type newmanRun struct {
	Executions []newmanExecution `json:"executions"`
	Stats      struct {
		Assertions struct {
			Total   int `json:"total"`
			Failed  int `json:"failed"`
			Pending int `json:"pending"`
		} `json:"assertions"`
	} `json:"stats"`
}

type newmanExecution struct {
	Item struct {
		Name string `json:"name"`
	} `json:"item"`
	Request struct {
		Method string `json:"method"`
		URL    pmURL  `json:"url"`
	} `json:"request"`
	Response struct {
		Code         int    `json:"code"`
		Status       string `json:"status"`
		ResponseTime int    `json:"responseTime"`
	} `json:"response"`
	Assertions []newmanAssertion `json:"assertions"`
}

type newmanAssertion struct {
	Assertion string `json:"assertion"`
	Error     *struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error"`
}
