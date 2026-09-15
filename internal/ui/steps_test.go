package ui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// fixedClock is a clock that moves only when advance is called, since the
// running view reads it on every spinner tick too.
func fixedClock() (func() time.Time, func()) {
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	advance := func() {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
	}
	return clock, advance
}

// TestStepsViewDrawsLikeTheDeployChecklist checks the rows against deploy's:
// the same glyphs, the times on the same column, a detail dimmed after the
// time, a skip reason in parentheses, and the steps not reached dim.
func TestStepsViewDrawsLikeTheDeployChecklist(t *testing.T) {
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	at := func(seconds int) time.Time { return start.Add(time.Duration(seconds) * time.Second) }
	var model tea.Model = stepsModel{spinner: spinner.New(spinner.WithSpinner(spinner.Dot)), style: newPalette(false), clock: func() time.Time { return at(9) }}
	model, _ = model.Update(stepsMsg{at: at(9), rows: []stageRow{
		{name: "Request stop", status: "done", detail: "Application stopping request queued.", started: at(0), ended: at(1)},
		{name: "Wait for exited", status: "started", detail: "running:healthy", started: at(1)},
		{name: "Tidy up", status: "skipped", note: "nothing left"},
		{name: "Report"},
	}})
	glyph := strings.TrimSpace(spinner.Dot.Frames[0])
	want := strings.Join([]string{
		"✓ Request stop                  0:01  Application stopping request queued.",
		glyph + " Wait for exited               0:08  running:healthy",
		"– Tidy up                       skipped (nothing left)",
		"  Report",
	}, "\n")
	if got := model.(stepsModel).render(); got != want {
		t.Fatalf("steps view\n got %q\nwant %q", got, want)
	}
	// The time column is deploy's: its header line pads to the same width.
	deploy := newChecklistModel(newPalette(false), "queued", start, time.Now)
	deploy.now = at(1)
	if header := strings.SplitN(deploy.render(), "\n", 2)[0]; strings.Index(header, "0:01") != strings.Index(want, "0:01") {
		t.Fatalf("times do not align with deploy's: %q and %q", header, want)
	}
}

// TestStepsArePlainOffATerminal checks the fallback lines, the same for
// human and JSON output: one line per transition, none for a note.
func TestStepsArePlainOffATerminal(t *testing.T) {
	for _, format := range []string{"human", "json"} {
		var diagnostic bytes.Buffer
		steps := NewSteps(Streams{Err: &diagnostic, Interactive: true}, format, []string{"Compare variables", "Write variables", "Report"})
		if steps.Live() {
			t.Fatal("a buffer is not a terminal")
		}
		steps.Start(0)
		steps.Note(0, "reading")
		steps.Done(0, "1 to create")
		steps.Start(1)
		steps.Fail(1, errors.New("boom"))
		steps.Skip(2, "failed before it")
		steps.Pause()
		steps.Resume()
		steps.Close()
		want := "Step Compare variables: started\nStep Compare variables: done\nStep Write variables: started\nStep Write variables: failed\nStep Report: skipped\n"
		if diagnostic.String() != want {
			t.Fatalf("%s stderr\n got %q\nwant %q", format, diagnostic.String(), want)
		}
	}
}

// liveSteps is a Steps drawing on a file, as it would on a terminal, so the
// Bubble Tea program really runs.
func liveSteps(t *testing.T, titles ...string) (*Steps, *os.File, func()) {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	steps := NewSteps(Streams{Err: file, Interactive: true}, "human", titles)
	steps.terminal = file
	var advance func()
	steps.clock, advance = fixedClock()
	return steps, file, advance
}

func readFile(t *testing.T, file *os.File) string {
	t.Helper()
	content, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// TestStepsPauseKeepsEndedStepsAboveThePrompt runs a prompt between two
// steps: the finished step is printed once, before the prompt, and the
// final checklist after it holds only the steps that came later.
func TestStepsPauseKeepsEndedStepsAboveThePrompt(t *testing.T) {
	steps, file, advance := liveSteps(t, "Compare variables", "Write variables")
	within(t, func() error {
		steps.Start(0)
		advance()
		steps.Done(0, "1 to create")
		steps.Pause()
		if steps.program != nil {
			t.Error("the view kept running through the prompt")
		}
		if _, err := file.WriteString("Confirm [y/N]: y\n"); err != nil {
			return err
		}
		steps.Resume()
		steps.Start(1)
		advance()
		steps.Done(1, "NEW")
		steps.Close()
		steps.Close() // a second Close is harmless
		return nil
	})
	stderr := readFile(t, file)
	prompt := strings.Index(stderr, "Confirm [y/N]: y\n")
	if prompt < 0 {
		t.Fatalf("prompt missing: %q", stderr)
	}
	before, after := stderr[:prompt], stderr[prompt:]
	if !strings.Contains(before, "✓ Compare variables             0:01  1 to create\n") {
		t.Fatalf("the finished step is not above the prompt: %q", before)
	}
	if strings.Contains(after, "Compare variables") {
		t.Fatalf("the finished step was printed again after the prompt: %q", after)
	}
	final := after[strings.LastIndex(after, "✓ Write variables"):]
	if final != "✓ Write variables               0:01  NEW\n" {
		t.Fatalf("final checklist %q", final)
	}
	if hide, show := strings.LastIndex(stderr, "\x1b[?25l"), strings.LastIndex(stderr, "\x1b[?25h"); hide >= 0 && show < hide {
		t.Fatalf("the cursor was left hidden: %q", stderr)
	}
}

// TestStepsCloseMarksWhatWasLeftOpen checks an interrupt and a failure: an
// interrupted step is … and not ✗, a step still open at Close is …, and the
// steps never reached stay dim in the final checklist. Nothing is printed
// when no step was ever reached.
func TestStepsCloseMarksWhatWasLeftOpen(t *testing.T) {
	steps, file, advance := liveSteps(t, "Request stop", "Wait for exited", "Report")
	within(t, func() error {
		steps.Start(0)
		advance()
		steps.Fail(0, context.Canceled)
		steps.Start(1)
		advance()
		steps.Close()
		return nil
	})
	stderr := readFile(t, file)
	final := stderr[strings.LastIndex(stderr, "… Request stop"):]
	for _, line := range []string{"… Request stop                  0:01\n", "… Wait for exited               0:01\n", "  Report\n"} {
		if !strings.Contains(final, line) {
			t.Fatalf("final checklist lacks %q: %q", line, final)
		}
	}

	untouched, file, _ := liveSteps(t, "Compare variables")
	within(t, func() error {
		untouched.Pause()
		untouched.Resume()
		untouched.Close()
		return nil
	})
	if stderr := readFile(t, file); stderr != "" {
		t.Fatalf("steps never reached printed %q", stderr)
	}

	failed, file, advance := liveSteps(t, "Write variables")
	within(t, func() error {
		failed.Start(0)
		advance()
		failed.Fail(0, errors.New("server said no"))
		failed.Close()
		return nil
	})
	if stderr := readFile(t, file); !strings.HasSuffix(stderr, "✗ Write variables               0:01\n") || strings.Contains(stderr, "server said no") {
		t.Fatalf("failed step %q", stderr)
	}
}
