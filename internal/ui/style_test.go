package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

// sgr is the SGR parameter list each look emits with colour on; the plain
// look emits nothing.
var sgr = map[look]string{bold: "1", dim: "2", red: "31", green: "32", yellow: "33", cyan: "36", redBold: "1;31", greenBold: "1;32"}

func TestPaletteIsNoOpWhenOff(t *testing.T) {
	off, on := newPalette(false), newPalette(true)
	if got := off.apply(redBold, "Error:"); got != "Error:" {
		t.Fatalf("off palette changed text: %q", got)
	}
	if got := on.apply(redBold, "Error:"); got != "\x1b[1;31mError:\x1b[m" {
		t.Fatalf("on palette = %q", got)
	}
	if got := on.apply(plain, "plain"); got != "plain" {
		t.Fatalf("plain look must not wrap: %q", got)
	}
	if got := on.apply(green, ""); got != "" {
		t.Fatalf("empty text must not produce escapes: %q", got)
	}
	if got := on.key("Status"); got != "\x1b[2mStatus:\x1b[m" {
		t.Fatalf("key = %q", got)
	}
}

// TestEveryLookIsPlainWithoutColor is the byte-for-byte rule for each look:
// with colour off Lip Gloss returns the text untouched, and with colour on it
// wraps the text in exactly the sequence the hand-written palette used, with
// escaped tabs and newlines from singleLine left alone.
func TestEveryLookIsPlainWithoutColor(t *testing.T) {
	off, on := newPalette(false), newPalette(true)
	for _, text := range []string{"finished", "Last deployment:", "a\\tb\\nc", "  padded  ", "über"} {
		for l := plain; l < looks; l++ {
			if got := off.apply(l, text); got != text {
				t.Errorf("look %d with colour off rendered %q as %q", l, text, got)
			}
			want := text
			if code, styled := sgr[l]; styled {
				want = "\x1b[" + code + "m" + text + "\x1b[m"
			}
			if got := on.apply(l, text); got != want {
				t.Errorf("look %d with colour on rendered %q as %q, want %q", l, text, got, want)
			}
		}
	}
	if len(sgr) != int(looks)-1 {
		t.Fatalf("sgr covers %d looks, the palette has %d", len(sgr), int(looks)-1)
	}
}

func TestDeploymentStatusStyles(t *testing.T) {
	for status, want := range map[string]look{"queued": dim, "in_progress": cyan, "finished": greenBold, "failed": redBold, "cancelled-by-user": redBold, "something-new": plain} {
		if got := deploymentStatus(status); got != want {
			t.Errorf("deploymentStatus(%q) = %d, want %d", status, got, want)
		}
	}
}

func TestStyledOutputEqualsPlainOutputWithoutColor(t *testing.T) {
	doctor := service.DoctorResult{Checks: []service.Check{
		{Name: "Configuration", Status: "ok", Detail: "coolship.toml"},
		{Name: "Binding", Status: "warning", Detail: "renamed"},
		{Name: "Server", Status: "failed", Detail: "unreachable"},
		{Name: "Application", Status: "skipped"},
		{Name: "Other", Status: "odd"},
	}}
	var plain, styled bytes.Buffer
	if err := NewRenderer(Streams{Out: &plain}, "human").Doctor(doctor); err != nil {
		t.Fatal(err)
	}
	if err := NewRenderer(Streams{Out: &styled, ColorOut: true}, "human").Doctor(doctor); err != nil {
		t.Fatal(err)
	}
	wantPlain := "[ok]   Configuration: coolship.toml\n[warn] Binding: renamed\n[FAIL] Server: unreachable\n[skip] Application\n[odd] Other\n"
	if plain.String() != wantPlain {
		t.Fatalf("plain doctor output = %q", plain.String())
	}
	wantStyled := "\x1b[32m[ok]\x1b[m   Configuration: coolship.toml\n" +
		"\x1b[33m[warn]\x1b[m Binding: renamed\n" +
		"\x1b[1;31m[FAIL]\x1b[m Server: unreachable\n" +
		"\x1b[2m[skip]\x1b[m Application\n" +
		"[odd] Other\n"
	if styled.String() != wantStyled {
		t.Fatalf("styled doctor output = %q", styled.String())
	}
	if strip(styled.String()) != plain.String() {
		t.Fatalf("styling changed the text: %q vs %q", strip(styled.String()), plain.String())
	}
}

