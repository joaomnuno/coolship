package cmd

import (
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// maxLogLines is the most the server returns in one snapshot.
const maxLogLines = 10000

func newLogsCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var logs service.LogsOptions
	command := &cobra.Command{
		Use:   "logs [target]",
		Short: "Read runtime logs from the linked application",
		Long: `Read runtime logs from the linked application.
Follow polls bounded snapshots; resets or missing overlap may produce gaps or
duplicates. Warnings report these cases. JSON output uses one event per line
(NDJSON), both for a single snapshot and when following.

The server reads the application's first container, up to 10000 lines per
snapshot; pull request preview containers cannot be tailed. An application
without a running container has no logs, which is reported as such.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if logs.Lines < 1 || logs.Lines > maxLogLines {
				return inputError(fmt.Errorf("--lines must be between 1 and %d", maxLogLines))
			}
			logs.Options = options.Options
			return app.Logs(command.Context(), logs, ui.NewRenderer(streams, options.format).LogEvent)
		},
	}
	command.Flags().BoolVarP(&logs.Follow, "follow", "f", false, "Poll new log snapshots until interrupted")
	command.Flags().IntVarP(&logs.Lines, "lines", "n", 100, "Maximum number of lines in each requested snapshot (1-10000)")
	return command
}
