package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/suggest"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// newConfigCommand builds config and its show, get, and set subcommands.
// report is the preferences file as the executable loaded it, once; get
// reads from it and set writes to its path.
func newConfigCommand(app Application, options *commandOptions, streams ui.Streams, report preferences.Report, order declaredOrder) *cobra.Command {
	show := func(command *cobra.Command) error {
		result, err := app.Config(command.Context(), options.Options)
		if err != nil {
			return err
		}
		return ui.NewRenderer(streams, options.format).Config(result)
	}
	keys := strings.Join(preferences.Names(), ", ")
	config := &cobra.Command{
		Use:   "config",
		Short: "Show the configuration and change your preferences",
		Long: `In a terminal, config opens a form that shows this directory's binding, where
credentials come from, and the instance, and edits your preferences:
verbosity, build_logs, color, hints, and update_check. Save writes only the
keys you changed to preferences.toml; Esc leaves without writing.

Piped, with --format json, or without a terminal, config prints the same
view as config show. No request is made to Coolify and no token is shown.`,
		Example: `  coolship config
  coolship config show --format json
  coolship config get verbosity
  coolship config set hints false`,
		Args: unknownSubcommand,
		RunE: func(command *cobra.Command, _ []string) error {
			if !ui.PreferencesEditable(streams, options.format) {
				return show(command)
			}
			input := ui.PreferencesForm{Report: report}
			result, err := app.Config(command.Context(), options.Options)
			if err != nil {
				if !errors.Is(err, service.ErrInput) {
					return err
				}
				input.ConfigError = err
			}
			input.Config = result
			if report.Path == "" {
				return preferencesLocationError(report)
			}
			changes, err := ui.EditPreferences(command.Context(), streams, input)
			if err != nil {
				return err
			}
			if len(changes) > 0 {
				if err := applyPreferences(report.Path, changes); err != nil {
					return err
				}
			}
			return ui.NewRenderer(streams, options.format).PreferencesSaved(report.Path, changes)
		},
	}
	showCommand := &cobra.Command{
		Use:   "show",
		Short: "Show the effective configuration for this directory",
		Long: `Show the discovered configuration file, the selected target, the binding,
which credentials would be used, and the preferences file, after applying any
overrides. No request is made to Coolify and no token is shown.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error { return show(command) },
	}
	getCommand := &cobra.Command{
		Use:   "get KEY",
		Short: "Print the value of one preference",
		Long: `Print the value in effect for one preference: the one preferences.toml
sets, or the default when it sets none. The keys are ` + keys + `.`,
		Example: `  coolship config get verbosity
  coolship config get hints --format json`,
		Args: exactPreferenceArgs(1, "config get KEY"),
		RunE: func(_ *cobra.Command, args []string) error {
			value, set, err := report.Preferences.Get(args[0])
			if err != nil {
				return preferenceInputError(err)
			}
			return ui.NewRenderer(streams, options.format).PreferenceGet(ui.PreferenceResult{Key: args[0], Value: value, Set: set, Path: report.Path})
		},
	}
	setCommand := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Change one preference",
		Long: `Write one preference to preferences.toml, creating the file when it is
absent. Other lines and comments in the file are kept. Setting build_logs to
auto removes the key. The keys and their values are:

` + describeKeys(),
		Example: `  coolship config set verbosity verbose
  coolship config set build_logs auto
  coolship config set hints false`,
		Args: exactPreferenceArgs(2, "config set KEY VALUE"),
		RunE: func(_ *cobra.Command, args []string) error {
			if _, err := preferences.Check(args[0], args[1]); err != nil {
				return preferenceInputError(err)
			}
			if report.Path == "" {
				return preferencesLocationError(report)
			}
			changes := []preferences.Change{{Key: args[0], Value: args[1]}}
			if err := applyPreferences(report.Path, changes); err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).PreferencesSaved(report.Path, changes)
		},
	}
	for _, child := range []*cobra.Command{showCommand, getCommand, setCommand} {
		config.AddCommand(child)
		order[config] = append(order[config], child)
	}
	return config
}

// exactPreferenceArgs refuses a wrong argument count as invalid input,
// naming the form to use.
func exactPreferenceArgs(count int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != count {
			return inputError(fmt.Errorf("expected %d argument(s), got %d; use coolship %s", count, len(args), usage))
		}
		return nil
	}
}

// applyPreferences writes changes and classifies the failure: a file that
// would not parse is the user's to fix (exit 2), a failed read or write is
// an operational failure (exit 1).
func applyPreferences(path string, changes []preferences.Change) error {
	err := preferences.Apply(path, changes)
	if err == nil {
		return nil
	}
	if errors.Is(err, preferences.ErrInvalidFile) {
		return inputError(fmt.Errorf("%w; fix or remove the file, nothing was written", err))
	}
	return preferenceInputError(err)
}

// preferenceInputError makes an unknown key or value invalid input, with
// the closest key when the name looks like a typo; other errors pass.
func preferenceInputError(err error) error {
	var keyErr *preferences.KeyError
	if errors.As(err, &keyErr) {
		return inputError(fmt.Errorf("unknown preference %q%s; the keys are %s",
			keyErr.Key, suggest.DidYouMean(keyErr.Key, preferences.Names()), strings.Join(preferences.Names(), ", ")))
	}
	var valueErr *preferences.ValueError
	if errors.As(err, &valueErr) {
		return inputError(err)
	}
	return err
}

func preferencesLocationError(report preferences.Report) error {
	if report.Err != nil {
		return inputError(fmt.Errorf("%w; set %s to a file path", report.Err, preferences.EnvPath))
	}
	return inputError(fmt.Errorf("no preferences file location; set %s to a file path", preferences.EnvPath))
}

// describeKeys lists each key with its values and what it does, for set's help.
func describeKeys() string {
	var lines []string
	for _, key := range preferences.Keys() {
		lines = append(lines, fmt.Sprintf("  %-13s %s (default %s)\n  %-13s %s", key.Name, strings.Join(key.Allowed, " | "), key.Default, "", key.Description))
	}
	return strings.Join(lines, "\n")
}
