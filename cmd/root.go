// Package cmd translates CLI arguments into project workflow requests.
package cmd

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// declaredOrder maps a command to its children in the order this tree
// declared them, for a command that has more than one and cares which
// comes first: root's five task groups, env's pull/diff/push, and
// completion's bash/zsh/fish/powershell. unsortedUsageFunc renders from
// this instead of Cobra's own Commands(), which sorts a command's children
// slice in place — and, once cobra.EnableCommandSorting is on (the
// package-wide default, left untouched here), permanently — the first
// time anything reads it. That includes paths this tree does not control,
// such as `__complete` and the default help command's ValidArgsFunction
// (see issue #30), so relying on Cobra's own bookkeeping surviving until
// render time is not safe. Each NewRootCommand builds its own map and
// never mutates it after construction, so nothing here is shared or
// racing with any other command tree the same process builds.
type declaredOrder map[*cobra.Command][]*cobra.Command

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
	var verbosity verbosityFlags
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
			// Verbosity is decided before anything reaches the server, so
			// the first request is already traced. A bad COOLSHIP_VERBOSITY
			// fails a command, like a bad flag, but not help or completion.
			level, err := resolveVerbosity(verbosity, config.environment, prefs.Verbosity)
			if err != nil && !offline(command) {
				return err
			}
			options.verbosity = level
			streams.Trace.SetLevel(level)
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
	verbosity.register(root.PersistentFlags())
	// The help lists the commands in five task-shaped groups, each in the
	// order a project moves through its verbs rather than alphabetically;
	// every command stays a flat verb. unsortedUsageFunc, installed below
	// once every command is attached, renders that order from the
	// declaredOrder map built alongside it, never from Cobra's own
	// Commands() (see declaredOrder and issue #30).
	order := declaredOrder{}
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
			newEnvCommand(app, options, streams, order), newDomainCommand(app, options, streams),
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
			order[root] = append(order[root], command)
		}
	}
	root.SetCompletionCommandGroupID(maintainGroupID)
	order[root] = append(order[root], ownCompletionCommand(root, order))
	helpCommand := &cobra.Command{
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
	}
	root.SetHelpCommand(helpCommand)
	root.SetHelpCommandGroupID(maintainGroupID)
	order[root] = append(order[root], helpCommand)
	root.SetUsageFunc(unsortedUsageFunc(order))
	return root
}

// unsortedUsageFunc returns a usage renderer equivalent to Cobra's own
// default (defaultUsageFunc in spf13/cobra@v1.10.2's command.go — the two
// should stay in sync, same as that function's own comment asks of its
// template twin), except that it lists a command's children (root's five
// task groups; env's pull, diff, push; completion's bash, zsh, fish,
// powershell) in the order this tree declared them, from order, rather
// than from Cobra's own Commands().
//
// It does not call Cobra's real renderer for that listing, because
// Commands() sorts a command's children slice in place — and, since
// cobra.EnableCommandSorting is the package-wide default this stays
// deliberately untouched, permanently — the first time anything reads it
// while sorting is on. That includes paths this tree does not control,
// such as `__complete` and the default help command's ValidArgsFunction
// (see issue #30), so a render that asked Cobra's own function to do the
// listing could never be sure its own Commands() call would not just sort
// the tree again on the spot. declaredCommands asks Commands() only for
// the current set of children — whatever order that comes back in, sorted
// or not, is irrelevant — and resequences a local copy from order before
// this prints them, so the result is always the declared order regardless
// of what Cobra's bookkeeping has done. Nothing here reads or writes any
// package-global switch, so concurrent use of a Coolship tree, or of any
// other Cobra tree in the same process, cannot race on this.
func unsortedUsageFunc(order declaredOrder) func(*cobra.Command) error {
	return func(c *cobra.Command) error {
		// LocalFlags and InheritedFlags below merge persistent flags down
		// from parents themselves — mirroring defaultUsageFunc, which does
		// not call mergePersistentFlags either; only UsageFunc's own
		// wrapper does, before invoking whichever function this is.
		w := c.OutOrStderr()
		fmt.Fprint(w, "Usage:")
		if c.Runnable() {
			fmt.Fprintf(w, "\n  %s", c.UseLine())
		}
		if c.HasAvailableSubCommands() {
			fmt.Fprintf(w, "\n  %s [command]", c.CommandPath())
		}
		if len(c.Aliases) > 0 {
			fmt.Fprintf(w, "\n\nAliases:\n  %s", c.NameAndAliases())
		}
		if c.HasExample() {
			fmt.Fprintf(w, "\n\nExamples:\n%s", c.Example)
		}
		if c.HasAvailableSubCommands() {
			cmds := declaredCommands(c, order)
			if len(c.Groups()) == 0 {
				fmt.Fprint(w, "\n\nAvailable Commands:")
				for _, subcmd := range cmds {
					if subcmd.IsAvailableCommand() || subcmd.Name() == "help" {
						fmt.Fprintf(w, "\n  %s %s", rpad(subcmd.Name(), subcmd.NamePadding()), subcmd.Short)
					}
				}
			} else {
				for _, group := range c.Groups() {
					fmt.Fprintf(w, "\n\n%s", group.Title)
					for _, subcmd := range cmds {
						if subcmd.GroupID == group.ID && (subcmd.IsAvailableCommand() || subcmd.Name() == "help") {
							fmt.Fprintf(w, "\n  %s %s", rpad(subcmd.Name(), subcmd.NamePadding()), subcmd.Short)
						}
					}
				}
				if !c.AllChildCommandsHaveGroup() {
					fmt.Fprint(w, "\n\nAdditional Commands:")
					for _, subcmd := range cmds {
						if subcmd.GroupID == "" && (subcmd.IsAvailableCommand() || subcmd.Name() == "help") {
							fmt.Fprintf(w, "\n  %s %s", rpad(subcmd.Name(), subcmd.NamePadding()), subcmd.Short)
						}
					}
				}
			}
		}
		if c.HasAvailableLocalFlags() {
			fmt.Fprintf(w, "\n\nFlags:\n%s", strings.TrimRightFunc(c.LocalFlags().FlagUsages(), unicode.IsSpace))
		}
		if c.HasAvailableInheritedFlags() {
			fmt.Fprintf(w, "\n\nGlobal Flags:\n%s", strings.TrimRightFunc(c.InheritedFlags().FlagUsages(), unicode.IsSpace))
		}
		if c.HasHelpSubCommands() {
			fmt.Fprint(w, "\n\nAdditional help topics:")
			for _, subcmd := range declaredCommands(c, order) {
				if subcmd.IsAdditionalHelpTopicCommand() {
					fmt.Fprintf(w, "\n  %s %s", rpad(subcmd.CommandPath(), subcmd.CommandPathPadding()), subcmd.Short)
				}
			}
		}
		if c.HasAvailableSubCommands() {
			fmt.Fprintf(w, "\n\nUse %q for more information about a command.", c.CommandPath()+" [command] --help")
		}
		fmt.Fprintln(w)
		return nil
	}
}

