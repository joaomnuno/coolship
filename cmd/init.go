package cmd

import (
	"errors"
	"time"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newInitCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var create service.InitOptions
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a Coolify application for this repository, then link it",
		Long: `Create a Coolify application from this repository's public Git remote and
write coolship.toml for it, exactly as link would.

The repository and branch are read from Git (the origin remote, normalized to
https, and the checked-out branch) unless --repo and --branch say otherwise.
The build pack is detected from the application root: a Dockerfile builds
itself, anything else is handed to Nixpacks. Docker Compose projects are
refused, since their domains and variables are per service. --port is what
the application listens on: 80 for a Dockerfile or static site and 3000 for
Nixpacks unless you say otherwise — check it, Coolify routes traffic there.

The repository must be public: Coolify clones it without credentials.
Private repositories need a GitHub App or deploy key registered in Coolify
first; create those applications in Coolify and run coolship link.

The plan is shown and confirmed before anything is created, or requires
--yes when input is noninteractive. Nothing is deployed unless --deploy is
given, which then waits for the first deployment like deploy does. A
directory that is already linked is refused; use link to change a binding.

In a monorepo, --target NAME writes an [apps.NAME] table with the current
directory as its root, which becomes the application's base directory.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if create.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			create.Options = options.Options
			if create.Target != "" && create.Target != "default" {
				if err := config.ValidateTargetName(create.Target); err != nil {
					return inputError(err)
				}
			}
			prompter := ui.NewPrompter(streams)
			renderer := ui.NewRenderer(streams, options.format)
			result, err := app.Init(command.Context(), create, prompter.Select, prompter.ConfirmInit, renderer.DeploymentEvent)
			if err != nil {
				return err
			}
			return renderer.Init(result)
		},
	}
	command.Flags().StringVar(&create.Repository, "repo", "", "Public repository URL (default: the origin remote)")
	command.Flags().StringVar(&create.Branch, "branch", "", "Branch Coolify deploys (default: the checked-out branch)")
	command.Flags().StringVar(&create.BuildPack, "build-pack", "", "Build pack: nixpacks, dockerfile, or static (default: detected)")
	command.Flags().IntVar(&create.Port, "port", 0, "Port the application listens on (default: 80 for dockerfile and static, 3000 for nixpacks)")
	command.Flags().BoolVar(&create.Static, "static", false, "Serve the build output as a static site")
	command.Flags().StringVar(&create.Name, "name", "", "Application name (default: the repository name)")
	command.Flags().StringVar(&create.Project, "project", "", "Exact Coolify project name (default: prompt, or the only project)")
	command.Flags().BoolVar(&create.CreateProject, "create-project", false, "Create the --project if it does not exist")
	command.Flags().StringVar(&create.Server, "server", "", "Exact server name (default: prompt, or the only usable server)")
	command.Flags().BoolVarP(&create.Yes, "yes", "y", false, "Create without confirmation")
	command.Flags().BoolVar(&create.Deploy, "deploy", false, "Deploy after creating and wait for it")
	command.Flags().DurationVar(&create.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for the deployment with --deploy")
	return command
}
