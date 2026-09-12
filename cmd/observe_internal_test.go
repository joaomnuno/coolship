package cmd

import (
	"errors"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// TestBuildLogsResolvesFlagThenPreferenceThenVerbosity covers the three
// sources in order: a flag beats the preference, the preference beats the
// verbosity, normal collapses while verbose and debug stream, and both flags
// at once is invalid input.
func TestBuildLogsResolvesFlagThenPreferenceThenVerbosity(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name       string
		flags      logFlags
		preference *bool
		verbosity  ui.Verbosity
		want       bool
	}{
		{"normal collapses", logFlags{}, nil, ui.VerbosityNormal, false},
		{"verbose streams", logFlags{}, nil, ui.VerbosityVerbose, true},
		{"debug streams", logFlags{}, nil, ui.VerbosityDebug, true},
		{"preference true streams", logFlags{}, &yes, ui.VerbosityNormal, true},
		{"preference false collapses", logFlags{}, &no, ui.VerbosityNormal, false},
		{"preference false beats debug", logFlags{}, &no, ui.VerbosityDebug, false},
		{"--logs alone streams", logFlags{logs: true}, nil, ui.VerbosityNormal, true},
		{"--no-logs alone collapses", logFlags{noLogs: true}, nil, ui.VerbosityNormal, false},
		{"--no-logs beats verbose", logFlags{noLogs: true}, nil, ui.VerbosityVerbose, false},
		{"--logs beats a false preference", logFlags{logs: true}, &no, ui.VerbosityNormal, true},
		{"--no-logs beats a true preference", logFlags{noLogs: true}, &yes, ui.VerbosityNormal, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := buildLogs(test.flags, test.preference, test.verbosity)
			if err != nil || got != test.want {
				t.Fatalf("buildLogs(%+v, %v, %v) = %v, %v; want %v", test.flags, test.preference, test.verbosity, got, err, test.want)
			}
		})
	}
	if _, err := buildLogs(logFlags{logs: true, noLogs: true}, &yes, ui.VerbosityDebug); !errors.Is(err, service.ErrInput) || err.Error() != "--logs and --no-logs cannot be combined" {
		t.Fatalf("both flags: %v", err)
	}
}

// TestDeploymentOutcomeFailsOnlyOnTheServersVerdict covers the decision that
// gates the build log dump: the deployment's last observed status, not the
// error the run returned. A timeout, an interrupt, or a failed poll leaves
// the deployment queued or running, which stops observation without failing
// the deployment, so its log must stay collapsed.
func TestDeploymentOutcomeFailsOnlyOnTheServersVerdict(t *testing.T) {
	for status, want := range map[string]ui.Outcome{
		"failed":            ui.OutcomeFailed,
		"cancelled-by-user": ui.OutcomeFailed,
		"queued":            ui.OutcomeStopped,
		"in_progress":       ui.OutcomeStopped,
		"":                  ui.OutcomeStopped,
	} {
		if got := deploymentOutcome(status); got != want {
			t.Errorf("deploymentOutcome(%q) = %v, want %v", status, got, want)
		}
	}
}
