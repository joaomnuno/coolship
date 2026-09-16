package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/joaomnuno/coolship/internal/service"
)

// TestChecklistViewFollowsAScriptedDeployment drives the model with the
// messages the Checklist would send for one deployment and reads the view
// at each step, with a fixed clock so the elapsed times are exact.
func TestChecklistViewFollowsAScriptedDeployment(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }
	now := at(0)
	var model tea.Model = newChecklistModel(newPalette(false), "queued", start, func() time.Time { return now })
	update := func(msg tea.Msg) {
		model, _ = model.Update(msg)
	}
	view := func() string { return model.(checklistModel).render() }
	frame := func(lines ...string) string { return strings.Join(lines, "\n") }
	// Each tick advances the spinner; the palette is off, so the glyph is
	// the bare frame.
	glyph := func() string { return strings.TrimSpace(model.(checklistModel).spinner.View()) }

	now = at(4)
	update(spinner.TickMsg{})
	if got, want := view(), frame(
		glyph()+" Deployment queued             0:04",
		"    build",
		"    rolling update",
		"    container",
		"    cleanup",
	); got != want {
		t.Fatalf("queued view\n got %q\nwant %q", got, want)
	}

	update(deploymentMsg{status: "in_progress", at: at(5)})
	update(stageMsg{stage: service.StageBuild, status: service.StageStarted, at: at(6)})
	now = at(20)
	update(spinner.TickMsg{})
	if got, want := view(), frame(
		glyph()+" Deployment in progress        0:20",
		"  "+glyph()+" build                       0:14",
		"    rolling update",
		"    container",
		"    cleanup",
	); got != want {
		t.Fatalf("building view\n got %q\nwant %q", got, want)
	}

	update(stageMsg{stage: service.StageBuild, status: service.StageDone, at: at(47)})
	update(stageMsg{stage: service.StageRollingUpdate, status: service.StageStarted, at: at(47)})
	update(stageMsg{stage: service.StageContainer, status: service.StageStarted, at: at(48)})
	now = at(50)
	update(spinner.TickMsg{})
	if got, want := view(), frame(
		glyph()+" Deployment in progress        0:50",
		"  ✓ build                       0:41",
		"  "+glyph()+" rolling update              0:03",
		"    "+glyph()+" container                 0:02",
		"      cleanup",
	); got != want {
		t.Fatalf("rolling view\n got %q\nwant %q", got, want)
	}

	// Cleanup starts and ends in one line; the rolling update completing
	// accepts the container; the end freezes everything.
	update(stageMsg{stage: service.StageContainer, status: service.StageDone, at: at(58)})
	update(stageMsg{stage: service.StageCleanup, status: service.StageStarted, at: at(59)})
	update(stageMsg{stage: service.StageCleanup, status: service.StageDone, at: at(59)})
	update(stageMsg{stage: service.StageRollingUpdate, status: service.StageDone, at: at(60)})
	update(deploymentMsg{status: "finished", at: at(61)})
	update(endMsg{outcome: OutcomeSucceeded, at: at(61)})
	if got, want := view(), frame(
		"✓ Deployed                      1:01",
		"  ✓ build                       0:41",
		"  ✓ rolling update              0:13",
		"    ✓ container                 0:10",
		"    ✓ cleanup                   0:00",
	); got != want {
		t.Fatalf("finished view\n got %q\nwant %q", got, want)
	}
}

