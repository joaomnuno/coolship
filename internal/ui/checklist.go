package ui

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/joaomnuno/coolship/internal/service"
)

// Checklist is the terminal experience of one deployment: the target, then
// the deployment and its stages as a checklist that ticks as Coolify's log
// markers arrive, with a spinner and elapsed time on whatever is open. It is
// a Bubble Tea program on stderr, driven like Spinner: no input, no signal
// handler, an explicit colour profile, and the quietTerminal writer. The
// build log is collapsed unless log streaming is on, and printed in full
// when the deployment fails, since no command can fetch a past build log.
//
// Off a terminal, or with JSON output, every event goes through
// Renderer.DeploymentEvent exactly as before, so pipes and CI see no change.
// Above normal verbosity no live view is drawn, so the plain stage and
// status lines carry the timings the checklist would have shown instead.
type Checklist struct {
	streams  Streams
	renderer *Renderer
	terminal *os.File // nil when the plain path is in use
	// display is where the live view draws: the terminal, sized when the
	// view starts, unless a test set a recorder first.
	display display
	style   palette
	logs    bool
	// hold keeps build log chunks back on the plain path: an interactive
	// run above normal verbosity draws no checklist, yet --no-logs or a
	// false build_logs still collapse the log until a failure prints it.
	hold bool
	// timed appends durations to the plain lines above normal verbosity:
	// a finished stage's own, and the time since the first event on every
	// deployment status line.
	timed        bool
	started      time.Time
	stageStarted map[string]time.Time
	clock        func() time.Time

	program *tea.Program
	done    chan struct{}
	final   tea.Model
	build   strings.Builder
}

// Outcome is how observation of a deployment ended, which is not the same
// question as whether the command failed: a timeout, an interrupt, or a
// poll that failed all end observation while the deployment carries on, and
// none of them is the deployment failing.
type Outcome int

const (
	// OutcomeSucceeded is the server reporting the deployment finished.
	OutcomeSucceeded Outcome = iota
	// OutcomeFailed is the server's verdict that the deployment failed or
	// was cancelled. It is the only outcome that prints the build log.
	OutcomeFailed
	// OutcomeStopped is observation ending before the server pronounced
	// anything; the deployment is still queued or running.
	OutcomeStopped
)

// NewChecklist prepares the view for one deployment. logs streams the build
// log lines above the checklist as they arrive; otherwise they are kept for
// a failure. Nothing is drawn until the first deployment event.
func NewChecklist(streams Streams, format string, logs bool) *Checklist {
	streams = streams.Normalized()
	c := &Checklist{streams: streams, renderer: NewRenderer(streams, format), style: streams.errPalette(), logs: logs, clock: time.Now}
	if format != "json" {
		c.terminal, _ = drawable(streams)
		above := streams.Trace.Level() != VerbosityNormal
		c.hold = c.terminal == nil && !logs && streams.Interactive && above
		c.timed = c.terminal == nil && above
	}
	return c
}

// Event is the Emitter a deployment workflow reports to.
func (c *Checklist) Event(event service.Event) error {
	if c.terminal == nil {
		if c.hold && event.Type == "build" {
			c.build.WriteString(event.Logs)
			return nil
		}
		if c.timed {
			return c.renderer.deploymentEvent(event, c.timing(event))
		}
		return c.renderer.DeploymentEvent(event)
	}
	now := c.clock()
	switch event.Type {
	case "deployment":
		if c.program == nil {
			// The first status opens the view; the target it carries is
			// printed above it, plainly, so it stays when the view is gone.
			if err := c.header(event.Target); err != nil {
				return err
			}
			c.start(event.Status, now)
			return nil
		}
		c.program.Send(deploymentMsg{status: event.Status, at: now})
	case "stage":
		if c.program != nil {
			c.program.Send(stageMsg{stage: event.Stage, status: event.Status, note: event.Message, at: now})
		}
	case "build":
		c.build.WriteString(event.Logs)
		if c.logs && c.program != nil {
			// One message per chunk: Bubble Tea runs each print as its own
			// command, concurrently, so lines sent one by one could land out
			// of order. The renderer scrolls by the lines the body holds.
			c.program.Send(printMsg(strings.TrimSuffix(event.Logs, "\n")))
		}
	case "warning":
		if c.program == nil {
			return c.renderer.DeploymentEvent(event)
		}
		c.program.Send(printMsg(c.style.apply(yellow, "Warning:") + " " + singleLine(event.Message)))
	default:
		if c.program == nil {
			return c.renderer.DeploymentEvent(event)
		}
	}
	return nil
}

