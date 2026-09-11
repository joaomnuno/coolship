package cmd

import (
	"context"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	return &cobra.Command{
		Use:   "status [target]",
		Short: "Inspect the linked application's current status",
		Args:  targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			var result service.StatusResult
			err := ui.Wait(command.Context(), streams, "Reading application status", func(ctx context.Context) error {
				var err error
				result, err = app.Status(ctx, options.Options)
				return err
			})
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Status(result)
		},
	}
}
