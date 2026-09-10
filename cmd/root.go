// Package cmd translates CLI arguments into project workflow requests.
package cmd

import (
	"context"
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// Application is the workflow contract consumed by the command tree.
type Application interface {
	Status(context.Context, service.Options) (service.StatusResult, error)
	Deploy(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error)
	Logs(context.Context, service.LogsOptions, service.Emitter) error
	Link(context.Context, service.LinkOptions, service.Selector, service.Confirm) (service.LinkResult, error)
	Open(context.Context, service.OpenOptions) (service.OpenResult, error)
	Unlink(context.Context, service.UnlinkOptions, service.ConfirmUnlink) (service.UnlinkResult, error)
	Config(context.Context, service.Options) (service.ConfigResult, error)
	Doctor(context.Context, service.Options) (service.DoctorResult, error)
	EnvPull(context.Context, service.EnvOptions) (service.EnvPullResult, error)
	EnvDiff(context.Context, service.EnvOptions) (service.EnvDiffResult, error)
	EnvPush(context.Context, service.EnvPushOptions, service.ConfirmPush) (service.EnvPushResult, error)
	Dev(context.Context, service.DevOptions, service.Emitter) error
	Domain(context.Context, service.Options) (service.DomainResult, error)
	DomainSet(context.Context, service.DomainSetOptions, service.ConfirmDomain) (service.DomainSetResult, error)
	Login(context.Context, service.LoginOptions) (service.LoginResult, error)
	Logout(context.Context, service.LogoutOptions) (service.LogoutResult, error)
}

// Option configures process-level behavior the command tree cannot own.
type Option func(*settings)

type settings struct {
	openBrowser func(string) error
	environment func(string) string
}

// WithOpener supplies the browser launcher used by open. Without one, open
// only prints the URL.
func WithOpener(open func(string) error) Option {
	return func(s *settings) { s.openBrowser = open }
}

// WithEnvironment supplies process environment lookup for commands that can
// infer a value from CI, such as preview reading GITHUB_REF. Without it,
// nothing is inferred.
func WithEnvironment(lookup func(string) string) Option {
	return func(s *settings) { s.environment = lookup }
}

// NewRootCommand constructs an offline command tree with explicit dependencies.
func NewRootCommand(app Application, streams ui.Streams, version string, opts ...Option) *cobra.Command {
	streams = streams.Normalized()
	var config settings
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}
	options := &commandOptions{format: "human"}
	root := &cobra.Command{
		Use:           "coolship",
		Short:         "Project-local deployment workflows for Coolify",
		Long:          "Link this repository to an existing Coolify application, then inspect, deploy, open, and read the logs of that application.",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(command *cobra.Command, args []string) error {
			if len(args) != 0 {
				return inputError(fmt.Errorf("unknown command %q; run 'coolship help' for available commands", args[0]))
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error { return command.Help() },
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if options.format != "human" && options.format != "json" {
				return inputError(fmt.Errorf("unsupported format %q; use human or json", options.format))
			}
			return nil
		},
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	root.SetVersionTemplate("coolship {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return inputError(err) })
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&options.CWD, "cwd", "", "Use this working directory without changing the process directory")
	root.PersistentFlags().StringVar(&options.ConfigPath, "config", "", "Project configuration path, relative to the effective working directory")
	root.PersistentFlags().StringVar(&options.Context, "context", "", "Coolify CLI instance name for this invocation")
	root.PersistentFlags().StringVar(&options.CoolifyConfig, "coolify-config", "", "Read credentials from this Coolify CLI configuration file")
	root.PersistentFlags().StringVarP(&options.Environment, "environment", "e", "", "Remote environment name for this invocation")
	root.PersistentFlags().StringVarP(&options.Target, "target", "t", "", "Named target in a monorepo configuration ([apps.<name>])")
	root.PersistentFlags().StringVar(&options.format, "format", "human", "Output format: human or json (logs uses NDJSON)")
	root.AddCommand(newLinkCommand(app, options, streams), newStatusCommand(app, options, streams),
		newDeployCommand(app, options, streams), newLogsCommand(app, options, streams),
		newOpenCommand(app, options, streams, config.openBrowser), newUnlinkCommand(app, options, streams),
		newConfigCommand(app, options, streams), newDoctorCommand(app, options, streams),
		newEnvCommand(app, options, streams), newPreviewCommand(app, options, streams, config.environment),
		newDevCommand(app, options, streams), newDomainCommand(app, options, streams),
		newLoginCommand(app, options, streams), newLogoutCommand(app, options, streams))
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: "Help about a command",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return inputError(fmt.Errorf("help accepts at most one command"))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			command, remaining, err := root.Find(args)
			if err != nil {
				return inputError(err)
			}
			if len(remaining) != 0 {
				return inputError(fmt.Errorf("unknown help topic %q", remaining[0]))
			}
			command.InitDefaultHelpFlag()
			command.InitDefaultVersionFlag()
			return command.Help()
		},
	})
	return root
}