// timing is the duration a plain line carries above normal verbosity, in
// the checklist's m:ss form: a finished stage's own duration (0:00 when it
// never reported starting, as the checklist shows it), and the time since
// the first event on a deployment status line. Other lines, a skipped
// stage's included, carry nothing.
func (c *Checklist) timing(event service.Event) string {
	now := c.clock()
	if c.started.IsZero() {
		c.started = now
	}
	switch {
	case event.Type == "stage" && event.Status == service.StageStarted:
		if c.stageStarted == nil {
			c.stageStarted = map[string]time.Time{}
		}
		c.stageStarted[event.Stage] = now
	case event.Type == "stage" && (event.Status == service.StageDone || event.Status == service.StageFailed):
		started, ok := c.stageStarted[event.Stage]
		if !ok {
			started = now
		}
		return " (" + FormatElapsed(now.Sub(started)) + ")"
	case event.Type == "deployment" && event.Message == "" && event.Status != "":
		return " (" + FormatElapsed(now.Sub(c.started)) + ")"
	}
	return ""
}

// header names the target above the checklist: the target when it is a
// named one, then the application and its environment.
func (c *Checklist) header(target *service.TargetInfo) error {
	if target == nil {
		return nil
	}
	lines := []string{target.Application, target.Environment}
	if target.Target != "" && target.Target != "default" {
		lines = append([]string{target.Target}, lines...)
	}
	var out strings.Builder
	for _, line := range lines {
		if line != "" {
			out.WriteString(c.style.apply(cyan, "→") + " " + singleLine(line) + "\n")
		}
	}
	out.WriteString("\n")
	_, err := io.WriteString(c.streams.Err, out.String())
	return err
}

func (c *Checklist) start(status string, now time.Time) {
	if c.display.output == nil {
		c.display = terminalDisplay(c.terminal)
	}
	model := newChecklistModel(c.style, status, now, c.clock)
	model.width, model.height = c.display.width, c.display.height
	c.program, c.done = runView(c.display, c.style, model, &c.final)
}

// display is the terminal a live view draws on: the writer Bubble Tea draws
// through and the terminal's size, which the first frame is cut to. A
// command's display is the stderr terminal behind quietTerminal, sized by
// the terminal; a test's is a recorder with a fixed size, so the frames and
// the erase that ends them can be read back.
type display struct {
	output        io.Writer
	width, height int
}

// terminalDisplay is the display of the terminal a live view may draw on,
// sized by it, 0 by 0 when it does not say. Bubble Tea's size message keeps
// the view fitted after a resize.
func terminalDisplay(terminal *os.File) display {
	width, height, err := term.GetSize(int(terminal.Fd()))
	if err != nil {
		width, height = 0, 0
	}
	return display{output: quietTerminal{terminal}, width: width, height: height}
}

// terminalWidth is the terminal's columns, or 0 when it does not say.
func terminalWidth(terminal *os.File) int {
	return terminalDisplay(terminal).width
}

// runView starts a live view on the display the way every Coolship view
// runs: no input, no signal handler, and an explicit colour profile. done is
// closed once the program has ended, and final then holds its last model.
// The view stays on screen when the program quits; eraseView removes it.
func runView(d display, style palette, model tea.Model, final *tea.Model) (*tea.Program, chan struct{}) {
	program := tea.NewProgram(model,
		tea.WithOutput(d.output),
		tea.WithWindowSize(d.width, d.height),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
		tea.WithColorProfile(style.colorProfile()))
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The program only draws; whatever ends it, the last model is what
		// is printed in its place, and there is nothing else to report.
		*final, _ = program.Run()
	}()
	return program, done
}

// eraseView is what erases a live view of rows once its program has quit,
// written where Bubble Tea left the cursor. Bubble Tea leaves the last
// frame on screen: it moves to the frame's last row and clears only that
// row, so every row above it would stay, and what is printed next would
// repeat them. A frame is one terminal line per row, since rows are cut to
// the width, so the cursor is moved up to the first row and everything from
// there is erased. Hiding the view before quitting does not work: a frame
// that shrinks loses track of the cursor and is cleared from the wrong row.
func eraseView(rows int) string {
	if rows <= 0 {
		return ""
	}
	erase := "\r"
	if up := rows - 1; up > 0 {
		erase += fmt.Sprintf("\x1b[%dA", up)
	}
	return erase + "\x1b[J"
}

// Close ends the view once observation has ended, outcome says how. The
// live view is erased and the final checklist is printed plainly in its
// place, once, where it stays on screen. It is safe to call when nothing
// was drawn.
func (c *Checklist) Close(outcome Outcome) error {
	if c.program == nil {
		if c.hold && outcome == OutcomeFailed && c.build.Len() > 0 {
			return writeLogs(c.streams.Err, c.build.String())
		}
		return nil
	}
	return c.close(outcome, "")
}

