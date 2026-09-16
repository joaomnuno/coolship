package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/joaomnuno/coolship/internal/service"
)

// Steps is a command's work shown as a checklist of named steps, drawn the
// way deploy draws its stages: a spinner and elapsed time on the open step,
// ✓ on what finished, ✗ on what failed, – on what was skipped, and the steps
// not reached yet dim. Like Checklist it is a Bubble Tea program on stderr
// with no input and no signal handler, and nothing is drawn until the first
// step starts.
//
// A prompt can run between steps: Pause stops the live view and prints the
// steps that already ended, so they stay on screen above the question, and
// Resume draws the rest again below it. Close prints the final checklist in
// place of the live view.
//
// Off a terminal, with JSON output, or above normal verbosity, no view is
// drawn and each transition is one plain line on stderr, "Step <title>:
// started", "done", "failed", or "skipped". A command whose plain output
// predates Steps checks Live and keeps its own lines instead.
type Steps struct {
	streams  Streams
	renderer *Renderer
	terminal *os.File // nil when the plain path is in use
	style    palette
	clock    func() time.Time

	rows    []stageRow
	printed int  // rows Pause already printed above the view
	paused  bool // between Pause and Resume
	closed  bool

	program *tea.Program
	done    chan struct{}
	final   tea.Model
}

// NewSteps prepares the checklist for titles, in the order they run.
func NewSteps(streams Streams, format string, titles []string) *Steps {
	streams = streams.Normalized()
	s := &Steps{streams: streams, renderer: NewRenderer(streams, format), style: streams.errPalette(), clock: time.Now}
	if format != "json" {
		s.terminal, _ = drawable(streams)
	}
	for _, title := range titles {
		s.rows = append(s.rows, stageRow{name: singleLine(title)})
	}
	return s
}

// Live reports whether the checklist is drawn in a terminal; otherwise every
// transition is a plain line.
func (s *Steps) Live() bool { return s.terminal != nil }

// Start opens step i; its elapsed time counts from now.
func (s *Steps) Start(i int) {
	s.change(i, service.StageStarted, func(row *stageRow, now time.Time) {
		row.status, row.started = service.StageStarted, now
	})
}

// Note sets the detail step i shows after its time, such as the status a
// wait last saw, without changing its state. The plain path prints nothing.
func (s *Steps) Note(i int, detail string) {
	s.change(i, "", func(row *stageRow, _ time.Time) { row.detail = detail })
}

// Done ends step i as finished, with detail after its time when it is not
// empty. A step that never started is shown as lasting no time.
func (s *Steps) Done(i int, detail string) {
	s.change(i, service.StageDone, func(row *stageRow, now time.Time) {
		s.end(row, service.StageDone, now)
		row.detail = detail
	})
}

// Fail ends step i as failed. The error itself is left for the executable
// boundary to print once; an interrupt marks the step … rather than ✗, since
// nothing failed.
func (s *Steps) Fail(i int, err error) {
	status := service.StageFailed
	if errors.Is(err, context.Canceled) {
		status = stageStopped
	}
	s.change(i, service.StageFailed, func(row *stageRow, now time.Time) { s.end(row, status, now) })
}

// Answer ends step i as a question answered: the answer is shown in the
// column a time would take, and no time is shown. The plain path prints
// nothing, since the answer was typed where it can already be seen.
func (s *Steps) Answer(i int, answer string) {
	s.change(i, "", func(row *stageRow, now time.Time) {
		*row = stageRow{name: row.name, status: service.StageDone, answer: true, detail: answer, started: now, ended: now}
	})
}

// Reset returns step i to not reached, for a form that goes back to an
// earlier question. The plain path prints nothing.
func (s *Steps) Reset(i int) {
	s.change(i, "", func(row *stageRow, _ time.Time) { *row = stageRow{name: row.name} })
}

// Skip marks step i as not run, with the reason in parentheses when given.
func (s *Steps) Skip(i int, reason string) {
	s.change(i, service.StageSkipped, func(row *stageRow, _ time.Time) {
		row.status, row.note = service.StageSkipped, reason
	})
}

// Warn prints a warning above the live view, where it stays, or as the
// usual warning line when nothing is drawn.
func (s *Steps) Warn(message string) error {
	if s.program != nil {
		s.program.Send(printMsg(s.style.apply(yellow, "Warning:") + " " + singleLine(message)))
		return nil
	}
	return s.renderer.Warn(message)
}

// Pause stops the live view so a prompt can use the terminal. The steps that
// ended so far are printed plainly and stay above the prompt; the rest are
// drawn again by Resume. It is safe to call when nothing is drawn.
func (s *Steps) Pause() {
	if s.terminal == nil || s.closed || s.paused {
		return
	}
	s.stop()
	s.paused = true
	ended := s.printed
	for ended < len(s.rows) && isEnded(s.rows[ended].status) {
		ended++
	}
	s.print(s.rows[s.printed:ended])
	s.printed = ended
}

