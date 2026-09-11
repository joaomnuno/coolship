package cmd

import (
	"context"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// linkWaits names what link reads from the server after each choice, keyed
// by the kind of choice just made; "context" is also what it reads first.
var linkWaits = map[string]string{
	"context":     "Listing projects",
	"project":     "Listing environments",
	"environment": "Listing applications",
	"application": "Reading the application",
}

func newLinkCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var link service.LinkOptions
	command := &cobra.Command{
		Use:   "link",
		Short: "Bind this repository to an existing Coolify application",
		Long: `Bind this repository to an existing Coolify application and write coolship.toml.
Select resources interactively, or supply explicit selectors for noninteractive
execution. Resource names match exactly within their selected parent.

Linking changes only local configuration. Existing changed bindings require
confirmation or --replace; replacing regenerates the complete configuration.

In a monorepo, --target NAME writes an [apps.NAME] table instead of [project],
with the current directory as its root. Adding a target keeps the others;
changing one, or converting between the two forms, requires review.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			link.Options = options.Options
			if link.Target != "" && link.Target != "default" {
				if err := config.ValidateTargetName(link.Target); err != nil {
					return inputError(err)
				}
			}
			prompter := ui.NewPrompter(streams)
			// The listings happen between the prompts, so the spinner runs
			// from the start and between each answer and the next question.
			spinner := ui.NewSpinner(streams)
			defer spinner.Stop()
			spinner.Start(linkWaits["context"])
			selectChoice := func(ctx context.Context, kind string, choices []service.Choice) (string, error) {
				spinner.Stop()
				id, err := prompter.Select(ctx, kind, choices)
				if err == nil {
					if label := linkWaits[kind]; label != "" {
						spinner.Start(label)
					}
				}
				return id, err
			}
			confirm := func(ctx context.Context, plan service.LinkPlan) (bool, error) {
				spinner.Stop()
				return prompter.Confirm(ctx, plan)
			}
			result, err := app.Link(command.Context(), link, selectChoice, confirm)
			spinner.Stop()
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
	command.Flags().StringVar(&link.Root, "root", "", "Local application root, relative to the project configuration directory (default: . for [project], the current directory for a named target)")
	command.Flags().BoolVar(&link.Replace, "replace", false, "Replace existing changed configuration, including comments and unrelated settings")
	return command
}