// Finish ends observation of a deployment that finished and prints its
// result. When the checklist was drawn, the result is already on screen, so
// its top line becomes one summary, "✓ Deployed web to production in 0:23",
// and the URL follows on stdout as the last line; the Deployment,
// Application, and Status lines would only repeat it. Otherwise (piped,
// JSON, above normal verbosity) the renderer prints the result as always.
func (c *Checklist) Finish(result service.DeployResult) error {
	if c.program == nil {
		if err := c.Close(OutcomeSucceeded); err != nil {
			return err
		}
		return c.renderer.Deploy(result)
	}
	if err := c.close(OutcomeSucceeded, deployedSubject(result)); err != nil {
		return err
	}
	if result.URL == "" {
		return nil
	}
	look := plain
	if result.URLKind == "application" {
		look = green
	}
	_, err := fmt.Fprintln(c.streams.Out, c.streams.outPalette().apply(look, singleLine(result.URL)))
	return err
}

// close stops the running program, erases its last frame, and prints what
// replaces it; subject, when set, names what was deployed on the summary
// line of a success. The last frame is the final model's: a program that
// quits draws the view of its last update before it ends, and the end
// message changes no row count anyway, so its frame says how many rows
// the erase moves over.
func (c *Checklist) close(outcome Outcome, subject string) error {
	c.program.Send(endMsg{outcome: outcome, subject: subject, at: c.clock()})
	c.program.Quit()
	<-c.done
	c.program, c.done = nil, nil
	var out strings.Builder
	if model, ok := c.final.(checklistModel); ok {
		out.WriteString(eraseView(len(model.frame())))
	}
	out.WriteString(c.closing(outcome))
	_, err := io.WriteString(c.streams.Err, out.String())
	return err
}

// deployedSubject is what the summary line says was deployed where: the
// application, or the pull request of it for a preview, and the environment.
// It is empty when the result names no application.
func deployedSubject(result service.DeployResult) string {
	subject := singleLine(result.Target.Application)
	if subject == "" {
		return ""
	}
	if result.PullRequest > 0 {
		subject = fmt.Sprintf("pull request #%d of %s", result.PullRequest, subject)
	}
	if environment := singleLine(result.Target.Environment); environment != "" {
		subject += " to " + environment
	}
	return subject
}

// closing is what Close prints in place of the live view: the final
// checklist, and, when the deployment itself failed, the build log gathered
// so far, so the log's tail is the last thing before the result, since no
// command can fetch a past build log. Observation that merely stopped
// leaves the log collapsed: the deployment has not failed, and dumping
// thousands of lines over an interrupt is what a reader least wants.
func (c *Checklist) closing(outcome Outcome) string {
	var out strings.Builder
	if model, ok := c.final.(checklistModel); ok {
		out.WriteString(model.render() + "\n")
	}
	if outcome == OutcomeFailed && c.build.Len() > 0 {
		out.WriteString("\n")
		out.WriteString(c.build.String())
		if !strings.HasSuffix(c.build.String(), "\n") {
			out.WriteString("\n")
		}
	}
	return out.String()
}

// Messages the Checklist sends its model. Each carries the time it happened,
// so elapsed times are the emitter's clock, not the renderer's.
type (
	deploymentMsg struct {
		status string
		at     time.Time
	}
	stageMsg struct {
		stage, status string
		note          string // why a stage was skipped
		at            time.Time
	}
	endMsg struct {
		outcome Outcome
		subject string
		at      time.Time
	}
	// printMsg is a line to print above the view, where it stays.
	printMsg string
)

// checklistModel is the Bubble Tea view behind Checklist: the deployment
// line, then one line per stage in DeploymentStages order.
type checklistModel struct {
	spinner spinner.Model
	style   palette
	clock   func() time.Time
	started time.Time // submission
	now     time.Time
	status  string
	ended   bool
	failed  bool
	stopped bool   // ended without the server's verdict
	subject string // what the summary line of a success names
	width   int    // the terminal's columns; 0 when unknown
	height  int    // the terminal's rows; 0 when unknown
	stages  []stageRow
}

type stageRow struct {
	name    string
	status  string // empty until reached, then started, done, failed, or skipped
	note    string // why it was skipped, when Coolify said
	detail  string // what a Steps row adds after its time; deploy's stages have none
	answer  bool   // a finished Steps row that shows an answer where its time would be
	started time.Time
	ended   time.Time
}

