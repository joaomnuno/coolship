package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/joaomnuno/coolship/internal/problem"
	"github.com/joaomnuno/coolship/internal/service"
)

// A form flow asks a command's questions and runs its checks as one terminal
// view built on Steps. Every step is a checklist row: answered questions show
// their answer, checks show a spinner and then ✓ or ✗ with their time, and
// the rows not reached yet are dim. The question being asked is drawn in
// place of its row, so the previous answers stay listed above it.
//
// Shift+Tab goes back to the previous question with its answer kept, and so
// does the up arrow when a text field is empty or a list is at its top. Esc
// and Ctrl-C leave. A failed check explains itself the way the executable
// boundary would, then offers Retry, Go back, and Leave, plus opening the
// Coolify page a setup problem names. Nothing the flow's steps do is
// committed until the last one runs, so leaving is always safe.

// flowKind is what a stage of a flow does.
type flowKind int

const (
	flowChoose flowKind = iota // pick one option from a list
	flowText                   // type a line
	flowSecret                 // type a line that is not echoed
	flowCheck                  // run work with a spinner
)

// flowOption is one entry of a list: a label, a dim detail after it, and the
// value the stage records. Choosing a leave option leaves the flow.
type flowOption struct {
	label  string
	detail string
	value  string
	leave  bool
}

// flowStage is one step of a flow. Several stages may fill the same row, such
// as a choice that decides whether a text question follows; the row shows the
// last answer.
type flowStage struct {
	step  int // the Steps row this stage fills
	kind  flowKind
	title string // the question; the row's own name when empty

	// note returns dim lines shown under the question.
	note func() []string
	// options lists a choice's entries.
	options     func() []flowOption
	placeholder string
	// initial is the value a question starts with the first time it is asked;
	// going back to it shows the last answer instead.
	initial  func() string
	validate func(string) error
	// set records an answer and returns what its row shows.
	set func(string) string
	// skip decides, when the stage is reached, that it is not asked. A
	// nonempty detail marks its row answered with it, such as a URL given
	// as a flag.
	skip func() (string, bool)

	// prepare reads what a check needs while the flow owns its state, and
	// returns the work, which runs in the background.
	prepare func() func(context.Context) (any, error)
	// accept records a check's result and returns what its row shows.
	accept func(any) string
}

type flowCheckMsg struct {
	id     int
	result any
	err    error
}

type flowFailure struct {
	err     error
	report  string
	options []flowOption
	cursor  int
	notice  string
	fixURL  string
}

// flowModel is the Bubble Tea model of a running flow.
type flowModel struct {
	ctx     context.Context
	steps   *Steps
	stages  []flowStage
	style   palette
	clock   func() time.Time
	now     time.Time
	width   int
	spinner spinner.Model
	open    func(string) error
	// launch turns a check's work into a command; tests run it at once.
	launch func(func() tea.Msg) tea.Cmd

	index   int
	history []int // the questions answered, in order, for going back
	answers map[int]string
	input   textinput.Model
	options []flowOption
	cursor  int
	invalid string

	running int // the id of the check in progress, 0 when none
	runs    int
	cancel  context.CancelFunc

	failure  *flowFailure
	finished bool
	err      error

	// height is the tallest frame drawn so far. Every frame is padded to it:
	// Bubble Tea clears a frame that shrinks from the wrong row, leaving
	// stale lines above the view. frame is the last frame, kept on screen
	// when the flow ends so runFlow can erase exactly its lines.
	height int
	frame  string
}

func newFlowModel(ctx context.Context, steps *Steps, stages []flowStage, style palette, width int, open func(string) error) *flowModel {
	m := &flowModel{ctx: ctx, steps: steps, stages: stages, style: style, clock: time.Now, width: width, open: open,
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)), answers: map[int]string{},
		launch: func(work func() tea.Msg) tea.Cmd { return work }}
	m.now = m.clock()
	return m
}

func (m *flowModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.enter(0))
}

