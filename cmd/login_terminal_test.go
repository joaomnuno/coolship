//go:build linux

package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// openTerminal opens a pseudo-terminal and returns its two ends: control,
// which the test types into and reads the drawn output from, and terminal,
// which a command sees as its stdin and stderr. The terminal is 80 columns
// wide, so a live view could be drawn on it.
func openTerminal(t *testing.T) (control, terminal *os.File) {
	t.Helper()
	control, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = control.Close() })
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, control.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("unlock pseudo-terminal: %v", errno)
	}
	var number uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, control.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number))); errno != 0 {
		t.Fatalf("pseudo-terminal number: %v", errno)
	}
	terminal, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = terminal.Close() })
	size := struct{ rows, cols, x, y uint16 }{24, 80, 0, 0}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, terminal.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&size))); errno != 0 {
		t.Fatalf("size pseudo-terminal: %v", errno)
	}
	return control, terminal
}

func TestLoginWithJSONOnATerminalAsksLineByLineAndPrintsOnlyTheResult(t *testing.T) {
	control, terminal := openTerminal(t)
	var seen service.LoginOptions
	app := fakeApplication{login: func(_ context.Context, options service.LoginOptions) (service.LoginResult, error) {
		seen = options
		return service.LoginResult{Name: options.Name, URL: options.URL, Team: "Platform", Server: "4.3.18", Path: "/c.json", Default: true}, nil
	}}
	var out bytes.Buffer
	streams := ui.Streams{In: terminal, Out: &out, Err: terminal, Interactive: true, ErrTerminal: true}
	// The same terminal runs the form for a text result, so what follows
	// is the format's doing, not the terminal's.
	if !ui.LoginFormAvailable(streams, "human") || ui.LoginFormAvailable(streams, "json") {
		t.Fatal("the form must run for a text result and only then")
	}
	drawn := make(chan string, 1)
	go func() {
		// Reading ends once the terminal end is closed.
		var buffer bytes.Buffer
		_, _ = io.Copy(&buffer, control)
		drawn <- buffer.String()
	}()
	if _, err := io.WriteString(control, "https://coolify.example.com\nlab\ntyped\n"); err != nil {
		t.Fatal(err)
	}
	root := cmd.NewRootCommand(app, streams, "test")
	root.SetArgs([]string{"login", "--format", "json"})
	err := root.ExecuteContext(context.Background())
	_ = terminal.Close()
	stderr := <-drawn
	if err != nil || seen.URL != "https://coolify.example.com" || seen.Name != "lab" || seen.Token != "typed" {
		t.Fatalf("seen=%+v err=%v stderr=%q", seen, err, stderr)
	}
	for _, mark := range []string{"Instance type", "Check instance", "shift+tab", "✓", "?"} {
		if strings.Contains(stderr, mark) {
			t.Fatalf("the form or its checklist was drawn (%q): %q", mark, stderr)
		}
	}
	if !strings.Contains(stderr, "Coolify URL (https://app.coolify.io for Coolify Cloud):") || !strings.Contains(stderr, "Context name [coolify]:") || !strings.Contains(stderr, "API token:") {
		t.Fatalf("questions were not asked line by line: %q", stderr)
	}
	// Stdout is the result alone: one JSON object on one line.
	var result service.LoginResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || strings.Count(out.String(), "\n") != 1 || result.Name != "lab" || result.Team != "Platform" {
		t.Fatalf("stdout=%q err=%v", out.String(), err)
	}
}
