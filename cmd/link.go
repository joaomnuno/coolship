package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newLinkCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var link service.LinkOptions
	command := &cobra.Command{
		Use:   "link",
		Short: "Bind this repository to an existing Coolify application",
		Long: `Bind this repository to an existing Coolify application and write coolship.toml.
Select resources interactively, or supply explicit selectors for noninteractive
execution. Resource names match exactly within their selected parent.

Linking changes only local configuration. Existing changed bindings require
confirmation or --replace; replacing regenerates the complete configuration.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			link.Options = options.Options
			prompter := ui.NewPrompter(streams)
			result, err := app.Link(command.Context(), link, prompter.Select, prompter.Confirm)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Link(result)
		},
	}
	command.Flags().StringVar(&link.Project, "project", "", "Exact Coolify project name")
	command.Flags().StringVar(&link.Application, "application", "", "Exact application name within the selected environment")
	command.Flags().StringVar(&link.ProjectUUID, "project-uuid", "", "Pin a Coolify project UUID")
	command.Flags().StringVar(&link.EnvironmentUUID, "environment-uuid", "", "Pin an environment UUID within the selected project")
	command.Flags().StringVar(&link.ApplicationUUID, "application-uuid", "", "Pin an application UUID within the selected environment")
	command.Flags().StringVar(&link.Root, "root", "", "Local application root, relative to the project configuration directory (default .)")
	command.Flags().BoolVar(&link.Replace, "replace", false, "Replace existing changed configuration, including comments and unrelated settings")
	return command
}
