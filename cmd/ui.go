package cmd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// menuAnnotation marks a command the menu lists only through its
// subcommands, because on its own it prints help: env, whose work is pull,
// diff, and push.
const menuAnnotation = "coolship.menu"

// menuSubcommandsOnly is menuAnnotation's value for such a command.
const menuSubcommandsOnly = "subcommands-only"

// menuRefresh is how often the menu reads the header again.
const menuRefresh = 5 * time.Second

// menuInputs are the values a verb cannot run without, keyed by command
// path. The menu asks for them with an inline prompt before running the
// verb; TestMenuAsksForEveryRequiredArgument keeps this in step with each
// command's Use line. preview's --pr is here because outside GitHub Actions
// nothing else supplies it.
var menuInputs = map[string][]ui.MenuInput{
	"preview": {{Title: "Pull request number", Flag: "pr", Placeholder: "42", Validate: ui.RequirePositiveNumber}},
	"domain set": {{Title: "Domains", Description: "Separate several with spaces; a bare host means https.",
		Placeholder: "app.example.com", Fields: true, Validate: ui.RequireText}},
	"logout":     {{Title: "Context to remove", Placeholder: "home", Validate: ui.RequireText}},
	"config get": {{Title: "Preference", Placeholder: "verbosity", Validate: ui.RequireText}},
	"config set": {
		{Title: "Preference", Placeholder: "verbosity", Validate: ui.RequireText},
		{Title: "Value", Placeholder: "verbose", Validate: ui.RequireText},
	},
}

// menuRunner opens the menu and returns the chosen verb's arguments;
// ui.RunMenu unless a test supplies another with withMenuRunner.
type menuRunner func(context.Context, ui.Streams, ui.MenuOptions) ([]string, error)

// withMenuRunner replaces the terminal menu, so tests can choose a verb
// without a terminal.
func withMenuRunner(run menuRunner) Option {
	return func(s *settings) { s.runMenu = run }
}

func newUICommand(app Application, options *commandOptions, streams ui.Streams, root *cobra.Command, order declaredOrder, run menuRunner) *cobra.Command {
	if run == nil {
		run = ui.RunMenu
	}
	return &cobra.Command{
		Use:   "ui",
		Short: "(experimental) Open a menu of this directory's application and verbs",
		Long: `Open a full-screen menu: the linked application, its status, and its last
deployment at the top, read again every few seconds, and below them the verbs
in the same groups and order as the help.

Arrow keys or j and k move, typing filters, and Enter runs the chosen verb
exactly as if it had been typed, with the same global flags, once the menu has
closed. A verb that needs a value, such as domain set, asks for it first. Esc,
q, or Ctrl-C leaves without running anything.

The menu needs a terminal on stdin and stdout. It is experimental: its keys
and layout may change, and it may be removed.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			args, err := run(command.Context(), streams, ui.MenuOptions{
				Groups: menuGroups(root, order, command),
				Status: func(ctx context.Context) (service.StatusResult, error) {
					return app.Status(ctx, options.Options)
				},
				NotLinked: func(err error) bool { return errors.Is(err, project.ErrNotLinked) },
				Refresh:   menuRefresh,
				Now:       time.Now,
			})
			if err != nil || args == nil {
				return err
			}
			root.SetArgs(withGlobalFlags(args, changedGlobalFlags(command)))
			return root.ExecuteContext(command.Context())
		},
	}
}

// withGlobalFlags places flags among a chosen verb's arguments: before the
// "--" that guards its positional answers, or at the end when there is none,
// so a flag is never read as a positional value.
func withGlobalFlags(args, flags []string) []string {
	boundary := slices.Index(args, "--")
	if boundary < 0 {
		return append(slices.Clone(args), flags...)
	}
	return slices.Concat(args[:boundary], flags, args[boundary:])
}

// menuGroups reads the menu's groups and verbs from the command tree itself:
// root's help groups in their order, and in each the commands the help
// lists, in the order recorded in order. Help, completion, and the menu
// itself are left out; a command with subcommands contributes each of them
// after itself, unless it only prints help.
func menuGroups(root *cobra.Command, order declaredOrder, self *cobra.Command) []ui.MenuGroup {
	var groups []ui.MenuGroup
	for _, group := range root.Groups() {
		entry := ui.MenuGroup{Title: group.Title}
		for _, command := range declaredCommands(root, order) {
			if command.GroupID != group.ID || command == self || offline(command) || !command.IsAvailableCommand() {
				continue
			}
			entry.Verbs = append(entry.Verbs, menuVerbs(command, order)...)
		}
		if len(entry.Verbs) > 0 {
			groups = append(groups, entry)
		}
	}
	return groups
}

func menuVerbs(command *cobra.Command, order declaredOrder) []ui.MenuVerb {
	var verbs []ui.MenuVerb
	if command.Annotations[menuAnnotation] != menuSubcommandsOnly {
		verbs = append(verbs, menuVerb(command))
	}
	if !command.HasAvailableSubCommands() {
		return verbs
	}
	for _, child := range declaredCommands(command, order) {
		if child.IsAvailableCommand() {
			verbs = append(verbs, menuVerbs(child, order)...)
		}
	}
	return verbs
}

func menuVerb(command *cobra.Command) ui.MenuVerb {
	path := strings.Fields(strings.TrimPrefix(command.CommandPath(), command.Root().Name()))
	return ui.MenuVerb{Path: path, Short: command.Short, Inputs: menuInputs[strings.Join(path, " ")]}
}

// changedGlobalFlags returns the global flags given to ui, as typed, so the
// chosen verb runs with them. Flags left at their defaults are not repeated.
func changedGlobalFlags(command *cobra.Command) []string {
	var args []string
	command.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Changed {
			args = append(args, "--"+flag.Name+"="+flag.Value.String())
		}
	})
	return args
}
