package cmd

import (
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newOpenCommand(app Application, options *commandOptions, streams ui.Streams, openBrowser func(string) error) *cobra.Command {
	var open service.OpenOptions
	var printOnly bool
	command := &cobra.Command{
		Use:   "open [target]",
		Short: "Open the linked application, or its Coolify page, in a browser",
		Long: `Open the linked application's URL in the default browser.
The URL is always printed on stdout. When stdin is not a terminal, or with
--print, nothing is launched, so the command composes with other tools.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			open.Options = options.Options
			result, err := app.Open(command.Context(), open)
			if err != nil {
				return err
			}
			if err := ui.NewRenderer(streams, options.format).Open(result); err != nil {
				return err
			}
			if printOnly || openBrowser == nil || !streams.Interactive {
				return nil
			}
			if err := openBrowser(result.URL); err != nil {
				return fmt.Errorf("open browser (the URL is printed above): %w", err)
			}
			_, err = fmt.Fprintf(streams.Err, "Opening %s in your browser.\n", result.Kind)
			return err
		},
	}
	command.Flags().BoolVar(&open.Dashboard, "dashboard", false, "Open the application's page in Coolify instead of its public URL")
	command.Flags().BoolVar(&printOnly, "print", false, "Print the URL without launching a browser")
	return command
}