// TestChecklistViewMarksFailures checks a build that fails, an open stage
// that a failed end closes as failed, and an unknown status shown as it is.
func TestChecklistViewMarksFailures(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	var model tea.Model = newChecklistModel(newPalette(false), "queued", start, time.Now)
	update := func(msg tea.Msg) { model, _ = model.Update(msg) }
	update(deploymentMsg{status: "restarting", at: start.Add(time.Second)})
	if got := model.(checklistModel).render(); !strings.HasPrefix(got, strings.TrimSpace(spinner.Dot.Frames[0])+" Deployment restarting         0:01\n") {
		t.Fatalf("unknown status view %q", got)
	}
	update(stageMsg{stage: service.StageBuild, status: service.StageStarted, at: start.Add(2 * time.Second)})
	update(stageMsg{stage: service.StageBuild, status: service.StageFailed, at: start.Add(9 * time.Second)})
	update(stageMsg{stage: service.StageRollingUpdate, status: service.StageStarted, at: start.Add(9 * time.Second)})
	// Observation that stops before the server's verdict (a timeout, an
	// interrupt) is not a failed deployment: the open stage is left as it
	// was, and the line says what happened.
	stopped := model
	stopped, _ = stopped.Update(endMsg{outcome: OutcomeStopped, at: start.Add(time.Hour + 2*time.Minute + 3*time.Second)})
	want := strings.Join([]string{
		"✗ Observation stopped           1:02:03",
		"  ✗ build                       0:07",
		"  … rolling update              1:01:54",
		"      container",
		"      cleanup",
	}, "\n")
	if got := stopped.(checklistModel).render(); got != want {
		t.Fatalf("stopped view\n got %q\nwant %q", got, want)
	}
	update(deploymentMsg{status: "failed", at: start.Add(10 * time.Second)})
	update(endMsg{outcome: OutcomeFailed, at: start.Add(10 * time.Second)})
	want = strings.Join([]string{
		"✗ Deployment failed             0:10",
		"  ✗ build                       0:07",
		"  ✗ rolling update              0:01",
		"      container",
		"      cleanup",
	}, "\n")
	if got := model.(checklistModel).render(); got != want {
		t.Fatalf("failed view\n got %q\nwant %q", got, want)
	}
	var cancelled tea.Model = newChecklistModel(newPalette(false), "in_progress", start, time.Now)
	cancelled, _ = cancelled.Update(deploymentMsg{status: "cancelled-by-user", at: start.Add(3 * time.Second)})
	cancelled, _ = cancelled.Update(endMsg{outcome: OutcomeFailed, at: start.Add(3 * time.Second)})
	if got := cancelled.(checklistModel).render(); !strings.HasPrefix(got, "✗ Deployment cancelled          0:03\n") {
		t.Fatalf("cancelled view %q", got)
	}
}

// TestChecklistIsPlainOffATerminal checks the fallback: on a buffer, every
// event goes through the plain renderer, stage lines included, and Close
// prints nothing, so pipes see what they saw before the checklist existed.
func TestChecklistIsPlainOffATerminal(t *testing.T) {
	var diagnostic bytes.Buffer
	for _, format := range []string{"human", "json"} {
		diagnostic.Reset()
		checklist := NewChecklist(Streams{Err: &diagnostic, Interactive: true}, format, true)
		target := service.TargetInfo{Application: "web", Environment: "production"}
		for _, event := range []service.Event{
			{Type: "warning", Message: "proxy will learn the domain later"},
			{Type: "deployment", DeploymentUUID: "d-1", Status: "queued", Target: &target},
			{Type: "build", DeploymentUUID: "d-1", Logs: "Building docker image started.\n"},
			{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageStarted},
			{Type: "deployment", DeploymentUUID: "d-1", Status: "failed"},
		} {
			if err := checklist.Event(event); err != nil {
				t.Fatal(err)
			}
		}
		if err := checklist.Close(OutcomeFailed); err != nil {
			t.Fatal(err)
		}
		want := "Warning: proxy will learn the domain later\nDeployment d-1: queued\nBuilding docker image started.\nStage build: started\nDeployment d-1: failed\n"
		if diagnostic.String() != want {
			t.Fatalf("%s stderr\n got %q\nwant %q", format, diagnostic.String(), want)
		}
	}
}

// TestChecklistPrintsTheBuildLogOnlyForAFailedDeployment checks what
// replaces the live view when observation ends: the final checklist always,
// and the buffered build log only when the server said the deployment
// failed. Observation that stopped while the deployment was still running
// (a timeout, an interrupt, a poll that failed) keeps the log collapsed.
func TestChecklistPrintsTheBuildLogOnlyForAFailedDeployment(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	log := "Building docker image started.\nBuilding docker image completed.\n"
	for _, test := range []struct {
		name    string
		outcome Outcome
		wantLog bool
	}{
		{"failed prints the log", OutcomeFailed, true},
		{"stopped keeps it collapsed", OutcomeStopped, false},
		{"succeeded keeps it collapsed", OutcomeSucceeded, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			checklist := &Checklist{style: newPalette(false), clock: func() time.Time { return start }}
			checklist.build.WriteString(log)
			model := newChecklistModel(checklist.style, "in_progress", start, func() time.Time { return start })
			model.end(test.outcome, start)
			checklist.final = model
			got := checklist.closing(test.outcome)
			if !strings.HasPrefix(got, model.render()+"\n") {
				t.Fatalf("closing lost the final checklist: %q", got)
			}
			if strings.Contains(got, log) != test.wantLog {
				t.Fatalf("closing(%v) = %q; log present %v, want %v", test.outcome, got, !test.wantLog, test.wantLog)
			}
		})
	}
}

