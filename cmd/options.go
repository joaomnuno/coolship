package cmd

import (
	"context"
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandOptions struct {
	service.Options
	format string
	// verbosity is resolved once, before any command runs.
	verbosity ui.Verbosity
	// noHints is the hints = false preference; every next-step hint goes
	// through hint so the preference is honored in one place.
	noHints bool
	// run executes another command line in a new tree with the same
	// dependencies, for a follow-up the user asked for.
	run func(ctx context.Context, args []string) error
}

// hintsShown reports whether next-step hints and the questions after them
// reach a person: hints on, human output, and interactive streams.
func (o *commandOptions) hintsShown(streams ui.Streams) bool {
	return !o.noHints && o.format == "human" && streams.Interactive
}

// globalArgs repeats the global flags this run was given, so a follow-up
// command addresses the same directory, instance, and target. --format is
// left out: follow-ups only run for human output. A target given as a
// positional argument is repeated as --target.
func globalArgs(root *cobra.Command) []string {
	var args []string
	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "format" {
			return
		}
		if !flag.Changed && (flag.Name != "target" || flag.Value.String() == "") {
			return
		}
		args = append(args, "--"+flag.Name+"="+flag.Value.String())
	})
	return args
}

// hint prints a next-step suggestion unless the hints preference is off;
// ui.Hint decides whether the streams and format want one at all.
func (o *commandOptions) hint(streams ui.Streams, text string) {
	if o.noHints {
		return
	}
	ui.Hint(streams, o.format, text)
}

func noArgs(command *cobra.Command, args []string) error {
	if err := cobra.NoArgs(command, args); err != nil {
		return inputError(err)
	}
	return nil
}

func inputError(err error) error { return &service.InputError{Err: err} }

// targetArg accepts an optional positional target, so `coolship deploy web`
// reads like the brief. It cannot disagree with --target.
func targetArg(options *commandOptions) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) > 1 {
			return inputError(fmt.Errorf("%s accepts at most one target argument", command.Name()))
		}
		if len(args) == 1 {
			if options.Target != "" && options.Target != args[0] {
				return inputError(fmt.Errorf("target %q conflicts with --target %q", args[0], options.Target))
			}
			options.Target = args[0]
		}
		return nil
	}
}
