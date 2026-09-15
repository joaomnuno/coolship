package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/coolify"
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

func TestPrintErrorExplainsKnownServerAnswersOnce(t *testing.T) {
	for _, test := range []struct {
		status int
		denial coolify.Denial
		code   string
		hint   string
		docs   string
	}{
		{401, "", "unauthorized", "Run coolship login", "https://coolship.itrocas.com/docs/platform/errors#unauthorized"},
		{403, "", "forbidden", "lacks a required ability", "https://coolify.io/docs/api/permissions"},
		{403, coolify.DenialAPIDisabled, "api_disabled", "Settings, Advanced", "https://coolify.io/docs/api/ip-allowlist"},
		{301, "", "redirect", "does not follow redirects", "https://coolship.itrocas.com/docs/platform/errors#redirect"},
		{302, "", "redirect", "does not follow redirects", "https://coolship.itrocas.com/docs/platform/errors#redirect"},
		{404, "", "", "", ""},
	} {
		t.Run(fmt.Sprint(test.status, test.denial), func(t *testing.T) {
			err := fmt.Errorf("prepare: %w", &coolify.HTTPError{StatusCode: test.status, Method: http.MethodGet, Endpoint: "/projects", Denial: test.denial})
			var out bytes.Buffer
			if err := PrintError(Streams{Err: &out}, err); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if test.code == "" {
				if text != "Error: "+err.Error()+"\n" {
					t.Fatalf("unexplained status gained text: %q", text)
				}
				return
			}
			lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
			if len(lines) != 3 || lines[0] != "Error ["+test.code+"]: "+err.Error() || !strings.HasPrefix(lines[1], "Hint: ") ||
				strings.Count(text, test.hint) != 1 || lines[2] != "Docs: "+test.docs {
				t.Fatalf("text=%q", text)
			}
		})
	}
}

func TestJSONFormatAlsoWritesTheErrorObjectOnStdout(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		code   string
		text   string
		hinted bool
	}{
		{"catalogued", &coolify.HTTPError{StatusCode: 403, Method: http.MethodGet, Endpoint: "/projects", Message: "API is disabled.", Denial: coolify.DenialAPIDisabled},
			"api_disabled", "Error [api_disabled]: Coolify GET /projects: HTTP 403 Forbidden: API is disabled.\n", true},
		{"input", &service.InputError{Err: errors.New("--port must be between 1 and 65535")}, "invalid_input", "Error: --port must be between 1 and 65535\n", false},
		{"operation", errors.New("boom"), "unclassified", "Error: boom\n", false},
		{"doctor", service.ErrChecksFailed, "checks_failed", "Error: doctor found problems that need attention\n", false},
		{"interrupt", context.Canceled, "interrupted", "Interrupted\n", false},
		{"interrupt after submission", &coolify.UncertainSubmissionError{ResourceUUID: "a1", Err: context.Canceled}, "uncertain_submission",
			"Error [uncertain_submission]: deployment submission for application \"a1\" is uncertain; inspect Coolify before retrying: interrupted\n", true},
		{"declined", service.ErrCancelled, "cancelled", "Cancelled\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := ReportError(Streams{Out: &stdout, Err: &stderr}, "json", test.err); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(stderr.String(), test.text) || (test.hinted != strings.Contains(stderr.String(), "\nHint: ")) {
				t.Fatalf("stderr=%q", stderr.String())
			}
			var object struct {
				Error map[string]string `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &object); err != nil || strings.Count(stdout.String(), "\n") != 1 {
				t.Fatalf("stdout=%q err=%v", stdout.String(), err)
			}
			if object.Error["code"] != test.code || object.Error["message"] == "" || len(object.Error) != 4 {
				t.Fatalf("object=%v", object.Error)
			}
			if _, ok := object.Error["docs_url"]; !ok || test.hinted != (object.Error["hint"] != "") {
				t.Fatalf("object=%v", object.Error)
			}
			stdout.Reset()
			if err := ReportError(Streams{Out: &stdout, Err: &stderr}, "human", test.err); err != nil || stdout.Len() != 0 {
				t.Fatalf("human output wrote stdout: %q", stdout.String())
			}
		})
	}
	var stdout bytes.Buffer
	if err := ReportError(Streams{Out: &stdout}, "json", &service.ExitError{Code: 3}); err != nil || stdout.Len() != 0 {
		t.Fatalf("a child's status gained an object: %q", stdout.String())
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

// A command that already wrote its JSON result, such as doctor with a failed
// check, keeps stdout one JSON value: the text goes to stderr and no error
// object follows. The exit code stays the failure's.
func TestReportErrorAddsNoObjectAfterAWrittenResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := ResultWritten(service.ErrChecksFailed)
	if writeErr := ReportError(Streams{Out: &stdout, Err: &stderr}, "json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "doctor found problems") {
		t.Fatalf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	if ExitCode(err) != 1 || !errors.Is(err, service.ErrChecksFailed) {
		t.Fatalf("exit %d, err %v", ExitCode(err), err)
	}
}
