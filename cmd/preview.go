package cmd

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newPreviewCommand(app Application, options *commandOptions, streams ui.Streams, environment func(string) string) *cobra.Command {
	var deploy service.DeployOptions
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
GitHub Actions pull_request workflow.`,
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
			deploy.Options = options.Options
			renderer := ui.NewRenderer(streams, options.format)
			result, err := app.Deploy(command.Context(), deploy, renderer.DeploymentEvent)
			if err != nil {
				return deploymentFailure(renderer, options.format, result, err)
			}
			return renderer.Deploy(result)
		},
	}
	command.Flags().IntVar(&deploy.PullRequest, "pr", 0, "Pull request number (default: from GITHUB_REF)")
	command.Flags().BoolVar(&deploy.Force, "force", false, "Force Coolify to rebuild without cache")
	command.Flags().BoolVar(&deploy.NoWait, "no-wait", false, "Return after submission without observing completion")
	command.Flags().DurationVar(&deploy.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for deployment completion")
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
