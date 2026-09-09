package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDoctorCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, credentials, and server access",
		Long: `Run every step a command performs, reporting each: project configuration,
Git boundary, binding, credentials, context, server reachability and version,
and whether the binding resolves to an application.

Exit status is 1 when any check fails. Warnings do not fail the command.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			result, err := app.Doctor(command.Context(), options.Options)
			if err != nil {
				return err
			}
			if err := ui.NewRenderer(streams, options.format).Doctor(result); err != nil {
				return err
			}
			if result.Failed {
				return service.ErrChecksFailed
			}
			return nil
		},
	}
}