// TestChecklistHoldsTheBuildLogAboveNormalWhenCollapsed checks the plain
// path of an interactive verbose run: status lines print as they arrive,
// build log chunks are held when logs are off, and only a failed deployment
// prints them. With logs on, or off an interactive run, nothing is held.
func TestChecklistHoldsTheBuildLogAboveNormalWhenCollapsed(t *testing.T) {
	const log = "Building docker image started.\n"
	for _, test := range []struct {
		name        string
		interactive bool
		logs        bool
		outcome     Outcome
		wantLog     bool
	}{
		{"collapsed while running", true, false, OutcomeSucceeded, false},
		{"collapsed when stopped", true, false, OutcomeStopped, false},
		{"printed on failure", true, false, OutcomeFailed, true},
		{"streamed with logs on", true, true, OutcomeSucceeded, true},
		{"streamed when not interactive", false, false, OutcomeSucceeded, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var diagnostic bytes.Buffer
			checklist := NewChecklist(Streams{Err: &diagnostic, Interactive: test.interactive, Trace: leveled(VerbosityVerbose)}, "human", test.logs)
			for _, event := range []service.Event{
				{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"},
				{Type: "build", DeploymentUUID: "d-1", Logs: log},
			} {
				if err := checklist.Event(event); err != nil {
					t.Fatal(err)
				}
			}
			if err := checklist.Close(test.outcome); err != nil {
				t.Fatal(err)
			}
			got := diagnostic.String()
			if !strings.HasPrefix(got, "Deployment d-1: in_progress (0:00)\n") || strings.Contains(got, log) != test.wantLog {
				t.Fatalf("stderr = %q; log wanted %v", got, test.wantLog)
			}
		})
	}
}