// enter moves to stage i or the first stage after it that is not skipped,
// and finishes the flow after the last one.
func (m *flowModel) enter(i int) tea.Cmd {
	m.invalid, m.failure = "", nil
	for ; i < len(m.stages); i++ {
		stage := m.stages[i]
		if stage.skip != nil {
			if detail, skip := stage.skip(); skip {
				if detail != "" {
					m.steps.Answer(stage.step, detail)
				}
				continue
			}
		}
		m.index = i
		if stage.kind == flowCheck {
			return m.runCheck()
		}
		return m.ask()
	}
	m.index, m.finished = len(m.stages), true
	return tea.Quit
}

func (m *flowModel) ask() tea.Cmd {
	stage := m.stages[m.index]
	m.steps.Reset(stage.step)
	value, answered := m.answers[m.index]
	if !answered && stage.initial != nil {
		value = stage.initial()
	}
	if stage.kind == flowChoose {
		m.options, m.cursor = stage.options(), 0
		for i, option := range m.options {
			if value != "" && option.value == value {
				m.cursor = i
			}
		}
		return nil
	}
	m.input = textinput.New()
	m.input.Prompt = "> "
	m.input.Placeholder = stage.placeholder
	styles := m.input.Styles()
	styles.Focused.Prompt = m.style.styles[cyan]
	styles.Focused.Placeholder = m.style.styles[dim]
	styles.Focused.Text = m.style.styles[plain]
	m.input.SetStyles(styles)
	if stage.kind == flowSecret {
		m.input.EchoMode, m.input.EchoCharacter = textinput.EchoPassword, '•'
	}
	if m.width > 8 {
		m.input.SetWidth(m.width - 6)
	}
	m.input.SetValue(value)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *flowModel) runCheck() tea.Cmd {
	stage := m.stages[m.index]
	m.runs++
	id := m.runs
	m.running = id
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.steps.Start(stage.step)
	work := stage.prepare()
	return m.launch(func() tea.Msg {
		result, err := work(ctx)
		return flowCheckMsg{id: id, result: result, err: err}
	})
}

func (m *flowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width > 8 {
			m.input.SetWidth(m.width - 6)
		}
		return m, nil
	case spinner.TickMsg:
		m.now = m.clock()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case flowCheckMsg:
		return m, m.checked(msg)
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	if m.asking() && m.stages[m.index].kind != flowChoose {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// asking reports whether a question is on screen.
func (m *flowModel) asking() bool {
	return !m.finished && m.index < len(m.stages) && m.running == 0 && m.failure == nil && m.stages[m.index].kind != flowCheck
}

func (m *flowModel) checked(msg flowCheckMsg) tea.Cmd {
	if msg.id != m.running || m.finished {
		return nil // a check abandoned by going back or leaving
	}
	m.running = 0
	m.cancel()
	stage := m.stages[m.index]
	if msg.err != nil {
		m.steps.Fail(stage.step, msg.err)
		m.failure = m.newFailure(msg.err)
		return nil
	}
	m.steps.Done(stage.step, stage.accept(msg.result))
	return m.enter(m.index + 1)
}

func (m *flowModel) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m.leave()
	case "shift+tab":
		return m.back()
	}
	switch {
	case m.finished:
		return nil
	case m.failure != nil:
		return m.failureKey(msg)
	case m.running != 0:
		return nil
	}
	if m.stages[m.index].kind == flowChoose {
		switch msg.String() {
		case "up":
			if m.cursor == 0 {
				return m.back()
			}
			m.cursor--
		case "down":
			m.cursor = min(m.cursor+1, len(m.options)-1)
		case "enter":
			if option := m.options[m.cursor]; option.leave {
				return m.leave()
			} else {
				return m.answer(option.value)
			}
		}
		return nil
	}
	switch msg.String() {
	case "up":
		if m.input.Value() == "" {
			return m.back()
		}
	case "enter":
		return m.answer(m.input.Value())
	}
	m.invalid = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *flowModel) answer(value string) tea.Cmd {
	stage := m.stages[m.index]
	if stage.kind != flowChoose {
		value = strings.TrimSpace(value)
	}
	if stage.validate != nil {
		if err := stage.validate(value); err != nil {
			m.invalid = err.Error()
			return nil
		}
	}
	m.answers[m.index] = value
	m.steps.Answer(stage.step, stage.set(value))
	m.history = append(m.history, m.index)
	return m.enter(m.index + 1)
}

