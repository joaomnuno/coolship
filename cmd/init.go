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
		Long: `Create a Coolify application from this repository's Git remote and write
coolship.toml for it, exactly as link would.

The repository and branch are read from Git (the origin remote and the
checked-out branch) unless --repo and --branch say otherwise. The build pack
is detected from the application root: a Dockerfile builds itself, anything
else is handed to Nixpacks. Docker Compose projects are refused, since their
domains and variables are per service. --port is what the application
listens on: 80 for a Dockerfile or static site and 3000 for Nixpacks unless
you say otherwise — check it, Coolify routes traffic there.

How Coolify clones is decided by --source. The default, auto, checks whether
the remote can be read without credentials: a public repository is cloned
anonymously, and a private one needs one of the sources Coolify supports,
which you are asked to choose (or must name with --source when input is
noninteractive). --source public skips that check and clones anonymously:

  github-app  a GitHub App installed on the repository and registered in
              Coolify. Pick it with --github-app NAME, or from a list. The
              app's access to the repository is checked through Coolify
              before anything is created, and so is the branch when the
              app's listing of them is complete.
  deploy-key  an SSH key Coolify holds, registered on the repository as a
              read-only deploy key. Pick an existing key with --deploy-key
              NAME, or from a list. --create-deploy-key NAME generates a new
              key, stores it in Coolify, prints its public half once, and
              stops: add that public key to the repository, then run init
              again with --deploy-key NAME. A deploy key clones over SSH, so
              the remote is stored in its SSH form.

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
	command.Flags().StringVar(&create.Repository, "repo", "", "Repository URL (default: the origin remote)")
	command.Flags().StringVar(&create.Branch, "branch", "", "Branch Coolify deploys (default: the checked-out branch)")
	command.Flags().StringVar(&create.BuildPack, "build-pack", "", "Build pack: nixpacks, dockerfile, or static (default: detected)")
	command.Flags().IntVar(&create.Port, "port", 0, "Port the application listens on (default: 80 for dockerfile and static, 3000 for nixpacks)")
	command.Flags().BoolVar(&create.Static, "static", false, "Serve the build output as a static site")
	command.Flags().StringVar(&create.Name, "name", "", "Application name (default: the repository name)")
	command.Flags().StringVar(&create.Project, "project", "", "Exact Coolify project name (default: prompt, or the only project)")
	command.Flags().BoolVar(&create.CreateProject, "create-project", false, "Create the --project if it does not exist")
	command.Flags().StringVar(&create.Server, "server", "", "Exact server name (default: prompt, or the only usable server)")
	command.Flags().StringVar(&create.Source, "source", "auto", "How Coolify clones: auto, public, github-app, or deploy-key")
	command.Flags().StringVar(&create.GitHubApp, "github-app", "", "Exact name of the GitHub App to clone through (implies --source github-app)")
	command.Flags().StringVar(&create.DeployKey, "deploy-key", "", "Exact name of an existing Coolify key to clone with (implies --source deploy-key)")
	command.Flags().StringVar(&create.CreateDeployKey, "create-deploy-key", "", "Create a key with this name, print its public half, and stop (implies --source deploy-key)")
	command.Flags().BoolVarP(&create.Yes, "yes", "y", false, "Create without confirmation")
	command.Flags().BoolVar(&create.Deploy, "deploy", false, "Deploy after creating and wait for it")
	command.Flags().DurationVar(&create.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for the deployment with --deploy")
	return command
}