// TestChecklistShowsTimingsAboveNormal checks that a verbose or debug run,
// which draws no checklist, still shows what the checklist would have
// timed: each finished stage's duration and the elapsed time on every
// deployment status line. JSON and normal runs print the lines unchanged.
func TestChecklistShowsTimingsAboveNormal(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	events := []struct {
		seconds int
		event   service.Event
	}{
		{0, service.Event{Type: "deployment", DeploymentUUID: "d-1", Status: "queued"}},
		{5, service.Event{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"}},
		{6, service.Event{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageStarted}},
		{47, service.Event{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageDone}},
		{59, service.Event{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageCleanup, Status: service.StageDone}},
		{61, service.Event{Type: "warning", Message: "slow proxy"}},
		{61, service.Event{Type: "deployment", DeploymentUUID: "d-1", Status: "finished"}},
	}
	plain := "Deployment d-1: queued\nDeployment d-1: in_progress\nStage build: started\nStage build: done\nStage cleanup: done\nWarning: slow proxy\nDeployment d-1: finished\n"
	for _, test := range []struct {
		name   string
		level  Verbosity
		format string
		want   string
	}{
		{"verbose", VerbosityVerbose, "human", "Deployment d-1: queued (0:00)\nDeployment d-1: in_progress (0:05)\nStage build: started\nStage build: done (0:41)\nStage cleanup: done (0:00)\nWarning: slow proxy\nDeployment d-1: finished (1:01)\n"},
		{"debug", VerbosityDebug, "human", "Deployment d-1: queued (0:00)\nDeployment d-1: in_progress (0:05)\nStage build: started\nStage build: done (0:41)\nStage cleanup: done (0:00)\nWarning: slow proxy\nDeployment d-1: finished (1:01)\n"},
		{"normal", VerbosityNormal, "human", plain},
		{"json", VerbosityVerbose, "json", plain},
	} {
		t.Run(test.name, func(t *testing.T) {
			var diagnostic bytes.Buffer
			now := start
			checklist := NewChecklist(Streams{Err: &diagnostic, Trace: leveled(test.level)}, test.format, true)
			checklist.clock = func() time.Time { return now }
			for _, step := range events {
				now = start.Add(time.Duration(step.seconds) * time.Second)
				if err := checklist.Event(step.event); err != nil {
					t.Fatal(err)
				}
			}
			if err := checklist.Close(OutcomeSucceeded); err != nil {
				t.Fatal(err)
			}
			if got := diagnostic.String(); got != test.want {
				t.Fatalf("stderr\n got %q\nwant %q", got, test.want)
			}
		})
	}
}

// TestChecklistViewShowsSkippedStages checks a build Coolify skipped for a
// cached image and the summary line a finished deployment ends with. Only
// the skip marker marks a stage skipped; a stage with no marker stays
// unmarked. A compose deployment has no rolling update: its container is
// not drawn as a child of one, and the rolling update row reads not used
// rather than staying dim as if still to come.
func TestChecklistViewShowsSkippedStages(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }
	var model tea.Model = newChecklistModel(newPalette(false), "in_progress", start, time.Now)
	update := func(msg tea.Msg) { model, _ = model.Update(msg) }
	update(stageMsg{stage: service.StageBuild, status: service.StageSkipped, note: "cached image", at: at(3)})
	update(stageMsg{stage: service.StageRollingUpdate, status: service.StageStarted, at: at(4)})
	update(stageMsg{stage: service.StageContainer, status: service.StageStarted, at: at(5)})
	update(stageMsg{stage: service.StageContainer, status: service.StageDone, at: at(20)})
	update(stageMsg{stage: service.StageRollingUpdate, status: service.StageDone, at: at(22)})
	update(endMsg{outcome: OutcomeSucceeded, subject: "web to production", at: at(23)})
	want := strings.Join([]string{
		"✓ Deployed web to production in 0:23",
		"  – build                       skipped (cached image)",
		"  ✓ rolling update              0:18",
		"    ✓ container                 0:15",
		"      cleanup",
	}, "\n")
	if got := model.(checklistModel).render(); got != want {
		t.Fatalf("skipped view\n got %q\nwant %q", got, want)
	}

	var compose tea.Model = newChecklistModel(newPalette(false), "in_progress", start, time.Now)
	compose, _ = compose.Update(stageMsg{stage: service.StageBuild, status: service.StageDone, at: at(9)})
	compose, _ = compose.Update(stageMsg{stage: service.StageContainer, status: service.StageDone, at: at(9)})
	compose, _ = compose.Update(endMsg{outcome: OutcomeSucceeded, at: at(10)})
	want = strings.Join([]string{
		"✓ Deployed                      0:10",
		"  ✓ build                       0:00",
		"  – rolling update              not used",
		"  ✓ container                   0:00",
		"    cleanup",
	}, "\n")
	if got := compose.(checklistModel).render(); got != want {
		t.Fatalf("compose view\n got %q\nwant %q", got, want)
	}
	// An application with ports mapped to the host removes the old
	// containers before the new one starts; that cleanup says the same.
	var mapped tea.Model = newChecklistModel(newPalette(false), "in_progress", start, time.Now)
	mapped, _ = mapped.Update(stageMsg{stage: service.StageCleanup, status: service.StageStarted, at: at(9)})
	if got := mapped.(checklistModel).render(); !strings.Contains(got, "\n  – rolling update              not used\n") {
		t.Fatalf("mapped ports view\n got %q", got)
	}
	// A rolling update whose marker does come later still runs, and its
	// children move back under it.
	mapped, _ = mapped.Update(stageMsg{stage: service.StageRollingUpdate, status: service.StageStarted, at: at(10)})
	if got := mapped.(checklistModel).render(); !strings.Contains(got, "rolling update              0:0") || !strings.Contains(got, "\n    ") {
		t.Fatalf("late rolling update view\n got %q", got)
	}

	// A build log the token may not read carries no markers: the stages
	// may all have run, so a success must not call any of them skipped.
	var unobserved tea.Model = newChecklistModel(newPalette(false), "in_progress", start, time.Now)
	unobserved, _ = unobserved.Update(endMsg{outcome: OutcomeSucceeded, subject: "web to production", at: at(40)})
	want = strings.Join([]string{
		"✓ Deployed web to production in 0:40",
		"    build",
		"    rolling update",
		"    container",
		"    cleanup",
	}, "\n")
	if got := unobserved.(checklistModel).render(); got != want {
		t.Fatalf("unobserved view\n got %q\nwant %q", got, want)
	}
}

// TestChecklistViewFitsTheTerminalWidth checks that every row is cut to the
// terminal's width, so a narrow terminal never wraps a row and the redraw
// moves over exactly the lines it drew.
func TestChecklistViewFitsTheTerminalWidth(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	var model tea.Model = newChecklistModel(newPalette(true), "in_progress", start, time.Now)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 24, Height: 10})
	model, _ = model.Update(stageMsg{stage: service.StageBuild, status: service.StageSkipped, note: "cached image", at: start})
	model, _ = model.Update(stageMsg{stage: service.StageRollingUpdate, status: service.StageStarted, at: start})
	lines := strings.Split(model.(checklistModel).render(), "\n")
	if len(lines) != 5 {
		t.Fatalf("view has %d lines: %q", len(lines), lines)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > 24 {
			t.Fatalf("line %q is %d columns wide, over 24", line, width)
		}
	}
	if !strings.HasSuffix(ansi.Strip(lines[1]), "…") {
		t.Fatalf("a cut line should end in an ellipsis: %q", lines[1])
	}
}

// terminalRecorder records everything written to it: the escape stream a
// terminal would receive, from the header, the live view's frames, the
// erase, and the final checklist, in the order they were written.
type terminalRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *terminalRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *terminalRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// stageEvent is one stage transition of deployment d-1.
func stageEvent(stage, status string) service.Event {
	return service.Event{Type: "stage", DeploymentUUID: "d-1", Stage: stage, Status: status}
}