func TestStylesWrapSanitizedText(t *testing.T) {
	var diagnostic bytes.Buffer
	renderer := NewRenderer(Streams{Err: &diagnostic, ColorErr: true}, "human")
	// A status carrying a control character must be sanitized before it is styled.
	if err := renderer.DeploymentEvent(service.Event{Type: "deployment", DeploymentUUID: "d1\n", Status: "finished\x1b[31m"}); err != nil {
		t.Fatal(err)
	}
	if got, want := diagnostic.String(), `Deployment d1\n: finished\u001b[31m`+"\n"; got != want {
		t.Fatalf("hostile status = %q, want %q", got, want)
	}
	diagnostic.Reset()
	if err := renderer.DeploymentEvent(service.Event{Type: "deployment", DeploymentUUID: "d1", Status: "finished"}); err != nil {
		t.Fatal(err)
	}
	if got, want := diagnostic.String(), "Deployment d1: \x1b[1;32mfinished\x1b[m\n"; got != want {
		t.Fatalf("finished = %q, want %q", got, want)
	}
	diagnostic.Reset()
	if err := renderer.DeploymentEvent(service.Event{Type: "warning", Message: "name\tchanged"}); err != nil {
		t.Fatal(err)
	}
	if got, want := diagnostic.String(), "\x1b[33mWarning:\x1b[m name\\tchanged\n"; got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
	// Build log lines are the server's text and stay exactly as received.
	diagnostic.Reset()
	if err := renderer.DeploymentEvent(service.Event{Type: "logs", Logs: "step 1\nstep 2"}); err != nil {
		t.Fatal(err)
	}
	if got := diagnostic.String(); got != "step 1\nstep 2\n" {
		t.Fatalf("logs = %q", got)
	}
}

func TestJSONOutputIsNeverStyled(t *testing.T) {
	streams := func(out *bytes.Buffer) Streams { return Streams{Out: out, Err: out, ColorOut: true, ColorErr: true} }
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production", Project: "p", Instance: "home"}
	for name, render := range map[string]func(*Renderer) error{
		"status": func(r *Renderer) error {
			return r.Status(service.StatusResult{Target: target, Status: "running:healthy", URL: "https://web.example"})
		},
		"deploy": func(r *Renderer) error {
			return r.Deploy(service.DeployResult{DeploymentUUID: "d1", Target: target, Status: "finished"})
		},
		"doctor": func(r *Renderer) error {
			return r.Doctor(service.DoctorResult{Checks: []service.Check{{Name: "Server", Status: "failed", Detail: "x"}}, Failed: true})
		},
		"env diff": func(r *Renderer) error {
			return r.EnvDiff(service.EnvDiffResult{Target: target, Added: []service.EnvChange{{Key: "A", Local: "1"}}, Withheld: []string{"B"}}, false)
		},
		"log event": func(r *Renderer) error {
			return r.LogEvent(service.Event{Type: "deployment", DeploymentUUID: "d1", Status: "failed"})
		},
		"config": func(r *Renderer) error {
			return r.Config(service.ConfigResult{ConfigPath: "coolship.toml"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := render(NewRenderer(streams(&out), "json")); err != nil {
				t.Fatal(err)
			}
			if out.Len() == 0 || strings.Contains(out.String(), "\x1b") {
				t.Fatalf("json output contains escapes or is empty: %q", out.String())
			}
			out.Reset()
			if err := render(NewRenderer(streams(&out), "human")); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "\x1b[") {
				t.Fatalf("human output with color on has no styling: %q", out.String())
			}
		})
	}
}