func newChecklistModel(style palette, status string, now time.Time, clock func() time.Time) checklistModel {
	m := checklistModel{spinner: spinner.New(spinner.WithSpinner(spinner.Dot)), style: style, clock: clock, started: now, now: now, status: status}
	for _, name := range service.DeploymentStages {
		m.stages = append(m.stages, stageRow{name: name})
	}
	return m
}

func (m checklistModel) Init() tea.Cmd { return m.spinner.Tick }

func (m checklistModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.now = m.clock()
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case printMsg:
		return m, tea.Println(string(msg))
	case deploymentMsg:
		m.now = msg.at
		m.status = msg.status
	case stageMsg:
		m.now = msg.at
		m.stage(msg)
	case endMsg:
		m.now = msg.at
		m.subject = msg.subject
		m.end(msg.outcome, msg.at)
	}
	return m, nil
}

// stage records a transition. A stage that ends without having started is
// shown as lasting no time rather than dropped. A child stage reached while
// its parent has no marker of its own says the parent is not used: Coolify
// writes "Rolling update started." before it starts the new container, so a
// container, or a cleanup, that comes first belongs to a deployment with no
// rolling update, and the row would otherwise stay dim as if still to come.
func (m *checklistModel) stage(msg stageMsg) {
	m.stages = slices.Clone(m.stages)
	index := slices.IndexFunc(m.stages, func(row stageRow) bool { return row.name == msg.stage })
	if index < 0 {
		m.stages = append(m.stages, stageRow{name: msg.stage})
		index = len(m.stages) - 1
	}
	if parent := service.ParentStage(msg.stage); parent != "" {
		if p := slices.IndexFunc(m.stages, func(row stageRow) bool { return row.name == parent }); p >= 0 && m.stages[p].status == "" {
			m.stages[p].status = stageNotUsed
		}
	}
	row := &m.stages[index]
	switch msg.status {
	case service.StageStarted:
		row.status, row.started = msg.status, msg.at
	case service.StageDone, service.StageFailed:
		if row.status == "" {
			row.started = msg.at
		}
		row.status, row.ended = msg.status, msg.at
	case service.StageSkipped:
		row.status, row.note = msg.status, msg.note
	}
}

// stageStopped is a stage that was open when observation stopped before the
// server's verdict; it neither succeeded nor failed.
const stageStopped = "stopped"

// stageNotUsed is a stage the deployment had no use for, known from the
// markers of the stages that run inside it: a compose deployment, an
// application with ports mapped to the host, and a preview start the new
// container without a rolling update. It is drawn like a skipped stage,
// since it did not run either, with "not used" in place of the reason.
const stageNotUsed = "not used"

// end freezes the view: an open stage ends with the deployment, the way the
// deployment did, since no marker will arrive for it any more. Observation
// that stopped before the server's verdict (a timeout, an interrupt, a lost
// connection) is not the deployment failing, and its open stages are left
// as they were. A stage with no marker at all stays unmarked even on
// success: only Coolify's own skip line says a stage did not run, and a
// build log the token may not read carries no markers for stages that ran.
func (m *checklistModel) end(outcome Outcome, at time.Time) {
	m.ended = true
	m.failed = outcome != OutcomeSucceeded
	m.stopped = outcome == OutcomeStopped
	m.stages = slices.Clone(m.stages)
	for index := range m.stages {
		row := &m.stages[index]
		if row.status != service.StageStarted {
			continue
		}
		row.ended = at
		switch {
		case m.stopped:
			row.status = stageStopped
		case m.failed:
			row.status = service.StageFailed
		default:
			row.status = service.StageDone
		}
	}
}

// View is the frame, with no trailing newline, so the renderer draws and
// moves over exactly its rows. There is no input, so the terminal mode is
// left alone.
func (m checklistModel) View() tea.View {
	view := tea.NewView(strings.Join(m.frame(), "\n"))
	view.DisableBracketedPasteMode = true
	return view
}

// frame is the rows the live view draws: the checklist's rows, cut to the
// terminal's width, and no more of them than the terminal has rows, the
// last ones, which is what Bubble Tea keeps of a taller frame. Each is one
// terminal line, so the erase that ends the view moves over exactly them.
func (m checklistModel) frame() []string {
	rows := strings.Split(m.render(), "\n")
	if m.height > 0 && len(rows) > m.height {
		rows = rows[len(rows)-m.height:]
	}
	return rows
}

// labelWidth is the column the elapsed times align on, counted from the
// deployment line's glyph; stage names are indented two columns further,
// and a child stage's two more.
const labelWidth = 30

