package cmd

import (
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/spf13/cobra"
)

type commandOptions struct {
	service.Options
	format string
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
