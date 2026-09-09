package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
			if err := PrintError(&out, test.err); err != nil {
				t.Fatal(err)
			}
			if out.String() != test.want || ExitCode(test.err) != test.code {
				t.Fatalf("output=%q code=%d; want %q %d", out.String(), ExitCode(test.err), test.want, test.code)
			}
		})
	}
}