// render draws the checklist with the stream's palette: a spinner on what is
// open, ✓ and ✗ on what ended, – on what was skipped, and the stages not
// reached yet dim. Every line is cut to the terminal's width, so each row is
// one terminal line and the redraw moves over exactly the rows it drew.
func (m checklistModel) render() string {
	lines := make([]string, 0, len(m.stages)+1)
	glyph, label, look := m.glyph(), "", plain
	switch {
	case m.ended && m.stopped:
		glyph, label, look = m.style.apply(red, "✗"), "Observation stopped", redBold
	case m.ended && m.status == "cancelled-by-user":
		glyph, label, look = m.style.apply(red, "✗"), "Deployment cancelled", redBold
	case m.ended && m.failed:
		glyph, label, look = m.style.apply(red, "✗"), "Deployment failed", redBold
	case m.ended:
		glyph, label, look = m.style.apply(green, "✓"), "Deployed", greenBold
	case m.status == "queued":
		label = "Deployment queued"
	case m.status == "in_progress":
		label = "Deployment in progress"
	default:
		label = "Deployment " + singleLine(m.status)
	}
	total := FormatElapsed(m.now.Sub(m.started))
	if m.ended && !m.failed && m.subject != "" {
		lines = append(lines, glyph+" "+m.style.apply(look, label+" "+m.subject+" in "+total))
	} else {
		lines = append(lines, glyph+" "+m.style.apply(look, label)+pad(label, labelWidth)+total)
	}
	for _, row := range m.stages {
		indent, width := "  ", labelWidth-2
		if parent := service.ParentStage(row.name); parent != "" && m.reached(parent) {
			indent, width = "    ", labelWidth-4
		}
		lines = append(lines, indent+drawRow(m.style, m.glyph(), row, width, m.now))
	}
	return fitLines(lines, m.width)
}

// drawRow draws one checklist row, the look deploy's stages and Steps share:
// spin (the spinner frame) on what is open, ✓ and ✗ on what ended, … on what
// observation left open, – on what did not run (skipped, or not used), and
// a dim name for what was not reached yet. The name is padded to width, so
// the times align, and a row's detail, when it has one, follows its time
// dimmed.
func drawRow(style palette, spin string, row stageRow, width int, now time.Time) string {
	var line string
	switch row.status {
	case service.StageStarted:
		line = spin + " " + row.name + pad(row.name, width) + FormatElapsed(now.Sub(row.started))
	case service.StageDone:
		if row.answer {
			// A question's row: how long someone took to answer is noise.
			return style.apply(green, "✓") + " " + row.name + pad(row.name, width) + singleLine(row.detail)
		}
		line = style.apply(green, "✓") + " " + row.name + pad(row.name, width) + FormatElapsed(row.ended.Sub(row.started))
	case service.StageFailed:
		line = style.apply(red, "✗") + " " + row.name + pad(row.name, width) + FormatElapsed(row.ended.Sub(row.started))
	case stageStopped:
		line = "… " + row.name + pad(row.name, width) + FormatElapsed(row.ended.Sub(row.started))
	case service.StageSkipped:
		text := "– " + row.name + pad(row.name, width) + "skipped"
		if row.note != "" {
			text += " (" + singleLine(row.note) + ")"
		}
		return style.apply(dim, text)
	case stageNotUsed:
		return style.apply(dim, "– "+row.name+pad(row.name, width)+stageNotUsed)
	default:
		return style.apply(dim, "  "+row.name)
	}
	if row.detail != "" {
		line += "  " + style.apply(dim, singleLine(row.detail))
	}
	return line
}

// fitLines joins the rows of a live view, each cut to the terminal's width
// when it is known, so each row is one terminal line and the redraw moves
// over exactly the rows it drew.
func fitLines(lines []string, width int) string {
	if width > 0 {
		for index, line := range lines {
			lines[index] = ansi.Truncate(line, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// reached reports whether the named stage has run or is running, which is
// when its children are drawn under it; a stage skipped or not used has
// nothing running inside it.
func (m checklistModel) reached(name string) bool {
	index := slices.IndexFunc(m.stages, func(row stageRow) bool { return row.name == name })
	if index < 0 {
		return false
	}
	status := m.stages[index].status
	return status != "" && status != service.StageSkipped && status != stageNotUsed
}

// glyph is the current spinner frame, one cell wide.
func (m checklistModel) glyph() string {
	return m.style.apply(cyan, strings.TrimSpace(m.spinner.View()))
}

// pad returns the spaces that bring label to width, at least one.
func pad(label string, width int) string {
	return strings.Repeat(" ", max(width-len([]rune(label)), 1))
}
