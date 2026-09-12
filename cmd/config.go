package cmd

import (
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newConfigCommand(app Application, options *commandOptions, streams ui.Streams, prefs preferences.Report) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the effective configuration for this directory",
		Long: `Show the discovered configuration file, the selected target, the binding,
which credentials would be used, and the preferences file, after applying any
overrides. No request is made to Coolify and no token is shown.`,
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			result, err := app.Config(command.Context(), options.Options, prefs)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Config(result)
		},
	}
}
