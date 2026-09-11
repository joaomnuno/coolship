package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDeploymentsCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var deployments service.DeploymentsOptions
	command := &cobra.Command{
		Use:   "deployments [target]",
		Short: "List recent deployments of the linked application",
		Long: `List the linked application's most recent deployments, newest first: a short
UUID, the status, the commit, whether it was a pull request preview, a
restart, or a rollback, when it was created, and how long it took. Build logs
are never included; deploy streams them while a deployment runs.

The full UUID is in --format json and can be passed to cancel.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if deployments.Limit <= 0 {
				return inputError(errors.New("--limit must be greater than zero"))
			}
			deployments.Options = options.Options
			var result service.DeploymentsResult
			err := ui.Wait(command.Context(), streams, "Listing deployments", func(ctx context.Context) error {
				var err error
				result, err = app.Deployments(ctx, deployments)
				return err
			})
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Deployments(result)
		},
	}
	command.Flags().IntVarP(&deployments.Limit, "limit", "n", 10, "Number of deployments to list")
	return command
}

func newCancelCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var cancel service.CancelOptions
	command := &cobra.Command{
		Use:   "cancel [target] [DEPLOYMENT_UUID]",
		Short: "Cancel a queued or running deployment",
		Long: `Cancel a deployment of the linked application. Without a UUID, the one that is
queued or in progress is cancelled; when there is none, or more than one, the
command says so and stops. Only queued and in-progress deployments can be
cancelled — Coolify refuses the rest.

One argument is a deployment UUID; in a monorepo, name the target first, as in
"cancel api DEPLOYMENT_UUID", or select it with --target. The cancellation is
confirmed first, or --yes skips the question.`,
		Args: func(command *cobra.Command, args []string) error {
			if len(args) > 2 {
				return inputError(fmt.Errorf("cancel accepts at most a target and a deployment UUID"))
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			if err := splitCancelArgs(options, &cancel, args); err != nil {
				return err
			}
			cancel.Options = options.Options
			result, err := app.Cancel(command.Context(), cancel, ui.NewPrompter(streams).ConfirmCancel)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Cancel(result)
		},
	}
	command.Flags().BoolVarP(&cancel.Yes, "yes", "y", false, "Cancel without confirmation")
	return command
}

// splitCancelArgs reads the optional positionals. One argument is the
// deployment UUID — the thing the command is about, while the target is
// normally implied by the directory; two are the target and then the UUID.
func splitCancelArgs(options *commandOptions, cancel *service.CancelOptions, args []string) error {
	switch len(args) {
	case 0:
		return nil
	case 1:
		cancel.DeploymentUUID = args[0]
		return nil
	default:
		if options.Target != "" && options.Target != args[0] {
			return inputError(fmt.Errorf("target %q conflicts with --target %q", args[0], options.Target))
		}
		options.Target = args[0]
		cancel.DeploymentUUID = args[1]
		return nil
	}
}
