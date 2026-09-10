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
	var composeDomains []string
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a Coolify application for this repository, then link it",
		Long: `Create a Coolify application from this repository's Git remote and write
coolship.toml for it, exactly as link would.

The repository and branch are read from Git (the origin remote and the
checked-out branch) unless --repo and --branch say otherwise. The build pack
is detected from the application root the way Coolify's own form would, or
named with --build-pack:

  railpack       Coolify's default: detects the language and builds an
                 image. --install-command, --build-command, and
                 --start-command override what it detects; --static serves
                 the build output with nginx from --publish-dir (/dist).
  nixpacks       the same, through Nixpacks.
  static         serves the files as they are, with no build; chosen when
                 the root has an index.html and no package.json.
  dockerfile     builds the Dockerfile in the root, or the one --dockerfile
                 names.
  dockercompose  runs the compose file in the root (docker-compose.yaml,
                 docker-compose.yml, compose.yaml, or compose.yml, or the
                 one --compose-file names). Each service that should be
                 reachable takes a domain with --compose-domain SERVICE=URL;
                 without one, set the domains in Coolify afterwards. Ports
                 come from the compose file, so --port does not apply.

--port is what the application listens on: 3000 for a Railpack or Nixpacks
build, 80 for a Dockerfile or a static site unless you say otherwise. Check
it: Coolify routes traffic there.

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
			create.ComposeDomains = nil
			for _, value := range composeDomains {
				domain, err := service.ParseComposeDomain(value)
				if err != nil {
					return inputError(err)
				}
				create.ComposeDomains = append(create.ComposeDomains, domain)
			}
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
	command.Flags().StringVar(&create.BuildPack, "build-pack", "", "Build pack: railpack, nixpacks, static, dockerfile, or dockercompose (default: detected)")
	command.Flags().IntVar(&create.Port, "port", 0, "Port the application listens on (default: 3000 for railpack and nixpacks, 80 for dockerfile and static)")
	command.Flags().BoolVar(&create.Static, "static", false, "Serve the railpack or nixpacks build output as a static site")
	command.Flags().StringVar(&create.PublishDirectory, "publish-dir", "", "Directory to serve with --static or --build-pack static (default: /dist with --static)")
	command.Flags().StringVar(&create.InstallCommand, "install-command", "", "Install command for railpack and nixpacks (default: detected)")
	command.Flags().StringVar(&create.BuildCommand, "build-command", "", "Build command for railpack and nixpacks (default: detected)")
	command.Flags().StringVar(&create.StartCommand, "start-command", "", "Start command for railpack and nixpacks (default: detected)")
	command.Flags().StringVar(&create.Dockerfile, "dockerfile", "", "Dockerfile to build, relative to the application root (default: Dockerfile)")
	command.Flags().StringVar(&create.ComposeFile, "compose-file", "", "Compose file to run, relative to the application root (default: the one found)")
	command.Flags().StringArrayVar(&composeDomains, "compose-domain", nil, "Domain for one Compose service, SERVICE=URL (repeatable)")
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