// Resume draws the steps Pause left again, below whatever the prompt wrote.
func (s *Steps) Resume() {
	if !s.paused {
		return
	}
	s.paused = false
	s.draw()
}

// Close ends the live view and prints the final checklist in its place: the
// steps Pause did not already print, including those never reached. A step
// still open is marked … with the time it ran. It is safe to call more than
// once and when nothing was drawn.
func (s *Steps) Close() {
	if s.terminal == nil || s.closed {
		return
	}
	s.stop()
	s.closed = true
	now := s.clock()
	for index := range s.rows {
		if row := &s.rows[index]; row.status == service.StageStarted {
			row.status, row.ended = stageStopped, now
		}
	}
	if slices.ContainsFunc(s.rows, func(row stageRow) bool { return row.status != "" }) {
		s.print(s.rows[s.printed:])
	}
	s.printed = len(s.rows)
}

// change applies one transition to step i: on the plain path it prints the
// line for word, when there is one; on a terminal it redraws.
func (s *Steps) change(i int, word string, apply func(*stageRow, time.Time)) {
	if i < 0 || i >= len(s.rows) || s.closed {
		return
	}
	apply(&s.rows[i], s.clock())
	if s.terminal == nil {
		if word != "" {
			_, _ = fmt.Fprintf(s.streams.Err, "Step %s: %s\n", s.rows[i].name, word)
		}
		return
	}
	s.draw()
}

func (s *Steps) end(row *stageRow, status string, now time.Time) {
	if row.status != service.StageStarted {
		row.started = now
	}
	row.status, row.ended = status, now
}

// draw sends the rows to the running view, or starts it once a step not yet
// printed has been reached. A paused view waits for Resume.
func (s *Steps) draw() {
	if s.paused || s.closed {
		return
	}
	rows := slices.Clone(s.rows[s.printed:])
	now := s.clock()
	if s.program != nil {
		s.program.Send(stepsMsg{rows: rows, at: now})
		return
	}
	if !slices.ContainsFunc(rows, func(row stageRow) bool { return row.status != "" }) {
		return
	}
	d := terminalDisplay(s.terminal)
	model := stepsModel{spinner: spinner.New(spinner.WithSpinner(spinner.Dot)), style: s.style, clock: s.clock, now: now, rows: rows, width: d.width}
	s.program, s.done = runView(d, s.style, model, &s.final)
}

// stop ends the running view, waits for it, and erases the frame Bubble Tea
// leaves on screen, one row per step drawn, so what Pause and Close print
// does not repeat them.
func (s *Steps) stop() {
	if s.program == nil {
		return
	}
	s.program.Quit()
	<-s.done
	s.program, s.done = nil, nil
	_, _ = fmt.Fprint(s.streams.Err, eraseView(len(s.rows)-s.printed))
}

// print writes rows plainly on stderr, cut to the terminal's width.
func (s *Steps) print(rows []stageRow) {
	if len(rows) == 0 {
		return
	}
	model := stepsModel{style: s.style, now: s.clock(), rows: rows, width: terminalWidth(s.terminal)}
	_, _ = fmt.Fprintln(s.streams.Err, model.render())
}

func isEnded(status string) bool {
	switch status {
	case service.StageDone, service.StageFailed, service.StageSkipped, stageStopped:
		return true
	}
	return false
}

// stepsMsg carries the rows the view draws, copied, and when they changed.
type stepsMsg struct {
	rows []stageRow
	at   time.Time
}

// stepsModel is the Bubble Tea view behind Steps: one row per step, drawn
// by the same drawRow as deploy's stages.
type stepsModel struct {
	spinner spinner.Model
	style   palette
	clock   func() time.Time
	now     time.Time
	width   int
	rows    []stageRow
}

func (m stepsModel) Init() tea.Cmd { return m.spinner.Tick }

func (m stepsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.now = m.clock()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case printMsg:
		return m, tea.Println(string(msg))
	case stepsMsg:
		m.rows, m.now = msg.rows, msg.at
	}
	return m, nil
}

// View has no trailing newline, so the frame is exactly one line per row,
// which is what stop counts on to erase it.
func (m stepsModel) View() tea.View {
	view := tea.NewView(m.render())
	view.DisableBracketedPasteMode = true
	return view
}

// render draws the rows with their names padded to the column deploy's
// times align on.
func (m stepsModel) render() string {
	spin := m.style.apply(cyan, strings.TrimSpace(m.spinner.View()))
	lines := make([]string, 0, len(m.rows))
	for _, row := range m.rows {
		lines = append(lines, drawRow(m.style, spin, row, labelWidth, m.now))
	}
	return fitLines(lines, m.width)
}
