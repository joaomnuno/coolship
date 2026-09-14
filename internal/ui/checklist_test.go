package ui

import (
	"bytes"
	"os"
	"path/filepath"
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
// unmarked. A compose deployment has no rolling update, so its container is
// not drawn as a child of one.
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
		"    rolling update",
		"  ✓ container                   0:00",
		"    cleanup",
	}, "\n")
	if got := compose.(checklistModel).render(); got != want {
		t.Fatalf("compose view\n got %q\nwant %q", got, want)
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

// liveChecklist is a Checklist drawing on a file, as it would on a
// terminal, so the Bubble Tea program really runs and Close and Finish are
// exercised against it.
func liveChecklist(t *testing.T, out *bytes.Buffer) (*Checklist, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	start := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	now := start
	var clock sync.Mutex
	checklist := NewChecklist(Streams{Out: out, Err: file, Interactive: true}, "human", false)
	checklist.terminal = file
	// Events and the program's spinner ticks both read the clock.
	checklist.clock = func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		now = now.Add(time.Second)
		return now
	}
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"}
	for _, event := range []service.Event{
		{Type: "deployment", DeploymentUUID: "d-1", Status: "queued", Target: &target},
		{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"},
		{Type: "build", DeploymentUUID: "d-1", Logs: "Build step skipped.\n"},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageSkipped, Message: "cached image"},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageRollingUpdate, Status: service.StageStarted},
	} {
		if err := checklist.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	return checklist, path
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
// deployment in a terminal: the live view is replaced by the final
// checklist with one summary line, the URL is the only thing on stdout, and
// the result block the plain renderer prints is not repeated.
func TestChecklistFinishPrintsOneSummaryAndTheURL(t *testing.T) {
	var out bytes.Buffer
	checklist, path := liveChecklist(t, &out)
	result := service.DeployResult{
		Target:         service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"},
		DeploymentUUID: "d-1", Status: "finished", URL: "https://web.example.com", URLKind: "application",
	}
	within(t, func() error { return checklist.Finish(result) })
	if out.String() != "https://web.example.com\n" {
		t.Fatalf("stdout = %q, want only the URL", out.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stderr := string(content)
	summary := "✓ Deployed web to production in 0:0"
	if !strings.HasPrefix(stderr, "→ web\n→ production\n\n") || strings.Count(stderr, summary) != 1 {
		t.Fatalf("stderr lacks the header or one summary line: %q", stderr)
	}
	tail := stderr[strings.LastIndex(stderr, summary):]
	for _, line := range []string{"– build                       skipped (cached image)", "✓ rolling update"} {
		if !strings.Contains(tail, line) {
			t.Fatalf("final checklist lacks %q: %q", line, tail)
		}
	}
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

// TestChecklistCloseAfterAnInterruptRestoresTheTerminal checks what Ctrl-C
// leaves behind: Close returns promptly, the program is gone, the final
// checklist says observation stopped with the open stage marked …, the build
// log stays collapsed, and the cursor Bubble Tea hid is shown again.
func TestChecklistCloseAfterAnInterruptRestoresTheTerminal(t *testing.T) {
	var out bytes.Buffer
	checklist, path := liveChecklist(t, &out)
	within(t, func() error { return checklist.Close(OutcomeStopped) })
	if checklist.program != nil || out.Len() != 0 {
		t.Fatalf("program %v left running or stdout written %q", checklist.program, out.String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stderr := string(content)
	final := stderr[strings.LastIndex(stderr, "✗ Observation stopped"):]
	for _, line := range []string{"  – build", "  … rolling update", "      container"} {
		if !strings.Contains(final, line) {
			t.Fatalf("stopped checklist lacks %q: %q", line, final)
		}
	}
	if strings.Contains(final, "Build step skipped.") {
		t.Fatalf("an interrupt printed the build log: %q", final)
	}
	if hide, show := strings.LastIndex(stderr, "\x1b[?25l"), strings.LastIndex(stderr, "\x1b[?25h"); hide >= 0 && show < hide {
		t.Fatalf("the cursor was left hidden: %q", stderr)
	}
}
