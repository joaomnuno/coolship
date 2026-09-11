package cmd

import (
	"errors"
	"time"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDeployCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var deploy service.DeployOptions
	command := &cobra.Command{
		Use:   "deploy [target]",
		Short: "Deploy the linked application using its configured Coolify source",
		Long: `Deploy the linked application using the source and branch already configured in Coolify.
This command does not upload your worktree or push local commits.

By default, wait for the submitted deployment to finish. Interrupting observation
stops local waiting; the remote deployment continues. Use --no-wait to return
the queued deployment UUID immediately.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if deploy.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			deploy.Options = options.Options
			renderer := ui.NewRenderer(streams, options.format)
			result, err := app.Deploy(command.Context(), deploy, renderer.DeploymentEvent)
			if err != nil {
				return deploymentFailure(renderer, options.format, result, err)
			}
			return renderer.Deploy(result)
		},
	}
	command.Flags().BoolVar(&deploy.Force, "force", false, "Force Coolify to rebuild without cache")
	command.Flags().BoolVar(&deploy.NoWait, "no-wait", false, "Return after submission without observing completion")
	command.Flags().DurationVar(&deploy.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for deployment completion")
	return command
}

// deploymentFailure returns the failure after writing what a reader needs to
// follow up: once a deployment has a UUID, --format json prints the result
// with its last observed status and URL even though the command fails, and
// human output names the deployment's Coolify page on stderr, where the log
// and the retry live. The exit status is the failure's either way.
func deploymentFailure(renderer *ui.Renderer, format string, result service.DeployResult, err error) error {
	if result.DeploymentUUID == "" {
		return err
	}
	// The failure is what the caller must learn; a lost write cannot displace it.
	if format == "json" {
		_ = renderer.Deploy(result)
	} else if result.URL != "" {
		_ = renderer.DeploymentPage(result.URL)
	}
	return err
}
