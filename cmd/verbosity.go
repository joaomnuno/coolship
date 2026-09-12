package cmd

import (
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/pflag"
)

// VerbosityEnv names the environment variable that sets the verbosity where
// flags are awkward to add, such as CI.
const VerbosityEnv = "COOLSHIP_VERBOSITY"

// verbosityFlags are the global --verbose and --debug. Both are long-only:
// -v stays the version, as it is in most peer CLIs (ADR 0002).
type verbosityFlags struct {
	verbose, debug bool
}

func (f *verbosityFlags) register(flags *pflag.FlagSet) {
	flags.BoolVar(&f.verbose, "verbose", false, "Show one line per request, with its status and time, on stderr; stream build logs")
	flags.BoolVar(&f.debug, "debug", false, "Show every request and response in full on stderr, token masked")
}

// resolveVerbosity decides how much a run shows. The first source that
// speaks wins:
//
//  1. --debug, then --verbose; debug includes verbose, so both means debug;
//  2. COOLSHIP_VERBOSITY, whose value must be normal, verbose, or debug;
//  3. the verbosity key of the preferences file, already checked when read;
//  4. normal.
func resolveVerbosity(flags verbosityFlags, environment func(string) string, preference string) (ui.Verbosity, error) {
	switch {
	case flags.debug:
		return ui.VerbosityDebug, nil
	case flags.verbose:
		return ui.VerbosityVerbose, nil
	}
	if environment != nil {
		if value := strings.TrimSpace(environment(VerbosityEnv)); value != "" {
			level, ok := parseVerbosity(strings.ToLower(value))
			if !ok {
				return ui.VerbosityNormal, inputError(fmt.Errorf("%s must be one of normal, verbose, debug, not %q", VerbosityEnv, value))
			}
			return level, nil
		}
	}
	if level, ok := parseVerbosity(preference); ok {
		return level, nil
	}
	return ui.VerbosityNormal, nil
}

func parseVerbosity(value string) (ui.Verbosity, bool) {
	switch value {
	case preferences.VerbosityNormal:
		return ui.VerbosityNormal, true
	case preferences.VerbosityVerbose:
		return ui.VerbosityVerbose, true
	case preferences.VerbosityDebug:
		return ui.VerbosityDebug, true
	}
	return ui.VerbosityNormal, false
}