func TestKeysAndDiffMarkersAreStyledOnStdoutOnly(t *testing.T) {
	var out, diagnostic bytes.Buffer
	renderer := NewRenderer(Streams{Out: &out, Err: &diagnostic, ColorOut: true}, "human")
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production", Project: "p", Instance: "home"}
	if err := renderer.Status(service.StatusResult{Target: target, Status: "running", Warnings: []string{"w"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "\x1b[2mApplication:\x1b[m web (app-1)\n") || !strings.Contains(out.String(), "\x1b[2mStatus:\x1b[m running\n") {
		t.Fatalf("status keys = %q", out.String())
	}
	if diagnostic.String() != "Warning: w\n" {
		t.Fatalf("stderr without ColorErr must stay plain: %q", diagnostic.String())
	}
	out.Reset()
	if err := renderer.Config(service.ConfigResult{ConfigPath: "/p/coolship.toml", Target: "api"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\x1b[2mConfiguration:\x1b[m    /p/coolship.toml\n\x1b[2mTarget:\x1b[m           api\n"; got != want {
		t.Fatalf("config = %q, want %q", got, want)
	}
	out.Reset()
	diff := service.EnvDiffResult{File: ".env", Scope: "runtime", Target: target,
		Added: []service.EnvChange{{Key: "A", Local: "1"}}, Changed: []service.EnvChange{{Key: "B", Local: "1", Remote: "2"}},
		Removed: []service.EnvChange{{Key: "C", Remote: "3"}}, Withheld: []string{"D"}, Unchanged: 1}
	if err := renderer.EnvDiff(diff, false); err != nil {
		t.Fatal(err)
	}
	want := "Comparing .env with runtime variables of web\n" +
		"\x1b[32m+\x1b[m A=********  (local only; push creates it)\n" +
		"\x1b[33m~\x1b[m B: local ********, remote ********\n" +
		"\x1b[31m-\x1b[m C=********  (remote only; push --prune deletes it)\n" +
		"\x1b[2m?\x1b[m D  (remote value withheld; cannot compare)\n" +
		"1 unchanged\n"
	if out.String() != want {
		t.Fatalf("diff = %q, want %q", out.String(), want)
	}
}

func TestErrorsAndPromptsAreStyledOnStderr(t *testing.T) {
	var diagnostic bytes.Buffer
	streams := Streams{Err: &diagnostic, ColorErr: true}
	for _, test := range []struct {
		err  error
		want string
	}{
		{errors.New("server returned 500\n"), "\x1b[1;31mError:\x1b[m server returned 500\\n\n"},
		{context.Canceled, "\x1b[2mInterrupted\x1b[m\n"},
		{service.ErrCancelled, "\x1b[2mCancelled\x1b[m\n"},
	} {
		diagnostic.Reset()
		if err := PrintError(streams, test.err); err != nil {
			t.Fatal(err)
		}
		if diagnostic.String() != test.want {
			t.Fatalf("PrintError(%v) = %q, want %q", test.err, diagnostic.String(), test.want)
		}
	}
	diagnostic.Reset()
	prompter := NewPrompter(Streams{In: strings.NewReader("2\n"), Err: &diagnostic, Interactive: true, ColorErr: true})
	choice, err := prompter.Select(context.Background(), "project", []service.Choice{{ID: "p1", Name: "one"}, {ID: "p2", Name: "two\n"}})
	if err != nil || choice != "p2" {
		t.Fatalf("choice=%q err=%v", choice, err)
	}
	want := "\x1b[1mSelect project:\x1b[m\n  \x1b[36m1.\x1b[m one (p1)\n  \x1b[36m2.\x1b[m two\\n (p2)\n\x1b[1mChoice [1-2, q to cancel]:\x1b[m "
	if diagnostic.String() != want {
		t.Fatalf("select prompt = %q, want %q", diagnostic.String(), want)
	}
	diagnostic.Reset()
	prompter = NewPrompter(Streams{In: strings.NewReader("y\n"), Err: &diagnostic, Interactive: true, ColorErr: true})
	ok, err := prompter.ConfirmUnlink(context.Background(), service.UnlinkPlan{Path: "/p/coolship.toml"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !strings.HasPrefix(diagnostic.String(), "\x1b[1mDelete /p/coolship.toml?\x1b[m\n") || !strings.HasSuffix(diagnostic.String(), "\x1b[1mConfirm [y/N]:\x1b[m ") {
		t.Fatalf("confirm prompt = %q", diagnostic.String())
	}
}

// strip removes the SGR sequences this package emits, and nothing else.
func strip(text string) string {
	for _, code := range sgr {
		text = strings.ReplaceAll(text, "\x1b["+code+"m", "")
	}
	return strings.ReplaceAll(text, "\x1b[m", "")
}
