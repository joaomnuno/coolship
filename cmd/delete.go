package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDeleteCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var del service.DeleteOptions
	command := &cobra.Command{
		Use:   "delete [target]",
		Short: "Delete the linked application from Coolify",
		Long: `Delete the linked application on the Coolify server, the inverse of what init
created. Coolify stops and removes its containers, then removes the application
itself with its volumes, its configuration, and its deployment history. Nothing
brings it back.

The application, its environment, its current status, and the URLs it serves are
shown and confirmed first, or --yes skips the question, which is required when
input is noninteractive. --keep-volumes leaves the volumes on the server, so the
data outlives the application.

The local coolship.toml is kept, and then names an application that is gone:
--unlink deletes it in the same run, or coolship unlink does it afterwards.
Deleting a project, a server, or anything else on the instance is not something
this command does; Coolship stays a project-local tool.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			del.Options = options.Options
			renderer := ui.NewRenderer(streams, options.format)
			result, err := app.Delete(command.Context(), del, ui.NewPrompter(streams).ConfirmDelete, renderer.DeploymentEvent)
			if err != nil {
				return err
			}
			if err := renderer.Delete(result); err != nil {
				return err
			}
			if result.Unlinked == "" {
				options.hint(streams, ui.NextUnlink(options.Target))
			}
			return nil
		},
	}
	command.Flags().BoolVarP(&del.Yes, "yes", "y", false, "Delete without confirmation")
	command.Flags().BoolVar(&del.KeepVolumes, "keep-volumes", false, "Leave the application's volumes on the server")
	command.Flags().BoolVar(&del.Unlink, "unlink", false, "Delete the local coolship.toml too")
	return command
}
