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
type Checklist struct {
	streams  Streams
	renderer *Renderer
	terminal *os.File // nil when the plain path is in use
	style    palette
	logs     bool
	clock    func() time.Time

	program *tea.Program
	done    chan struct{}
	final   tea.Model
	build   strings.Builder
}

// NewChecklist prepares the view for one deployment. logs streams the build
// log lines above the checklist as they arrive; otherwise they are kept for
// a failure. Nothing is drawn until the first deployment event.
func NewChecklist(streams Streams, format string, logs bool) *Checklist {
	streams = streams.Normalized()
	c := &Checklist{streams: streams, renderer: NewRenderer(streams, format), style: streams.errPalette(), logs: logs, clock: time.Now}
	if format != "json" {
		c.terminal, _ = drawable(streams)
	}
	return c
}

// Event is the Emitter a deployment workflow reports to.
func (c *Checklist) Event(event service.Event) error {
	if c.terminal == nil {
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
			c.program.Send(stageMsg{stage: event.Stage, status: event.Status, at: now})
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
	model := newChecklistModel(c.style, status, now, c.clock)
	c.program = tea.NewProgram(model,
		tea.WithOutput(quietTerminal{c.terminal}),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
		tea.WithColorProfile(c.style.colorProfile()))
	c.done = make(chan struct{})
	go func(program *tea.Program, done chan struct{}) {
		defer close(done)
		// The program only draws; whatever ends it, the last model is what
		// Close prints, and there is nothing else to report.
		c.final, _ = program.Run()
	}(c.program, c.done)
}

// Close ends the view once the deployment has ended, failed says how. The
// live view is cleared by Bubble Tea, so the final checklist is printed
// plainly in its place and stays on screen; on a failure the build log
// gathered so far follows it, so the log's tail is the last thing before the
// result. It is safe to call when nothing was drawn.
func (c *Checklist) Close(failed bool) error {
	if c.program == nil {
		return nil
	}
	c.program.Send(endMsg{failed: failed, at: c.clock()})
	c.program.Quit()
	<-c.done
	c.program, c.done = nil, nil
	var out strings.Builder
	if model, ok := c.final.(checklistModel); ok {
		out.WriteString(model.render() + "\n")
	}
	if failed && c.build.Len() > 0 {
		out.WriteString("\n")
		out.WriteString(c.build.String())
		if !strings.HasSuffix(c.build.String(), "\n") {
			out.WriteString("\n")
		}
	}
	_, err := io.WriteString(c.streams.Err, out.String())
	return err
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
		at            time.Time
	}
	endMsg struct {
		failed bool
		at     time.Time
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
	stopped bool // ended without the server's verdict
	stages  []stageRow
}

type stageRow struct {
	name    string
	status  string // empty until reached, then started, done, or failed
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
		m.end(msg.failed, msg.at)
	}
	return m, nil
}

// stage records a transition. A stage that ends without having started is
// shown as lasting no time rather than dropped.
func (m *checklistModel) stage(msg stageMsg) {
	m.stages = slices.Clone(m.stages)
	index := slices.IndexFunc(m.stages, func(row stageRow) bool { return row.name == msg.stage })
	if index < 0 {
		m.stages = append(m.stages, stageRow{name: msg.stage})
		index = len(m.stages) - 1
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
	}
}

// stageStopped is a stage that was open when observation stopped before the
// server's verdict; it neither succeeded nor failed.
const stageStopped = "stopped"

// end freezes the view: an open stage ends with the deployment, the way the
// deployment did, since no marker will arrive for it any more. A failure the
// server did not pronounce (a timeout, an interrupt, a lost connection) is
// observation stopping, not the deployment failing, and its open stages are
// left as they were.
func (m *checklistModel) end(failed bool, at time.Time) {
	m.ended, m.failed = true, failed
	m.stopped = failed && m.status != "failed" && m.status != "cancelled-by-user"
	m.stages = slices.Clone(m.stages)
	for index := range m.stages {
		if m.stages[index].status != service.StageStarted {
			continue
		}
		m.stages[index].ended = at
		switch {
		case m.stopped:
			m.stages[index].status = stageStopped
		case failed:
			m.stages[index].status = service.StageFailed
		default:
			m.stages[index].status = service.StageDone
		}
	}
}

// View has no trailing newline, so the renderer erases exactly it when the
// program stops. There is no input, so the terminal mode is left alone.
func (m checklistModel) View() tea.View {
	view := tea.NewView(m.render())
	view.DisableBracketedPasteMode = true
	return view
}

// labelWidth is the column the elapsed times align on, counted from the
// deployment line's glyph; stage names are indented two columns further.
const labelWidth = 30

// render draws the checklist with the stream's palette: a spinner on what is
// open, ✓ and ✗ on what ended, and the stages not reached yet dim.
func (m checklistModel) render() string {
	var out strings.Builder
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
	out.WriteString(glyph + " " + m.style.apply(look, label) + pad(label, labelWidth) + elapsed(m.now.Sub(m.started)))
	for _, row := range m.stages {
		out.WriteString("\n  ")
		switch row.status {
		case service.StageStarted:
			out.WriteString(m.glyph() + " " + row.name + pad(row.name, labelWidth-2) + elapsed(m.now.Sub(row.started)))
		case service.StageDone:
			out.WriteString(m.style.apply(green, "✓") + " " + row.name + pad(row.name, labelWidth-2) + elapsed(row.ended.Sub(row.started)))
		case service.StageFailed:
			out.WriteString(m.style.apply(red, "✗") + " " + row.name + pad(row.name, labelWidth-2) + elapsed(row.ended.Sub(row.started)))
		case stageStopped:
			out.WriteString("… " + row.name + pad(row.name, labelWidth-2) + elapsed(row.ended.Sub(row.started)))
		default:
			out.WriteString(m.style.apply(dim, "  "+row.name))
		}
	}
	return out.String()
}

// glyph is the current spinner frame, one cell wide.
func (m checklistModel) glyph() string {
	return m.style.apply(cyan, strings.TrimSpace(m.spinner.View()))
}

// pad returns the spaces that bring label to width, at least one.
func pad(label string, width int) string {
	return strings.Repeat(" ", max(width-len([]rune(label)), 1))
}

// elapsed formats a duration as m:ss, or h:mm:ss from an hour on, whole
// seconds only.
func elapsed(d time.Duration) string {
	d = max(d, 0).Truncate(time.Second)
	hours, minutes, seconds := int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
