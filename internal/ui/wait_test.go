package ui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWaitRunsFnAndReturnsItsError(t *testing.T) {
	var out, diagnostic bytes.Buffer
	streams := Streams{Out: &out, Err: &diagnostic, Interactive: true, ColorErr: true}
	want := errors.New("server returned 500")
	ran := false
	err := Wait(context.Background(), streams, "Reading application status", func(ctx context.Context) error {
		ran = true
		if ctx == nil || ctx.Err() != nil {
			t.Fatalf("fn received a bad context: %v", ctx)
		}
		return want
	})
	if !ran {
		t.Fatal("fn did not run")
	}
	if !errors.Is(err, want) {
		t.Fatalf("Wait returned %v, want %v", err, want)
	}
	if out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatalf("Wait wrote to non-terminal streams: stdout %q, stderr %q", out.String(), diagnostic.String())
	}
	if err := Wait(context.Background(), streams, "Listing projects", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Wait returned %v for a successful fn", err)
	}
}

func TestWaitPrintsNothingWhenStderrIsAFileButNotATerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stderr")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	streams := Streams{Out: file, Err: file, Interactive: true, ColorErr: true}
	if err := Wait(context.Background(), streams, "Listing deployments", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("Wait wrote %q to a stderr that is not a terminal", content)
	}
}

func TestWaitHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var diagnostic bytes.Buffer
	err := Wait(ctx, Streams{Err: &diagnostic, Interactive: true}, "Reading application status", func(context.Context) error {
		t.Fatal("fn ran under a cancelled context")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait returned %v, want context.Canceled", err)
	}
	if diagnostic.Len() != 0 {
		t.Fatalf("Wait wrote %q", diagnostic.String())
	}
}

func TestSpinnerStopIsSafeWithoutAStart(t *testing.T) {
	var diagnostic bytes.Buffer
	spinner := NewSpinner(Streams{Err: &diagnostic, Interactive: true})
	spinner.Stop()
	spinner.Start("Listing projects")
	spinner.Start("Listing environments")
	spinner.Stop()
	spinner.Stop()
	if diagnostic.Len() != 0 {
		t.Fatalf("spinner wrote %q to a stderr that is not a terminal", diagnostic.String())
	}
	// Streams that are not interactive never spin, terminal or not.
	spinner = NewSpinner(Streams{Err: os.Stderr})
	spinner.Start("Listing projects")
	if spinner.program != nil {
		t.Fatal("a noninteractive stream started a program")
	}
	spinner.Stop()
}

func TestQuietTerminalDropsOnlyTerminalRequests(t *testing.T) {
	for _, test := range []struct{ name, in, want string }{
		{"mode queries", "\x1b[?2026$p\x1b[?2027$p", ""},
		{"cursor position", "\x1b[6n", ""},
		{"device attributes", "\x1b[c\x1b[0c", ""},
		{"terminal version", "\x1b[>q\x1b[>0q", ""},
		{"colour queries", "\x1b]11;?\x1b\\\x1b]10;?\a\x1b]12;?\x1b\\", ""},
		{"query inside a frame", "\x1b[?2026$p\x1b[?25l\x1b[36m⣾\x1b[m Listing projects", "\x1b[?25l\x1b[36m⣾\x1b[m Listing projects"},
		{"frame", "\r\x1b[2K\x1b[1;32mfinished\x1b[m\x1b[?25h\x1b[6;1H", "\r\x1b[2K\x1b[1;32mfinished\x1b[m\x1b[?25h\x1b[6;1H"},
		{"text that looks close", "6n [c >q ]11;? $p", "6n [c >q ]11;? $p"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := string(terminalRequests.ReplaceAll([]byte(test.in), nil)); got != test.want {
				t.Fatalf("filtered %q = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
