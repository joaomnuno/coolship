// Package cmd translates CLI arguments into project workflow requests.
package cmd

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// maintainGroupID is the help group that also lists help and completion.
const maintainGroupID = "maintain"

// Application is the workflow contract consumed by the command tree.
type Application interface {
	Status(context.Context, service.Options) (service.StatusResult, error)
	Deploy(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error)
	Stop(context.Context, service.StopOptions, service.ConfirmStop, service.Emitter) (service.StopResult, error)
	Start(context.Context, service.StartOptions, service.Emitter) (service.DeployResult, error)
	Restart(context.Context, service.StartOptions, service.ConfirmRestart, service.Emitter) (service.DeployResult, error)
	Deployments(context.Context, service.DeploymentsOptions) (service.DeploymentsResult, error)
	Cancel(context.Context, service.CancelOptions, service.ConfirmCancel) (service.CancelResult, error)
	Logs(context.Context, service.LogsOptions, service.Emitter) error
	Link(context.Context, service.LinkOptions, service.Selector, service.Confirm) (service.LinkResult, error)
	Init(context.Context, service.InitOptions, service.Selector, service.ConfirmInit, service.Emitter) (service.InitResult, error)
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
	preferences preferences.Report
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

// WithPreferences supplies the developer's preferences as the executable
// loaded them once, before the tree runs. A report whose Err is set is
// warned about at the start of every command except help, completion,
// --help, and --version; it never fails a command. Without the option,
// config shows no Preferences line.
func WithPreferences(report preferences.Report) Option {
	return func(s *settings) { s.preferences = report }
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
	// A file that could not be read left the zero value: no preference.
	prefs := config.preferences.Preferences
	root := &cobra.Command{
		Use:           "coolship",
		Short:         "Project-local deployment workflows for Coolify",
		Long:          "Link this repository to a Coolify application — an existing one with link, or a new one with init — then inspect, deploy, stop, start, open, and read the logs of that application.",
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
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			if options.format != "human" && options.format != "json" {
				return inputError(fmt.Errorf("unsupported format %q; use human or json", options.format))
			}
			// A typo in the preferences file is noticed once, here, and
			// never stops a deploy; help and completion stay silent.
			if err := config.preferences.Err; err != nil && !offline(command) {
				message := "Preferences not read: " + err.Error()
				if config.preferences.Path != "" {
					message = "Ignoring " + err.Error()
				}
				return ui.NewRenderer(streams, options.format).Warn(message)
			}
			return nil
		},
	}
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)
	root.SetVersionTemplate("coolship {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return inputError(err) })
	root.PersistentFlags().StringVar(&options.CWD, "cwd", "", "Use this working directory without changing the process directory")
	root.PersistentFlags().StringVar(&options.ConfigPath, "config", "", "Project configuration path, relative to the effective working directory")
	root.PersistentFlags().StringVar(&options.Context, "context", "", "Coolify CLI instance name for this invocation")
	root.PersistentFlags().StringVar(&options.CoolifyConfig, "coolify-config", "", "Read credentials from this Coolify CLI configuration file")
	root.PersistentFlags().StringVarP(&options.Environment, "environment", "e", "", "Remote environment name for this invocation")
	root.PersistentFlags().StringVarP(&options.Target, "target", "t", "", "Named target in a monorepo configuration ([apps.<name>])")
	root.PersistentFlags().StringVar(&options.format, "format", "human", "Output format: human or json (logs uses NDJSON)")
	// The executable reads --no-color from the arguments before the command
	// tree exists, because styling is a stream capability decided with the
	// streams; the flag is declared here so parsing accepts and documents it.
	root.PersistentFlags().Bool("no-color", false, "Disable styled output (NO_COLOR does the same)")
	// The help lists the commands in five task-shaped groups, each in the
	// order a project moves through its verbs rather than alphabetically;
	// every command stays a flat verb. Cobra's sorting switch is package-wide,
	// so it is set here, where the order is decided.
	cobra.EnableCommandSorting = false
	groups := []struct {
		group    cobra.Group
		commands []*cobra.Command
	}{
		{cobra.Group{ID: "start", Title: "Get started"}, []*cobra.Command{
			newLoginCommand(app, options, streams), newInitCommand(app, options, streams), newLinkCommand(app, options, streams),
		}},
		{cobra.Group{ID: "ship", Title: "Ship"}, []*cobra.Command{
			newDeployCommand(app, options, streams, prefs), newPreviewCommand(app, options, streams, config.environment, prefs),
			newCancelCommand(app, options, streams), newDeploymentsCommand(app, options, streams),
		}},
		{cobra.Group{ID: "run", Title: "Run"}, []*cobra.Command{
			newStartCommand(app, options, streams, prefs), newStopCommand(app, options, streams), newRestartCommand(app, options, streams, prefs),
			newStatusCommand(app, options, streams), newLogsCommand(app, options, streams),
			newOpenCommand(app, options, streams, config.openBrowser),
		}},
		{cobra.Group{ID: "configure", Title: "Configure"}, []*cobra.Command{
			newEnvCommand(app, options, streams), newDomainCommand(app, options, streams),
			newConfigCommand(app, options, streams), newDevCommand(app, options, streams),
		}},
		{cobra.Group{ID: maintainGroupID, Title: "Maintain"}, []*cobra.Command{
			newDoctorCommand(app, options, streams), newUnlinkCommand(app, options, streams), newLogoutCommand(app, options, streams),
		}},
	}
	for _, entry := range groups {
		group := entry.group
		root.AddGroup(&group)
		for _, command := range entry.commands {
			command.GroupID = group.ID
			root.AddCommand(command)
		}
	}
	root.SetCompletionCommandGroupID(maintainGroupID)
	ownCompletionCommand(root)
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command [subcommand]]",
		Short: "Help about a command",
		// Nested commands such as `help env pull` are found by the whole path.
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
	root.SetHelpCommandGroupID(maintainGroupID)
	return root
}

// offline reports the commands that read nothing and so have no use for a
// preferences warning: help, the completion scripts, and a bare invocation
// of the root command. Cobra passes the found command to
// PersistentPreRunE, so a bare `coolship` arrives here as the root command
// itself, with no parent; its RunE prints the help page exactly like
// `coolship help` does, so it is treated the same way.
func offline(command *cobra.Command) bool {
	if command.Parent() == nil {
		return true
	}
	for c := command; c != nil; c = c.Parent() {
		if c.Name() == "help" || c.Name() == "completion" {
			return true
		}
	}
	return false
}

// ownCompletionCommand keeps Cobra's completion command available and listed
// (`coolship completion bash|zsh|fish|powershell`), but creates it now so its
// argument failures are this tree's: an unknown shell would otherwise print
// the help page and exit 0, and an extra argument exit 1 with Cobra's words,
// where every other command reports invalid input.
func ownCompletionCommand(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	for _, completion := range root.Commands() {
		if completion.Name() != "completion" {
			continue
		}
		var shells []string
		for _, shell := range completion.Commands() {
			shell.Args = noArgs
			shells = append(shells, shell.Name())
		}
		slices.Sort(shells) // Cobra lists the shells in registration order once sorting is off
		completion.Args = func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return inputError(fmt.Errorf("unknown shell %q; use one of %s", args[0], strings.Join(shells, ", ")))
			}
			return nil
		}
		completion.RunE = func(command *cobra.Command, _ []string) error { return command.Help() }
	}
}
