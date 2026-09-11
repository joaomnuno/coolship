package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

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
		"  "+glyph()+" container                   0:02",
		"    cleanup",
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
	update(endMsg{at: at(61)})
	if got, want := view(), frame(
		"✓ Deployed                      1:01",
		"  ✓ build                       0:41",
		"  ✓ rolling update              0:13",
		"  ✓ container                   0:10",
		"  ✓ cleanup                     0:00",
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
	stopped, _ = stopped.Update(endMsg{failed: true, at: start.Add(time.Hour + 2*time.Minute + 3*time.Second)})
	want := strings.Join([]string{
		"✗ Observation stopped           1:02:03",
		"  ✗ build                       0:07",
		"  … rolling update              1:01:54",
		"    container",
		"    cleanup",
	}, "\n")
	if got := stopped.(checklistModel).render(); got != want {
		t.Fatalf("stopped view\n got %q\nwant %q", got, want)
	}
	update(deploymentMsg{status: "failed", at: start.Add(10 * time.Second)})
	update(endMsg{failed: true, at: start.Add(10 * time.Second)})
	want = strings.Join([]string{
		"✗ Deployment failed             0:10",
		"  ✗ build                       0:07",
		"  ✗ rolling update              0:01",
		"    container",
		"    cleanup",
	}, "\n")
	if got := model.(checklistModel).render(); got != want {
		t.Fatalf("failed view\n got %q\nwant %q", got, want)
	}
	var cancelled tea.Model = newChecklistModel(newPalette(false), "in_progress", start, time.Now)
	cancelled, _ = cancelled.Update(deploymentMsg{status: "cancelled-by-user", at: start.Add(3 * time.Second)})
	cancelled, _ = cancelled.Update(endMsg{failed: true, at: start.Add(3 * time.Second)})
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
		if err := checklist.Close(true); err != nil {
			t.Fatal(err)
		}
		want := "Warning: proxy will learn the domain later\nDeployment d-1: queued\nBuilding docker image started.\nStage build: started\nDeployment d-1: failed\n"
		if diagnostic.String() != want {
			t.Fatalf("%s stderr\n got %q\nwant %q", format, diagnostic.String(), want)
		}
	}
}

func TestElapsed(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                                     "0:00",
		-time.Second:                          "0:00",
		1900 * time.Millisecond:               "0:01",
		41 * time.Second:                      "0:41",
		12*time.Minute + 3*time.Second:        "12:03",
		time.Hour + 2*time.Minute:             "1:02:00",
		25*time.Hour + 59*time.Second:         "25:00:59",
		59*time.Minute + 59*time.Second:       "59:59",
		61*time.Minute + 500*time.Millisecond: "1:01:00",
	} {
		if got := elapsed(d); got != want {
			t.Errorf("elapsed(%v) = %q, want %q", d, got, want)
		}
	}
}
