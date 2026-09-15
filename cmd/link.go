package cmd

import (
	"context"
	"errors"

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
execution. Resource names match exactly within their selected parent. The
file records each resource's UUID beside its name; a UUID that no longer
exists falls back to the name, with a warning to run link again.

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
			steps := ui.NewSteps(streams, options.format, []string{"Choose the application", "Write coolship.toml"})
			var result service.LinkResult
			var err error
			if steps.Live() {
				result, err = linkWithSteps(command.Context(), app, link, prompter, steps)
			} else {
				result, err = linkWithSpinner(command.Context(), app, link, prompter, ui.NewSpinner(streams))
			}
			if err != nil {
				return err
			}
			if err := ui.NewRenderer(streams, options.format).Link(result); err != nil {
				return err
			}
			showNextSteps(command.Context(), app, options, streams, ui.NextStepOptions{Target: link.Target, NoDeploy: options.rerunPending})
			return nil
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

// linkWithSpinner is link without a checklist. The listings happen between
// the prompts, so the spinner runs from the start and between each answer
// and the next question.
func linkWithSpinner(ctx context.Context, app Application, options service.LinkOptions, prompter *ui.Prompter, spinner *ui.Spinner) (service.LinkResult, error) {
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
	return app.Link(ctx, options, selectChoice, confirm)
}

// linkWithSteps runs link as a checklist: choosing the application, with the
// read in progress beside the open step and each picker shown while the
// checklist is paused, then writing the binding. A replacement's review
// pauses it after the choice is done.
func linkWithSteps(ctx context.Context, app Application, options service.LinkOptions, prompter *ui.Prompter, steps *ui.Steps) (service.LinkResult, error) {
	defer steps.Close()
	steps.Start(0)
	steps.Note(0, linkWaits["context"])
	current := 0
	selectChoice := func(ctx context.Context, kind string, choices []service.Choice) (string, error) {
		steps.Pause()
		id, err := prompter.Select(ctx, kind, choices)
		steps.Resume()
		if err == nil {
			steps.Note(0, linkWaits[kind])
		}
		return id, err
	}
	confirm := func(ctx context.Context, plan service.LinkPlan) (bool, error) {
		steps.Done(0, bindingNames(plan.Target))
		current = -1
		steps.Pause()
		accepted, err := prompter.Confirm(ctx, plan)
		if err != nil || !accepted {
			return accepted, err
		}
		steps.Resume()
		steps.Start(1)
		current = 1
		return true, nil
	}
	result, err := app.Link(ctx, options, selectChoice, confirm)
	switch {
	case err == nil:
		if current == 0 {
			steps.Done(0, bindingNames(result.Target))
		}
		steps.Done(1, "")
	case errors.Is(err, service.ErrCancelled) && current == -1:
		steps.Skip(1, "cancelled")
	case current >= 0:
		steps.Fail(current, err)
	}
	return result, err
}

// bindingNames is the project, environment, and application a binding names.
func bindingNames(target service.TargetInfo) string {
	return target.Project + " / " + target.Environment + " / " + target.Application
}