// newLiveChecklist is a Checklist drawing on a recorder as it would on a 90
// by 40 terminal, so the Bubble Tea program really draws its frames and
// Close and Finish are exercised against them. The recorder holds the
// whole stream: the header, the frames, the erase, and what replaces the
// view. Nothing is drawn until the first event.
func newLiveChecklist(t *testing.T, out *bytes.Buffer) (*Checklist, *terminalRecorder) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Bubble Tea maps newlines only off Windows; the recorded stream is replayed that way")
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "terminal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	now := start
	var clock sync.Mutex
	recorder := &terminalRecorder{}
	checklist := NewChecklist(Streams{Out: out, Err: recorder, Interactive: true}, "human", false)
	// The file stands for the terminal; the recorder is where it draws.
	checklist.terminal = file
	checklist.display = display{output: recorder, width: 90, height: 40}
	// Events and the program's spinner ticks both read the clock.
	checklist.clock = func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		now = now.Add(time.Second)
		return now
	}
	return checklist, recorder
}

// liveChecklist is newLiveChecklist after the events of a deployment whose
// build was skipped and whose rolling update is open.
func liveChecklist(t *testing.T, out *bytes.Buffer) (*Checklist, *terminalRecorder) {
	t.Helper()
	checklist, recorder := newLiveChecklist(t, out)
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"}
	for _, event := range []service.Event{
		{Type: "deployment", DeploymentUUID: "d-1", Status: "queued", Target: &target},
		{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"},
		{Type: "build", DeploymentUUID: "d-1", Logs: "Build step skipped.\n"},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageSkipped, Message: "cached image"},
		stageEvent(service.StageRollingUpdate, service.StageStarted),
	} {
		if err := checklist.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	return checklist, recorder
}

// screen replays an escape stream the way a terminal would, as far as the
// live views need: text overwrites cells, or inserts them in insert mode;
// \r and \n move as on a terminal that maps newlines, which Bubble Tea
// assumes when it reads no input; the cursor moves up, down, left, right,
// and to a column; lines are erased to their end, the screen to its end;
// lines and cells are inserted and deleted. Colours, modes, and the
// queries a terminal would answer are dropped, and any other sequence is
// an error, so a new one is noticed. What is left is what a person sees.
type screen struct {
	lines    [][]rune
	row, col int
	insert   bool
	last     rune
}

func (s *screen) replay(stream string) error {
	runes := []rune(stream)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '\x1b':
			n, err := s.escape(runes[i+1:])
			if err != nil {
				return err
			}
			i += n
		case '\r':
			s.col = 0
		case '\n':
			s.row, s.col = s.row+1, 0
		case '\b':
			s.col = max(s.col-1, 0)
		case '\a':
		default:
			s.put(r)
		}
	}
	return nil
}

// escape handles what follows an ESC and returns how many runes it took.
func (s *screen) escape(rest []rune) (int, error) {
	if len(rest) == 0 {
		return 0, errors.New("escape at the end of the stream")
	}
	switch rest[0] {
	case '[':
		end := 1
		for end < len(rest) && (rest[end] < 0x40 || rest[end] > 0x7e) {
			end++
		}
		if end == len(rest) {
			return 0, errors.New("unterminated control sequence")
		}
		return end + 1, s.control(string(rest[1:end]), rest[end])
	case ']', 'P', 'X', '^', '_': // a string, up to BEL or ST
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\a' {
				return i + 1, nil
			}
			if rest[i] == '\x1b' && i+1 < len(rest) && rest[i+1] == '\\' {
				return i + 2, nil
			}
		}
		return 0, errors.New("unterminated string")
	case 'M': // reverse index
		s.row = max(s.row-1, 0)
		return 1, nil
	case '7', '8', '=', '>': // save and restore the cursor, keypad modes
		return 1, nil
	}
	return 0, fmt.Errorf("unsupported escape %q", string(rest[0]))
}