// back returns to the last question answered, abandoning any check in
// progress. The rows from that question on are cleared; the stages after it
// run again once it is answered.
func (m *flowModel) back() tea.Cmd {
	if len(m.history) == 0 || m.finished {
		return nil
	}
	target := m.history[len(m.history)-1]
	m.history = m.history[:len(m.history)-1]
	if m.running != 0 {
		m.cancel()
		m.running = 0
	}
	for i := target; i <= m.index && i < len(m.stages); i++ {
		m.steps.Reset(m.stages[i].step)
	}
	m.index, m.failure, m.invalid = target, nil, ""
	return m.ask()
}

func (m *flowModel) leave() tea.Cmd {
	if m.running != 0 {
		m.cancel()
		m.running = 0
	}
	m.finished, m.err = true, service.ErrCancelled
	return tea.Quit
}

const (
	failureRetry = "retry"
	failureBack  = "back"
	failureOpen  = "open"
)

func (m *flowModel) newFailure(err error) *flowFailure {
	failure := &flowFailure{err: err, report: describeFailure(err, m.style.profile == colorprofile.ANSI)}
	failure.options = append(failure.options, flowOption{label: "Retry", value: failureRetry})
	if len(m.history) > 0 {
		failure.options = append(failure.options, flowOption{label: "Go back", value: failureBack})
	}
	if found, ok := problem.Classify(err); ok && found.Fix == problem.FixOpenURL && found.FixURL != "" && m.open != nil {
		failure.fixURL = found.FixURL
		failure.options = append(failure.options, flowOption{label: "Open in browser", detail: found.FixURL, value: failureOpen})
	}
	failure.options = append(failure.options, flowOption{label: "Leave", leave: true})
	return failure
}

// describeFailure is the error as the executable boundary prints it: the
// code, message, hint, and docs link of a catalogued failure, or its plain
// Error line.
func describeFailure(err error, color bool) string {
	var text bytes.Buffer
	report, catalogued := describeError(err)
	_ = writeErrorText(Streams{Err: &text, ColorErr: color}, report, catalogued)
	return strings.TrimRight(text.String(), "\n")
}

func (m *flowModel) failureKey(msg tea.KeyPressMsg) tea.Cmd {
	failure := m.failure
	switch msg.String() {
	case "up":
		if failure.cursor == 0 {
			return m.back()
		}
		failure.cursor--
	case "down":
		failure.cursor = min(failure.cursor+1, len(failure.options)-1)
	case "enter":
		option := failure.options[failure.cursor]
		switch {
		case option.leave:
			return m.leave()
		case option.value == failureRetry:
			m.failure = nil
			return m.runCheck()
		case option.value == failureBack:
			return m.back()
		case option.value == failureOpen:
			if err := m.open(failure.fixURL); err != nil {
				failure.notice = "Could not open a browser; visit " + failure.fixURL
			} else {
				failure.notice = "Opened " + failure.fixURL + "; choose Retry once it is fixed"
			}
			failure.cursor = 0
		}
	}
	return nil
}

// View draws the flow padded to the tallest frame so far. Once the flow has
// ended it keeps the last frame, which runFlow erases before printing the
// final checklist in its place.
func (m *flowModel) View() tea.View {
	if !m.finished || m.frame == "" {
		lines := strings.Split(m.render(), "\n")
		m.height = max(m.height, len(lines))
		for len(lines) < m.height {
			lines = append(lines, "")
		}
		m.frame = strings.Join(lines, "\n")
	}
	return tea.NewView(m.frame)
}

func (m *flowModel) render() string {
	spin := m.style.apply(cyan, strings.TrimSpace(m.spinner.View()))
	active, failed := -1, -1
	if m.index < len(m.stages) {
		switch {
		case m.failure != nil:
			failed = m.stages[m.index].step
		case m.asking():
			active = m.stages[m.index].step
		}
	}
	var lines []string
	for r, row := range m.steps.rows {
		if r == active {
			lines = append(lines, m.question(row.name)...)
			continue
		}
		lines = append(lines, m.fit(drawRow(m.style, spin, row, labelWidth, m.now)))
		if r == failed {
			lines = append(lines, m.failureLines()...)
		}
	}
	lines = append(lines, "", m.style.apply(dim, m.hint()))
	return strings.Join(lines, "\n")
}

