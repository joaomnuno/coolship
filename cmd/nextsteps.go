package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

// showNextSteps prints up to three next steps chosen from the application's
// state after link or init. The state is read only when the hints reach a
// person; when it cannot be read, the plain deploy hint stands in, unless a
// question about deploying follows.
func showNextSteps(ctx context.Context, app Application, options *commandOptions, streams ui.Streams, hints ui.NextStepOptions) {
	if !options.hintsShown(streams) {
		return
	}
	state, err := app.ProjectState(ctx, options.Options)
	if err != nil {
		if !hints.NoDeploy {
			options.hint(streams, ui.NextDeploy(hints.Target))
		}
		return
	}
	ui.Hints(streams, options.format, ui.NextSteps(state, hints))
}

// offerDeploy asks "Deploy now?" after init, defaulting to no, and runs
// deploy for the same target when the answer is yes. End of input or Esc is
// a no.
func offerDeploy(command *cobra.Command, options *commandOptions, streams ui.Streams) error {
	yes, err := ui.NewPrompter(streams).YesNo(command.Context(), "Deploy now?", false)
	switch {
	case errors.Is(err, service.ErrCancelled):
		// End the question's line, which the answer never did.
		_, _ = fmt.Fprintln(streams.Err)
		return nil
	case err != nil:
		return err
	case !yes:
		return nil
	}
	return options.run(command.Context(), append([]string{"deploy"}, globalArgs(command.Root())...))
}