// control applies one control sequence, given what came between the
// bracket and the final byte.
func (s *screen) control(params string, final rune) error {
	if params != "" && !strings.ContainsRune("0123456789;", rune(params[0])) || strings.ContainsAny(params, " $\"") {
		return nil // private modes, queries, cursor styles, mode reports
	}
	fields := strings.Split(params, ";")
	arg := func(index, missing int) int {
		if index < len(fields) && fields[index] != "" {
			n, _ := strconv.Atoi(fields[index])
			return n
		}
		return missing
	}
	switch final {
	case 'A':
		s.row = max(s.row-arg(0, 1), 0)
	case 'B':
		s.row += arg(0, 1)
	case 'C':
		s.col += arg(0, 1)
	case 'D':
		s.col = max(s.col-arg(0, 1), 0)
	case 'G', '`':
		s.col = arg(0, 1) - 1
	case 'd':
		s.row = arg(0, 1) - 1
	case 'H', 'f':
		s.row, s.col = arg(0, 1)-1, arg(1, 1)-1
	case 'J':
		if arg(0, 0) != 0 {
			return fmt.Errorf("unsupported erase %q", "\x1b["+params+"J")
		}
		s.eraseLine()
		if s.row+1 < len(s.lines) {
			s.lines = s.lines[:s.row+1]
		}
	case 'K':
		if arg(0, 0) != 0 {
			return fmt.Errorf("unsupported erase %q", "\x1b["+params+"K")
		}
		s.eraseLine()
	case 'L':
		s.reach(s.row)
		s.lines = slices.Insert(s.lines, s.row, make([][]rune, arg(0, 1))...)
	case 'M':
		s.reach(s.row)
		s.lines = slices.Delete(s.lines, s.row, min(s.row+arg(0, 1), len(s.lines)))
	case '@':
		s.reach(s.row)
		if line := s.lines[s.row]; s.col < len(line) {
			s.lines[s.row] = slices.Insert(line, s.col, []rune(strings.Repeat(" ", arg(0, 1)))...)
		}
	case 'P':
		s.reach(s.row)
		if line := s.lines[s.row]; s.col < len(line) {
			s.lines[s.row] = slices.Delete(line, s.col, min(s.col+arg(0, 1), len(line)))
		}
	case 'X':
		s.reach(s.row)
		for i, line := 0, s.lines[s.row]; i < arg(0, 1) && s.col+i < len(line); i++ {
			line[s.col+i] = ' '
		}
	case 'b':
		for i := 0; i < arg(0, 1); i++ {
			s.put(s.last)
		}
	case 'h', 'l':
		if params == "4" {
			s.insert = final == 'h'
		}
	case 'm', 'n', 'c', 'q', 's', 'u', 't', 'p', 'r':
	default:
		return fmt.Errorf("unsupported control sequence %q", "\x1b["+params+string(final))
	}
	return nil
}

// reach makes sure the screen has a line at row.
func (s *screen) reach(row int) {
	for len(s.lines) <= row {
		s.lines = append(s.lines, nil)
	}
}

func (s *screen) put(r rune) {
	s.reach(s.row)
	line := s.lines[s.row]
	for len(line) < s.col {
		line = append(line, ' ')
	}
	switch {
	case s.insert && s.col < len(line):
		line = slices.Insert(line, s.col, r)
	case s.col < len(line):
		line[s.col] = r
	default:
		line = append(line, r)
	}
	s.lines[s.row], s.col, s.last = line, s.col+1, r
}

// eraseLine erases the line from the cursor to its end.
func (s *screen) eraseLine() {
	if s.row < len(s.lines) && s.col < len(s.lines[s.row]) {
		s.lines[s.row] = s.lines[s.row][:s.col]
	}
}

// text is what the screen shows, without trailing spaces or blank lines.
func (s *screen) text() string {
	lines := make([]string, len(s.lines))
	for i, line := range s.lines {
		lines[i] = strings.TrimRight(string(line), " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// lastErase splits a stream at the erase Close writes over the live view:
// the last erase to the end of the screen, right after a carriage return
// and the move up. It returns the stream before that move, how many rows
// the move went up, and what was written after the erase.
func lastErase(t *testing.T, stream string) (before string, up int, after string) {
	t.Helper()
	index := strings.LastIndex(stream, "\x1b[J")
	if index < 0 {
		t.Fatalf("no erase in %q", stream)
	}
	before, after = stream[:index], stream[index+len("\x1b[J"):]
	match := regexp.MustCompile(`\r(?:\x1b\[(\d+)A)?$`).FindStringSubmatch(before)
	if match == nil {
		t.Fatalf("the last erase does not follow a move up: %q", before)
	}
	if match[1] != "" {
		up, _ = strconv.Atoi(match[1])
	}
	return before[:len(before)-len(match[0])], up, after
}

// headerRows is what the header of a live checklist takes: the application,
// the environment, and a blank line, so the frame begins on the fourth row.
const headerRows = 3

// checkOneChecklist replays the stream of a live checklist and checks what
// it leaves on screen: the header, then the final checklist once, with the
// erase before it having moved up over exactly the rows the last frame
// drew. Bubble Tea leaves the cursor on the frame's last row, so the frame's
// height is read from there. Elapsed times depend on how often the spinner
// ticked, so they are compared as m:ss.
func checkOneChecklist(t *testing.T, stream string, want ...string) {
	t.Helper()
	before, up, _ := lastErase(t, stream)
	var live screen
	if err := live.replay(before); err != nil {
		t.Fatal(err)
	}
	if drawn := live.row - headerRows + 1; up != drawn-1 {
		t.Fatalf("the erase moved up %d rows over a frame of %d rows: %q", up, drawn, before)
	}
	var whole screen
	if err := whole.replay(stream); err != nil {
		t.Fatal(err)
	}
	got := regexp.MustCompile(`\d+:\d\d`).ReplaceAllString(whole.text(), "m:ss")
	if got != strings.Join(want, "\n") {
		t.Fatalf("screen\n got %q\nwant %q\nstream %q", got, strings.Join(want, "\n"), stream)
	}
}

// within fails the test when fn does not return in time: a view that does
// not stop would hang the command it belongs to.
func within(t *testing.T, fn func() error) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- fn() }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the checklist did not stop")
	}
}

