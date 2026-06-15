// Package tui provides a single, self-contained helper for showing an animated
// spinner during long-running AI calls. It is the only place ApiLens uses
// Bubble Tea, and it degrades gracefully to a plain printed label when stdout
// is not a terminal (e.g. CI, pipes).
package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// RunWithSpinner runs fn while displaying an animated spinner labeled with
// label. It blocks until fn returns and propagates fn's error.
//
// When stdout is not a TTY the spinner is skipped: the label is printed to
// stderr and fn runs directly. fn always receives a context derived from ctx
// that is cancelled once the spinner stops (including on Ctrl+C).
func RunWithSpinner(ctx context.Context, label string, fn func(context.Context) error) error {
	if fn == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if !isTerminal() {
		if label != "" {
			fmt.Fprintln(os.Stderr, label)
		}
		return fn(ctx)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))

	prog := tea.NewProgram(
		newModel(sp, label),
		tea.WithOutput(os.Stderr),
	)

	go func() {
		err := fn(runCtx)
		prog.Send(doneMsg{err: err})
	}()

	final, err := prog.Run()
	if err != nil {
		return fmt.Errorf("tui: spinner program: %w", err)
	}
	if m, ok := final.(model); ok {
		if m.done {
			return m.err
		}
		// The program was interrupted (Ctrl+C) before fn completed.
		if runCtx.Err() != nil {
			return runCtx.Err()
		}
	}
	return nil
}

// isTerminal reports whether stdout is an interactive terminal.
func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// doneMsg signals that the wrapped function has returned.
type doneMsg struct{ err error }

// model is the Bubble Tea model driving the spinner.
type model struct {
	spinner spinner.Model
	label   string
	err     error
	done    bool
}

func newModel(sp spinner.Model, label string) model {
	return model{spinner: sp, label: label}
}

// Init starts the spinner ticking.
func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update advances the spinner and handles completion and interruption.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case doneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

// View renders the spinner and label, and clears the line once done.
func (m model) View() string {
	if m.done {
		return ""
	}
	if m.label == "" {
		return m.spinner.View()
	}
	return fmt.Sprintf("%s %s", m.spinner.View(), m.label)
}
