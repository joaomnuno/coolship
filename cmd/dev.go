package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newDevCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var dev service.DevOptions
	command := &cobra.Command{
		Use:   "dev [target] [-- command...]",
		Short: "Run a local command with the application's variables",
		Long: `Run a local command in the application root with the linked application's
runtime variables injected over your environment, so the process sees what it
would see on Coolify without a .env file.

A command after -- runs directly. Without one, the binding's dev setting runs
through the shell:

    [project]
    dev = "npm run dev"

Shared references are injected as the values they resolve to. Values Coolify
withholds are reported and left to your environment. The command's exit
status becomes coolship's.`,
		Args: func(command *cobra.Command, args []string) error {
			dash := command.ArgsLenAtDash()
			if dash < 0 {
				dash = len(args)
			}
			dev.Command = args[dash:]
			return targetArg(options)(command, args[:dash])
		},
		RunE: func(command *cobra.Command, _ []string) error {
			dev.Options = options.Options
			return app.Dev(command.Context(), dev, ui.NewRenderer(streams, options.format).DeploymentEvent)
		},
	}
	command.Flags().BoolVar(&dev.Preview, "preview", false, "Inject the preview-deployment scope instead of the regular one")
	return command
}
