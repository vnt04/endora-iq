// Package logging configures the shared zerolog logger for ApiLens.
//
// Logs are written to stderr (so they never pollute machine-readable stdout
// output) using zerolog's human-friendly console writer. Setup selects the
// level; L returns the current package logger for ad-hoc logging.
package logging

import (
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var (
	mu     sync.RWMutex
	logger = newLogger(zerolog.InfoLevel)
)

// newLogger builds a console logger at the given level writing to stderr.
func newLogger(level zerolog.Level) zerolog.Logger {
	cw := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	return zerolog.New(cw).Level(level).With().Timestamp().Logger()
}

// Setup configures and installs the package logger. When verbose is true the
// level is Debug, otherwise Info. The configured logger is returned for callers
// that prefer to hold their own copy.
func Setup(verbose bool) zerolog.Logger {
	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	l := newLogger(level)
	mu.Lock()
	logger = l
	mu.Unlock()
	return l
}

// L returns a pointer to a copy of the current package logger. It is safe for
// concurrent use; the returned pointer is stable for the caller's use.
func L() *zerolog.Logger {
	mu.RLock()
	l := logger
	mu.RUnlock()
	return &l
}
