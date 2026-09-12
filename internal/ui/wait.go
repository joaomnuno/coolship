package ui

import (
	"context"
	"os"
	"regexp"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

// Spinner shows what a command is waiting for: a Bubble Tea spinner and a
// label on stderr, only while stderr is an interactive terminal, cleared when
// it stops so nothing is left behind. When stderr is not a terminal it prints
// nothing at all, so pipes and CI see no change. The spinner never reads
// stdin, installs no signal handler, and asks the terminal nothing; Ctrl-C
// reaches the executable's own cancellation as it always did.
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
	terminal, ok := drawable(s.streams)
	if !ok {
		return
	}
	style := s.streams.errPalette()
	model := spinnerModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		style:   style,
		label:   singleLine(label),
	}
	s.program = tea.NewProgram(model,
		tea.WithOutput(quietTerminal{terminal}),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
		tea.WithColorProfile(style.colorProfile()))
	s.done = make(chan struct{})
	go func(program *tea.Program, done chan struct{}) {
		defer close(done)
		// The program only draws; whatever ends it, there is nothing to report.
		_, _ = program.Run()
	}(s.program, s.done)
}

// drawable returns stderr as the terminal a Bubble Tea program may draw on,
// or false when nothing should be drawn: the streams are not interactive,
// stderr is not a terminal, or it is a pseudo-terminal that does not report
// a size (unbuffer, script without a terminal behind it), which has no line
// to draw on; drawing would only move the cursor over what was printed
// before. Verbose and debug runs draw nothing either: their request lines
// share stderr, and a live view would redraw over them.
func drawable(streams Streams) (*os.File, bool) {
	terminal, ok := streams.Err.(*os.File)
	if !streams.Interactive || !ok || streams.Trace.Level() != VerbosityNormal || !term.IsTerminal(int(terminal.Fd())) {
		return nil, false
	}
	if width, _, err := term.GetSize(int(terminal.Fd())); err != nil || width <= 0 {
		return nil, false
	}
	return terminal, true
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

// spinnerModel is the one-line Bubble Tea view behind Spinner. The frame is
// styled by the stream's palette, so the colour decision is the stream's.
type spinnerModel struct {
	spinner spinner.Model
	style   palette
	label   string
}

func (m spinnerModel) Init() tea.Cmd { return m.spinner.Tick }

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View is a single line without a newline, so the renderer erases exactly it
// when the program stops. Bracketed paste is an input concern, and there is
// no input, so the terminal mode is left alone.
func (m spinnerModel) View() tea.View {
	view := tea.NewView(m.style.apply(cyan, m.spinner.View()) + " " + m.label)
	view.DisableBracketedPasteMode = true
	return view
}

// terminalRequests are the sequences that ask a terminal to answer: DECRQM
// mode queries, the cursor position report, primary device attributes, the
// terminal version, and the OSC 10, 11, and 12 colour queries.
var terminalRequests = regexp.MustCompile(`\x1b\[(?:\?\d+\$p|6n|0?c|>0?q)|\x1b\]1[0-2];\?(?:\x07|\x1b\\)`)

// quietTerminal is the spinner's output: the stderr terminal, minus anything
// that would ask it a question. Bubble Tea asks whether the terminal supports
// synchronized output when it starts, and with no input reader the answer
// would arrive after the program has exited and land in the shell as typed
// text. The file is still the terminal for size and capability checks.
type quietTerminal struct{ *os.File }

func (q quietTerminal) Write(p []byte) (int, error) {
	filtered := terminalRequests.ReplaceAll(p, nil)
	if len(filtered) > 0 {
		if _, err := q.File.Write(filtered); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}
