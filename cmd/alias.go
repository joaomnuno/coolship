package cmd

import (
	"errors"
	"fmt"

	"github.com/joaomnuno/coolship/internal/alias"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newAliasCommand(options *commandOptions, streams ui.Streams, system alias.System) *cobra.Command {
	var remove bool
	command := &cobra.Command{
		Use:   "alias [NAME]",
		Short: "Add a short name for coolship, such as cs",
		Long: `Add NAME (default cs) as a second name for coolship, next to the coolship
binary: a symlink, or a copy on Windows. Nothing else is written.

alias refuses when NAME already runs another command on your PATH, naming
that command, and when the binary's directory is not writable. Running it
again when the alias exists changes nothing. --remove deletes the alias,
but only when it is a link to or copy of Coolship.`,
		Example: `  coolship alias
  coolship alias ship
  coolship alias --remove`,
		Args: func(command *cobra.Command, args []string) error {
			if len(args) > 1 {
				return inputError(fmt.Errorf("alias accepts at most one name"))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			name := alias.DefaultName
			if len(args) == 1 {
				name = args[0]
			}
			operation := alias.Create
			if remove {
				operation = alias.Remove
			}
			result, err := operation(system, name)
			if err != nil {
				return aliasError(err)
			}
			return ui.NewRenderer(streams, options.format).Alias(result)
		},
	}
	command.Flags().BoolVar(&remove, "remove", false, "Remove the alias instead of adding it")
	return command
}

// aliasError marks refusals that the user resolves by choosing differently
// as invalid input (exit 2); an unwritable directory stays an operational
// failure (exit 1).
func aliasError(err error) error {
	var name *alias.NameError
	var conflict *alias.ConflictError
	var notCoolship *alias.NotCoolshipError
	if errors.As(err, &name) || errors.As(err, &conflict) || errors.As(err, &notCoolship) {
		return inputError(err)
	}
	return err
}
