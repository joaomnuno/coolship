package cmd

import (
	"errors"
	"testing"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// TestResolveVerbosityFlagsThenEnvironmentThenPreference covers the order:
// --debug, --verbose, COOLSHIP_VERBOSITY, the preference, then normal.
func TestResolveVerbosityFlagsThenEnvironmentThenPreference(t *testing.T) {
	env := func(value string) func(string) string {
		return func(name string) string {
			if name == VerbosityEnv {
				return value
			}
			return ""
		}
	}
	for _, test := range []struct {
		name       string
		flags      verbosityFlags
		env        func(string) string
		preference string
		want       ui.Verbosity
	}{
		{"nothing is normal", verbosityFlags{}, nil, "", ui.VerbosityNormal},
		{"preference", verbosityFlags{}, env(""), "verbose", ui.VerbosityVerbose},
		{"environment beats preference", verbosityFlags{}, env("debug"), "verbose", ui.VerbosityDebug},
		{"environment normal beats preference", verbosityFlags{}, env("normal"), "debug", ui.VerbosityNormal},
		{"environment is trimmed and case-blind", verbosityFlags{}, env(" Verbose "), "", ui.VerbosityVerbose},
		{"--verbose beats environment", verbosityFlags{verbose: true}, env("debug"), "debug", ui.VerbosityVerbose},
		{"--debug beats everything", verbosityFlags{debug: true}, env("normal"), "normal", ui.VerbosityDebug},
		{"both flags mean debug", verbosityFlags{verbose: true, debug: true}, nil, "", ui.VerbosityDebug},
		{"a flag wins over a bad environment", verbosityFlags{verbose: true}, env("loud"), "", ui.VerbosityVerbose},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveVerbosity(test.flags, test.env, test.preference)
			if err != nil || got != test.want {
				t.Fatalf("resolveVerbosity = %v, %v; want %v", got, err, test.want)
			}
		})
	}
	_, err := resolveVerbosity(verbosityFlags{}, env("loud"), "verbose")
	if !errors.Is(err, service.ErrInput) || err.Error() != `COOLSHIP_VERBOSITY must be one of normal, verbose, debug, not "loud"` {
		t.Fatalf("bad environment: %v", err)
	}
}
