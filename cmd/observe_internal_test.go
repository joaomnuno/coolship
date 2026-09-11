package cmd

import (
	"errors"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
)

// TestBuildLogsResolvesFlagThenPreferenceThenDefault covers the three
// sources in order: a flag beats the preference, the preference beats the
// default, the default is collapsed, and both flags at once is invalid input.
func TestBuildLogsResolvesFlagThenPreferenceThenDefault(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name       string
		flags      logFlags
		preference *bool
		want       bool
	}{
		{"default collapses", logFlags{}, nil, false},
		{"preference true streams", logFlags{}, &yes, true},
		{"preference false collapses", logFlags{}, &no, false},
		{"--logs alone streams", logFlags{logs: true}, nil, true},
		{"--no-logs alone collapses", logFlags{noLogs: true}, nil, false},
		{"--logs beats a false preference", logFlags{logs: true}, &no, true},
		{"--no-logs beats a true preference", logFlags{noLogs: true}, &yes, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := buildLogs(test.flags, test.preference)
			if err != nil || got != test.want {
				t.Fatalf("buildLogs(%+v, %v) = %v, %v; want %v", test.flags, test.preference, got, err, test.want)
			}
		})
	}
	if _, err := buildLogs(logFlags{logs: true, noLogs: true}, &yes); !errors.Is(err, service.ErrInput) || err.Error() != "--logs and --no-logs cannot be combined" {
		t.Fatalf("both flags: %v", err)
	}
}