// rpad right-pads s with spaces to padding width, matching Cobra's own
// unexported helper of the same name and behavior.
func rpad(s string, padding int) string {
	return fmt.Sprintf("%-*s", padding, s)
}

// declaredCommands returns c's current children (whatever Commands()
// reports — its order is not relied on) resequenced to match the order
// this tree declared them in, from order. A command order has no entry
// for (nothing recorded it, or it has zero or one child so order cannot
// be wrong) comes back exactly as Commands() reported it. A child
// Commands() reports that order does not know about — which should not
// happen, since every command this tree attaches is recorded — sorts
// after every declared one, stably.
func declaredCommands(c *cobra.Command, order declaredOrder) []*cobra.Command {
	cmds := c.Commands()
	declared, ok := order[c]
	if !ok {
		return cmds
	}
	position := make(map[*cobra.Command]int, len(declared))
	for i, child := range declared {
		position[child] = i
	}
	ordered := make([]*cobra.Command, len(cmds))
	copy(ordered, cmds)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, iKnown := position[ordered[i]]
		pj, jKnown := position[ordered[j]]
		if iKnown && jKnown {
			return pi < pj
		}
		return iKnown && !jKnown
	})
	return ordered
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

// completionShells are the shells Cobra's InitDefaultCompletionCmd attaches
// to the completion command, in the order it attaches them
// (completions.go's `completionCmd.AddCommand(bash, zsh, fish, powershell)`,
// stable across Cobra's history since the set is not configurable). Naming
// them lets ownCompletionCommand find each one with Find, which reads a
// command's children directly and never sorts them, rather than through
// Commands(), which would (see declaredOrder and issue #30).
var completionShells = []string{"bash", "zsh", "fish", "powershell"}

// ownCompletionCommand keeps Cobra's completion command available and listed
// (`coolship completion bash|zsh|fish|powershell`), but creates it now so its
// argument failures are this tree's: an unknown shell would otherwise print
// the help page and exit 0, and an extra argument exit 1 with Cobra's words,
// where every other command reports invalid input. It records completion's
// own children into order and returns the completion command so the caller
// can record its place among root's children too.
func ownCompletionCommand(root *cobra.Command, order declaredOrder) *cobra.Command {
	root.InitDefaultCompletionCmd()
	completion, _, err := root.Find([]string{"completion"})
	if err != nil || completion == root {
		// InitDefaultCompletionCmd only skips creating the command when a
		// same-named one already exists, which this tree never adds itself.
		panic("coolship: completion command missing after InitDefaultCompletionCmd")
	}
	var shells []string
	for _, name := range completionShells {
		shell, _, err := completion.Find([]string{name})
		if err != nil || shell == completion {
			continue
		}
		shell.Args = noArgs
		shells = append(shells, name)
		order[completion] = append(order[completion], shell)
	}
	sortedShells := slices.Clone(shells)
	slices.Sort(sortedShells) // the error message lists shells alphabetically, regardless of help order
	completion.Args = func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return inputError(fmt.Errorf("unknown shell %q; use one of %s", args[0], strings.Join(sortedShells, ", ")))
		}
		return nil
	}
	completion.RunE = func(command *cobra.Command, _ []string) error { return command.Help() }
	return completion
}
