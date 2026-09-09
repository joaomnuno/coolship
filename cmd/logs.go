package cmd

import (
	"errors"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newLogsCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var logs service.LogsOptions
	command := &cobra.Command{
		Use:   "logs [target]",
		Short: "Read runtime logs from the linked application",
		Long: `Read runtime logs from the linked application.
Follow polls bounded snapshots; resets or missing overlap may produce gaps or
duplicates. Warnings report these cases. JSON output uses one event per line
(NDJSON), both for a single snapshot and when following.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if logs.Lines <= 0 {
				return inputError(errors.New("--lines must be greater than zero"))
			}
			logs.Options = options.Options
			return app.Logs(command.Context(), logs, ui.NewRenderer(streams, options.format).LogEvent)
		},
	}
	command.Flags().BoolVarP(&logs.Follow, "follow", "f", false, "Poll new log snapshots until interrupted")
	command.Flags().IntVarP(&logs.Lines, "lines", "n", 100, "Maximum number of lines in each requested snapshot")
	return command
}
