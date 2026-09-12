package cmd

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newPreviewCommand(app Application, options *commandOptions, streams ui.Streams, environment func(string) string, prefs preferences.Preferences) *cobra.Command {
	var deploy service.DeployOptions
	var logs logFlags
	command := &cobra.Command{
		Use:   "preview [target]",
		Short: "Deploy the preview Coolify holds for a pull request",
		Long: `Deploy the preview deployment Coolify already holds for a pull request, and
observe it like deploy does.

Coolify must already know the pull request: preview deployments must be
enabled for the application, and the pull request added by Coolify's GitHub
webhook or in its UI. This command cannot create a preview, and reports the
server's answer when it does not know the pull request.

The pull request number comes from --pr, or from GITHUB_REF when running in a
GitHub Actions pull_request workflow.

In a terminal the deployment is shown as a stage checklist with the build log
collapsed, as deploy does; --logs streams the log, --no-logs keeps it
collapsed, the build_logs preference decides when neither is given, and
without that the verbosity: collapsed when normal, streamed with --verbose or
--debug.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if deploy.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			if deploy.PullRequest == 0 && environment != nil {
				deploy.PullRequest = pullRequestFromRef(environment("GITHUB_REF"))
			}
			if deploy.PullRequest <= 0 {
				return inputError(errors.New("--pr is required outside a GitHub Actions pull_request workflow"))
			}
			showLogs, err := buildLogs(logs, prefs.BuildLogs, options.verbosity)
			if err != nil {
				return err
			}
			deploy.Options = options.Options
			return observeDeployment(streams, options, deploy.NoWait, showLogs, func(emit service.Emitter) (service.DeployResult, error) {
				return app.Deploy(command.Context(), deploy, emit)
			})
		},
	}
	command.Flags().IntVar(&deploy.PullRequest, "pr", 0, "Pull request number (default: from GITHUB_REF)")
	command.Flags().BoolVar(&deploy.Force, "force", false, "Force Coolify to rebuild without cache")
	command.Flags().BoolVar(&deploy.NoWait, "no-wait", false, "Return after submission without observing completion")
	command.Flags().DurationVar(&deploy.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for deployment completion")
	logs.register(command.Flags())
	return command
}

// pullRequestFromRef reads refs/pull/<n>/merge or refs/pull/<n>/head.
func pullRequestFromRef(ref string) int {
	parts := strings.Split(strings.TrimSpace(ref), "/")
	if len(parts) != 4 || parts[0] != "refs" || parts[1] != "pull" {
		return 0
	}
	number, err := strconv.Atoi(parts[2])
	if err != nil || number <= 0 {
		return 0
	}
	return number
}