func (m *flowModel) question(name string) []string {
	stage := m.stages[m.index]
	title := stage.title
	if title == "" {
		title = name
	}
	lines := []string{m.fit(m.style.apply(cyan, "?") + " " + m.style.apply(bold, singleLine(title)))}
	if stage.note != nil {
		for _, note := range stage.note() {
			lines = append(lines, m.wrap(singleLine(note), dim)...)
		}
	}
	if stage.kind == flowChoose {
		lines = append(lines, m.list(m.options, m.cursor)...)
	} else {
		lines = append(lines, "  "+m.input.View())
	}
	if m.invalid != "" {
		lines = append(lines, m.wrap(singleLine(m.invalid), red)...)
	}
	return lines
}

func (m *flowModel) list(options []flowOption, cursor int) []string {
	lines := make([]string, 0, len(options))
	for i, option := range options {
		label := singleLine(option.label)
		marker := "  "
		if i == cursor {
			marker, label = m.style.apply(cyan, "❯")+" ", m.style.apply(cyan, label)
		}
		line := "  " + marker + label
		if option.detail != "" {
			line += "  " + m.style.apply(dim, singleLine(option.detail))
		}
		lines = append(lines, m.fit(line))
	}
	return lines
}

func (m *flowModel) failureLines() []string {
	var lines []string
	// The report is already styled and sanitized by writeErrorText; passing
	// it through singleLine again would print its escapes as text.
	for _, line := range strings.Split(m.failure.report, "\n") {
		lines = append(lines, m.wrap(line, plain)...)
	}
	if m.failure.notice != "" {
		lines = append(lines, m.wrap(singleLine(m.failure.notice), dim)...)
	}
	return append(lines, m.list(m.failure.options, m.failure.cursor)...)
}

func (m *flowModel) hint() string {
	if len(m.history) == 0 {
		return "esc leave"
	}
	return "shift+tab edit previous · esc leave"
}

// wrap indents text by two columns and wraps it to the terminal's width, so
// a hint is read in full rather than cut. text must already be one line of
// printable text, styled or not.
func (m *flowModel) wrap(text string, l look) []string {
	if m.width > 12 {
		text = ansi.Wrap(text, m.width-4, "")
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "  " + m.style.apply(l, line)
	}
	return lines
}

func (m *flowModel) fit(line string) string {
	if m.width > 0 {
		return ansi.Truncate(line, m.width, "…")
	}
	return line
}

// runFlow runs stages on the terminal as one view under steps, then prints
// the final checklist in its place. Leaving returns service.ErrCancelled;
// when a check had failed, its explanation is printed below the checklist
// first, so the reason stays on screen.
func runFlow(ctx context.Context, streams Streams, steps *Steps, stages []flowStage, open func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	streams = streams.Normalized()
	in, errTerminal, ok := terminalInput(streams)
	if !ok {
		return errors.New("this form needs a terminal")
	}
	style := streams.errPalette()
	steps.Pause()
	model := newFlowModel(ctx, steps, stages, style, terminalWidth(errTerminal), open)
	program := tea.NewProgram(model,
		tea.WithInput(in),
		tea.WithOutput(errTerminal),
		tea.WithoutSignalHandler(),
		tea.WithContext(ctx),
		tea.WithColorProfile(style.colorProfile()))
	_, err := program.Run()
	if model.cancel != nil {
		model.cancel()
	}
	// Bubble Tea leaves the last frame on screen with the cursor on its last
	// row, as Steps.stop describes; every frame is one terminal line per
	// line, so moving up by the height and erasing below removes it all.
	if model.height > 0 {
		erase := "\r"
		if up := model.height - 1; up > 0 {
			erase += fmt.Sprintf("\x1b[%dA", up)
		}
		_, _ = fmt.Fprint(errTerminal, erase+"\x1b[J")
	}
	steps.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("form: %w", err)
	}
	if model.err != nil && model.failure != nil {
		_, _ = fmt.Fprintln(streams.Err, model.failure.report)
	}
	return model.err
}
