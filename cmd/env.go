package cmd

import (
	"errors"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// ErrDifferences reports a diff with changes when --exit-code is requested.
var ErrDifferences = errors.New("variables differ")

func newEnvCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var env service.EnvOptions
	group := &cobra.Command{
		Use:   "env",
		Short: "Synchronize a local .env file with the linked application's variables",
		Long: `Pull, compare, and push environment variables between a local dotenv file and
the linked application.

Coolify keeps two scopes per application: the variables regular deployments
see, and a separate copy used by preview deployments. Commands act on the
regular scope unless --preview is given. Values are masked in output unless
--show-values is given. Shared references such as {{team.NAME}} are synced as
references, never as the values they resolve to.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
	}
	group.PersistentFlags().StringVar(&env.File, "file", ".env", "Dotenv file, relative to the application root")
	group.PersistentFlags().BoolVar(&env.Preview, "preview", false, "Act on the preview-deployment scope instead of the regular one")

	var reveal, exitCode bool
	diff := &cobra.Command{
		Use:   "diff",
		Short: "Compare the local file with the remote variables",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			env.Options = options.Options
			result, err := app.EnvDiff(command.Context(), env)
			if err != nil {
				return err
			}
			if err := ui.NewRenderer(streams, options.format).EnvDiff(result, reveal); err != nil {
				return err
			}
			if exitCode && !result.Clean() {
				return ErrDifferences
			}
			return nil
		},
	}
	diff.Flags().BoolVar(&reveal, "show-values", false, "Print values instead of masking them")
	diff.Flags().BoolVar(&exitCode, "exit-code", false, "Exit with status 1 when there are differences")

	pull := &cobra.Command{
		Use:   "pull",
		Short: "Write the remote variables into the local file",
		Long: `Write the remote variables into the local file, creating it with private
permissions. Keys that exist only locally are kept, comments and ordering are
preserved, and values Coolify withholds are noted rather than invented.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			env.Options = options.Options
			result, err := app.EnvPull(command.Context(), env)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).EnvPull(result)
		},
	}

	var push service.EnvPushOptions
	pushCommand := &cobra.Command{
		Use:   "push",
		Short: "Apply the local file to the remote variables",
		Long: `Create and update remote variables from the local file. Keys that exist only
remotely are left in place unless --prune is given. A key whose remote value is
withheld is overwritten only with --force. Changes take effect on the next
deployment.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			env.Options = options.Options
			push.EnvOptions = env
			result, err := app.EnvPush(command.Context(), push, ui.NewPrompter(streams).ConfirmPush)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).EnvPush(result)
		},
	}
	pushCommand.Flags().BoolVar(&push.Prune, "prune", false, "Delete remote variables that are not in the local file")
	pushCommand.Flags().BoolVar(&push.Force, "force", false, "Overwrite remote values Coolify withholds")
	pushCommand.Flags().BoolVarP(&push.Yes, "yes", "y", false, "Push without confirmation")

	group.AddCommand(pull, diff, pushCommand)
	return group
}
