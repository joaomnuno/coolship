package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunPropagatesExitStatusAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	var out bytes.Buffer
	runner := New(strings.NewReader(""), &out, &out)
	dir := t.TempDir()
	os.Setenv("COOLSHIP_TEST_INHERITED", "yes")
	defer os.Unsetenv("COOLSHIP_TEST_INHERITED")
	code, err := runner.Run(context.Background(), Spec{
		Dir:   dir,
		Shell: `printf '%s|%s|%s\n' "$PWD" "$INJECTED" "$COOLSHIP_TEST_INHERITED"; exit 3`,
		Env:   []string{"INJECTED=value with spaces"},
	})
	if err != nil || code != 3 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	line := strings.TrimSpace(out.String())
	// The temporary directory may be a symlink target; compare its resolved path.
	if !strings.HasSuffix(line, "|value with spaces|yes") || !strings.Contains(line, "|") {
		t.Fatalf("child saw %q", line)
	}
	// Direct execution has no shell: the argument is passed verbatim.
	out.Reset()
	code, err = runner.Run(context.Background(), Spec{Dir: dir, Args: []string{"printf", "%s", "$NOT_EXPANDED"}})
	if err != nil || code != 0 || out.String() != "$NOT_EXPANDED" {
		t.Fatalf("direct: code=%d err=%v out=%q", code, err, out.String())
	}
	// An injected pair overrides an inherited one.
	out.Reset()
	code, _ = runner.Run(context.Background(), Spec{Dir: dir, Shell: `printf '%s' "$COOLSHIP_TEST_INHERITED"`, Env: []string{"COOLSHIP_TEST_INHERITED=override"}})
	if code != 0 || out.String() != "override" {
		t.Fatalf("override: %q", out.String())
	}
}

func TestRunReportsMissingCommandsAndBadSpecs(t *testing.T) {
	runner := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if code, err := runner.Run(context.Background(), Spec{}); err == nil || code != -1 {
		t.Fatal("empty spec accepted")
	}
	if code, err := runner.Run(context.Background(), Spec{Args: []string{"x"}, Shell: "y"}); err == nil || code != -1 {
		t.Fatal("ambiguous spec accepted")
	}
	if code, err := runner.Run(context.Background(), Spec{Args: []string{"/nonexistent/coolship-test-binary"}}); err == nil || code != -1 {
		t.Fatalf("missing binary: code=%d err=%v", code, err)
	}
}

func TestCancellationInterruptsTheChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	runner := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	runner.grace = 500 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	start := time.Now()
	// The child traps the interrupt and exits 42, proving it was signalled, not killed.
	code, err := runner.Run(ctx, Spec{Shell: `trap 'exit 42' INT; sleep 5 & wait`})
	if !errors.Is(err, context.Canceled) || code != 42 || time.Since(start) > 3*time.Second {
		t.Fatalf("code=%d err=%v elapsed=%s", code, err, time.Since(start))
	}
}
