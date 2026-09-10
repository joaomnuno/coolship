package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

func TestPrintErrorDistinguishesInterruptsAndCancellations(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
		code int
	}{
		{"none", nil, "", 0},
		{"operation", errors.New("server returned 500"), "Error: server returned 500\n", 1},
		{"input", &service.InputError{Err: errors.New("select a project")}, "Error: select a project\n", 2},
		{"interrupt", context.Canceled, "Interrupted\n", 130},
		{"interrupt with recovery detail", &service.DeploymentError{DeploymentUUID: "d1", Err: fmt.Errorf("observation stopped: %w", context.Canceled)},
			"Error: deployment d1: observation stopped: interrupted\n", 130},
		{"declined prompt", service.ErrCancelled, "Cancelled\n", 130},
		{"multi-line stays one line", errors.New("first\nsecond"), "Error: first\\nsecond\n", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := PrintError(Streams{Err: &out}, test.err); err != nil {
				t.Fatal(err)
			}
			if out.String() != test.want || ExitCode(test.err) != test.code {
				t.Fatalf("output=%q code=%d; want %q %d", out.String(), ExitCode(test.err), test.want, test.code)
			}
		})
	}
}

type statusError struct{ code int }

func (e statusError) Error() string {
	return fmt.Sprintf("Coolify GET /projects: HTTP %d %s", e.code, http.StatusText(e.code))
}
func (e statusError) HTTPStatusCode() int { return e.code }

func TestPrintErrorExplainsRejectedTokensAndRedirectsOnce(t *testing.T) {
	for _, test := range []struct {
		code int
		hint string
	}{
		{401, "run coolship login"},
		{403, "lacks a required ability"},
		{301, "redirects are not followed"},
		{302, "redirects are not followed"},
		{404, ""},
		{500, ""},
	} {
		t.Run(fmt.Sprint(test.code), func(t *testing.T) {
			err := fmt.Errorf("prepare: %w", statusError{test.code})
			var out bytes.Buffer
			if err := PrintError(Streams{Err: &out}, err); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if test.hint == "" {
				if text != "Error: "+err.Error()+"\n" {
					t.Fatalf("unexplained status gained text: %q", text)
				}
				return
			}
			if !strings.HasPrefix(text, "Error: prepare: Coolify GET /projects: HTTP "+fmt.Sprint(test.code)) || strings.Count(text, test.hint) != 1 || strings.Count(text, "\n") != 1 {
				t.Fatalf("text=%q", text)
			}
		})
	}
}

func TestExitStatusThatIsTheWholeAnswerPrintsNothing(t *testing.T) {
	cause := errors.New("variables differ")
	err := &service.ExitError{Code: 1, Err: cause}
	var out bytes.Buffer
	if printErr := PrintError(Streams{Err: &out}, err); printErr != nil || out.Len() != 0 {
		t.Fatalf("out=%q err=%v", out.String(), printErr)
	}
	if !errors.Is(err, cause) || ExitCode(err) != 1 || err.Error() != "variables differ" {
		t.Fatalf("cause lost: %v (code %d)", err, ExitCode(err))
	}
	if (&service.ExitError{Code: 7}).Error() != "command exited with status 7" {
		t.Fatal("bare exit status changed its text")
	}
}
