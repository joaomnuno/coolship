package cmd

import (
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "status [target]",
		Short: "Inspect the linked application's current status",
		Args:  targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			result, err := app.Status(command.Context(), options.Options)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Status(result)
		},
	}
}