// TestChecklistFinishPrintsOneSummaryAndTheURL checks the end of a finished
// deployment in a terminal: the live view is erased and replaced by the
// final checklist with one summary line, which the screen then shows once,
// the URL is the only thing on stdout, and the result block the plain
// renderer prints is not repeated.
func TestChecklistFinishPrintsOneSummaryAndTheURL(t *testing.T) {
	var out bytes.Buffer
	checklist, recorder := liveChecklist(t, &out)
	result := service.DeployResult{
		Target:         service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"},
		DeploymentUUID: "d-1", Status: "finished", URL: "https://web.example.com", URLKind: "application",
	}
	within(t, func() error { return checklist.Finish(result) })
	if out.String() != "https://web.example.com\n" {
		t.Fatalf("stdout = %q, want only the URL", out.String())
	}
	stderr := recorder.String()
	if !strings.HasPrefix(stderr, "→ web\n→ production\n\n") {
		t.Fatalf("stderr lacks the header: %q", stderr)
	}
	checkOneChecklist(t, stderr,
		"→ web",
		"→ production",
		"",
		"✓ Deployed web to production in m:ss",
		"  – build                       skipped (cached image)",
		"  ✓ rolling update              m:ss",
		"      container",
		"      cleanup",
	)
	for _, repeated := range []string{"Deployment:", "Application:", "Status:", "app-1"} {
		if strings.Contains(stderr+out.String(), repeated) {
			t.Fatalf("output repeats %q: stderr %q stdout %q", repeated, stderr, out.String())
		}
	}
	// The summary names a preview's pull request.
	if got := deployedSubject(service.DeployResult{PullRequest: 7, Target: result.Target}); got != "pull request #7 of web to production" {
		t.Fatalf("preview subject %q", got)
	}
}

