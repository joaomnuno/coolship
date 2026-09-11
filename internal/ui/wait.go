package ui

import (
	"context"
	"io"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// Spinner shows what a command is waiting for: a Bubble Tea spinner and a
// label on stderr, only while stderr is an interactive terminal, cleared when
// it stops so nothing is left behind. When stderr is not a terminal it prints
// nothing at all, so pipes and CI see no change. The spinner never reads
// stdin and installs no signal handler; Ctrl-C reaches the executable's own
// cancellation as it always did.
type Spinner struct {
	streams Streams
	program *tea.Program
	done    chan struct{}
}

// NewSpinner prepares a spinner for the streams; nothing is drawn until Start.
func NewSpinner(streams Streams) *Spinner {
	return &Spinner{streams: streams.Normalized()}
}

// Start draws label with a spinner until Stop. A spinner that is already
// running is replaced by one with the new label.
func (s *Spinner) Start(label string) {
	s.Stop()
	if !s.streams.Interactive || !isTerminal(s.streams.Err) {
		return
	}
	model := spinnerModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(s.streams.errPalette().style(cyan))),
		label:   singleLine(label),
	}
	s.program = tea.NewProgram(model,
		tea.WithOutput(s.streams.Err),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
		tea.WithoutBracketedPaste())
	s.done = make(chan struct{})
	go func(program *tea.Program, done chan struct{}) {
		defer close(done)
		// The program only draws; whatever ends it, there is nothing to report.
		_, _ = program.Run()
	}(s.program, s.done)
}

// Stop clears the spinner and waits until the terminal is restored. It is
// safe to call more than once and when nothing was started.
func (s *Spinner) Stop() {
	if s.program == nil {
		return
	}
	s.program.Quit()
	<-s.done
	s.program, s.done = nil, nil
}

// Wait runs fn under a spinner labelled label and returns what fn returns.
// The spinner is gone before Wait returns, so fn's result can be printed on
// a clean line. A context that is already cancelled is returned without
// running fn.
func Wait(ctx context.Context, streams Streams, label string, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spinner := NewSpinner(streams)
	spinner.Start(label)
	defer spinner.Stop()
	return fn(ctx)
}

// spinnerModel is the one-line Bubble Tea view behind Spinner.
type spinnerModel struct {
	spinner spinner.Model
	label   string
}

func (m spinnerModel) Init() tea.Cmd { return m.spinner.Tick }

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View is a single line without a newline, so the renderer erases exactly it
// when the program stops.
func (m spinnerModel) View() string { return m.spinner.View() + " " + m.label }

// isTerminal asks the terminal driver whether w is an attached terminal, as
// the executable does for its own streams.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
