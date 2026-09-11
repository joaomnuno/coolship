package cmd

import (
	"errors"
	"time"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDeployCommand(app Application, options *commandOptions, streams ui.Streams, prefs preferences.Preferences) *cobra.Command {
	var deploy service.DeployOptions
	var logs logFlags
	command := &cobra.Command{
		Use:   "deploy [target]",
		Short: "Deploy the linked application using its configured Coolify source",
		Long: `Deploy the linked application using the source and branch already configured in Coolify.
This command does not upload your worktree or push local commits.

By default, wait for the submitted deployment to finish. Interrupting observation
stops local waiting; the remote deployment continues. Use --no-wait to return
the queued deployment UUID immediately.

In a terminal, the deployment is shown as a checklist of its stages (build,
rolling update, container, cleanup) with the build log collapsed; the log is
printed in full when the deployment fails. --logs streams it live instead,
--no-logs keeps it collapsed; without either, the build_logs preference
decides, and without that the log stays collapsed. Piped output and
--format json print every status line and the log as they always did.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if deploy.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			showLogs, err := buildLogs(logs, prefs.BuildLogs)
			if err != nil {
				return err
			}
			deploy.Options = options.Options
			return observeDeployment(streams, options, deploy.NoWait, showLogs, func(emit service.Emitter) (service.DeployResult, error) {
				return app.Deploy(command.Context(), deploy, emit)
			})
		},
	}
	command.Flags().BoolVar(&deploy.Force, "force", false, "Force Coolify to rebuild without cache")
	command.Flags().BoolVar(&deploy.NoWait, "no-wait", false, "Return after submission without observing completion")
	command.Flags().DurationVar(&deploy.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for deployment completion")
	logs.register(command.Flags())
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