// TestChecklistErasesExactlyTheLastFrame checks the erase that ends the
// live view against the frames Bubble Tea drew: it moves up over as many
// rows as the last frame had, no more and no fewer, so what stays on screen
// is the final checklist once. A row that the first frame did not have (a
// stage the checklist did not list, added when its marker came) makes the
// last frame taller than the first; a compose deployment ends with its
// rolling update row reading not used rather than dim.
func TestChecklistErasesExactlyTheLastFrame(t *testing.T) {
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"}
	opening := []service.Event{
		{Type: "deployment", DeploymentUUID: "d-1", Status: "queued", Target: &target},
		{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"},
	}
	for _, test := range []struct {
		name   string
		events []service.Event
		want   []string
	}{
		{"late row", []service.Event{
			{Type: "build", DeploymentUUID: "d-1", Logs: "Build step skipped.\n"},
			{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageSkipped, Message: "cached image"},
			stageEvent(service.StageRollingUpdate, service.StageStarted),
			stageEvent(service.StageContainer, service.StageStarted),
			stageEvent(service.StageContainer, service.StageDone),
			stageEvent(service.StageCleanup, service.StageStarted),
			stageEvent(service.StageCleanup, service.StageDone),
			stageEvent(service.StageRollingUpdate, service.StageDone),
			stageEvent("migrate", service.StageStarted),
		}, []string{
			"✓ Deployed web to production in m:ss",
			"  – build                       skipped (cached image)",
			"  ✓ rolling update              m:ss",
			"    ✓ container                 m:ss",
			"    ✓ cleanup                   m:ss",
			"  ✓ migrate                     m:ss",
		}},
		{"compose", []service.Event{
			{Type: "build", DeploymentUUID: "d-1", Logs: "Pulling & building required images.\n"},
			stageEvent(service.StageBuild, service.StageStarted),
			{Type: "build", DeploymentUUID: "d-1", Logs: "New container started.\n"},
			stageEvent(service.StageBuild, service.StageDone),
			stageEvent(service.StageContainer, service.StageStarted),
			stageEvent(service.StageContainer, service.StageDone),
			stageEvent(service.StageCleanup, service.StageStarted),
			stageEvent(service.StageCleanup, service.StageDone),
		}, []string{
			"✓ Deployed web to production in m:ss",
			"  ✓ build                       m:ss",
			"  – rolling update              not used",
			"  ✓ container                   m:ss",
			"  ✓ cleanup                     m:ss",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			checklist, recorder := newLiveChecklist(t, &out)
			for _, event := range slices.Concat(opening, test.events) {
				if err := checklist.Event(event); err != nil {
					t.Fatal(err)
				}
			}
			// Frames are flushed on a timer; let the last one be drawn
			// before the end, as a deployment's last poll leaves time for.
			time.Sleep(100 * time.Millisecond)
			result := service.DeployResult{Target: target, DeploymentUUID: "d-1", Status: "finished", URL: "https://web.example.com", URLKind: "application"}
			within(t, func() error { return checklist.Finish(result) })
			checkOneChecklist(t, recorder.String(), slices.Concat([]string{"→ web", "→ production", ""}, test.want)...)
			if out.String() != "https://web.example.com\n" {
				t.Fatalf("stdout = %q, want only the URL", out.String())
			}
		})
	}
}

// TestChecklistCloseAfterAFailurePrintsTheChecklistOnceThenTheLog checks
// the failure shape on a terminal: the live view is erased, the final
// checklist is printed once, and the build log follows it, so its tail is
// right before the deployment page the command names next.
func TestChecklistCloseAfterAFailurePrintsTheChecklistOnceThenTheLog(t *testing.T) {
	var out bytes.Buffer
	checklist, recorder := liveChecklist(t, &out)
	if err := checklist.Event(service.Event{Type: "deployment", DeploymentUUID: "d-1", Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	within(t, func() error { return checklist.Close(OutcomeFailed) })
	checkOneChecklist(t, recorder.String(),
		"→ web",
		"→ production",
		"",
		"✗ Deployment failed             m:ss",
		"  – build                       skipped (cached image)",
		"  ✗ rolling update              m:ss",
		"      container",
		"      cleanup",
		"",
		"Build step skipped.",
	)
}

// TestChecklistCloseAfterAnInterruptRestoresTheTerminal checks what Ctrl-C
// leaves behind: Close returns promptly, the program is gone, the final
// checklist says observation stopped with the open stage marked …, once,
// the build log stays collapsed, and the cursor Bubble Tea hid is shown
// again.
func TestChecklistCloseAfterAnInterruptRestoresTheTerminal(t *testing.T) {
	var out bytes.Buffer
	checklist, recorder := liveChecklist(t, &out)
	within(t, func() error { return checklist.Close(OutcomeStopped) })
	if checklist.program != nil || out.Len() != 0 {
		t.Fatalf("program %v left running or stdout written %q", checklist.program, out.String())
	}
	stderr := recorder.String()
	checkOneChecklist(t, stderr,
		"→ web",
		"→ production",
		"",
		"✗ Observation stopped           m:ss",
		"  – build                       skipped (cached image)",
		"  … rolling update              m:ss",
		"      container",
		"      cleanup",
	)
	if hide, show := strings.LastIndex(stderr, "\x1b[?25l"), strings.LastIndex(stderr, "\x1b[?25h"); hide >= 0 && show < hide {
		t.Fatalf("the cursor was left hidden: %q", stderr)
	}
}

// TestChecklistFrameKeepsTheLastRowsOfAShortTerminal checks the frame a
// terminal with fewer rows than the checklist gets: the last rows that fit,
// which is what Bubble Tea keeps of a taller frame, so the erase that ends
// the view moves over the rows that were drawn. The final checklist printed
// in its place is still complete.
func TestChecklistFrameKeepsTheLastRowsOfAShortTerminal(t *testing.T) {
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	var model tea.Model = newChecklistModel(newPalette(false), "queued", start, time.Now)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 3})
	frame := model.(checklistModel).frame()
	if want := []string{"    rolling update", "    container", "    cleanup"}; !slices.Equal(frame, want) {
		t.Fatalf("frame = %q, want %q", frame, want)
	}
	if got := model.(checklistModel).render(); strings.Count(got, "\n") != 4 {
		t.Fatalf("render lost rows: %q", got)
	}
	if got := eraseView(len(frame)); got != "\r\x1b[2A\x1b[J" {
		t.Fatalf("eraseView(3) = %q", got)
	}
	if got := eraseView(1); got != "\r\x1b[J" {
		t.Fatalf("eraseView(1) = %q", got)
	}
}
