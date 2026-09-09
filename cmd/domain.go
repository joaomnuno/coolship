package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDomainCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	group := &cobra.Command{
		Use:   "domain [target]",
		Short: "Show or change the linked application's domains",
		Long: `Show the linked application's domains. Coolify generates one from the
application UUID until you set your own; domain set replaces the list.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			result, err := app.Domain(command.Context(), options.Options)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).Domain(result)
		},
	}
	var set service.DomainSetOptions
	setCommand := &cobra.Command{
		Use:   "set DOMAIN [DOMAIN...]",
		Short: "Replace the application's domains",
		Long: `Replace the application's domains with the given ones. A bare host such as
app.example.com means https://app.example.com. The change is confirmed first,
or requires --yes when noninteractive, and reaches the proxy on the next
deployment.`,
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(command, args); err != nil {
				return inputError(err)
			}
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			set.Options = options.Options
			set.Domains = args
			result, err := app.DomainSet(command.Context(), set, ui.NewPrompter(streams).ConfirmDomain)
			if err != nil {
				return err
			}
			return ui.NewRenderer(streams, options.format).DomainSet(result)
		},
	}
	setCommand.Flags().StringVar(&set.Redirect, "redirect", "", "Redirect policy: www, non-www, or both")
	setCommand.Flags().BoolVar(&set.Force, "force", false, "Bypass Coolify's check that a domain is unused on the server")
	setCommand.Flags().BoolVarP(&set.Yes, "yes", "y", false, "Change without confirmation")
	group.AddCommand(setCommand)
	return group
}
