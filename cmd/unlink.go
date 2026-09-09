package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newUnlinkCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var unlink service.UnlinkOptions
	command := &cobra.Command{
		Use:   "unlink",
		Short: "Remove this repository's binding to its Coolify application",
		Long: `Delete coolship.toml. Nothing on the Coolify server changes.
Deletion asks for confirmation, or requires --yes when input is noninteractive.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			unlink.Options = options.Options
			result, err := app.Unlink(command.Context(), unlink, ui.NewPrompter(streams).ConfirmUnlink)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Unlink(result)
		},
	}
	command.Flags().BoolVarP(&unlink.Yes, "yes", "y", false, "Delete without confirmation")
	return command
}
