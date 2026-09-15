package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCommand(app Application, options *commandOptions, streams ui.Streams) *cobra.Command {
	var stop service.StopOptions
	command := &cobra.Command{
		Use:   "stop [target]",
		Short: "Stop the linked application's containers",
		Long: `Stop the linked application. Coolify stops and removes its containers; the
application, its configuration, and its deployment history stay, and deploy or
start brings it back.

The application and its environment are shown and confirmed first, or --yes
skips the question. The command then waits until the status reports exited,
or --timeout passes, and reports the last status it saw. An application that
already reports exited is left alone with a warning; any other status is
stopped, including a crash loop shown as restarting or degraded.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if stop.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			stop.Options = options.Options
			renderer := ui.NewRenderer(streams, options.format)
			prompter := ui.NewPrompter(streams)
			steps := ui.NewSteps(streams, options.format, []string{"Request stop", "Wait for exited"})
			var result service.StopResult
			var err error
			if steps.Live() {
				result, err = stopWithSteps(command.Context(), app, stop, prompter, steps)
			} else {
				// Piped, JSON, and verbose runs keep their status lines.
				result, err = app.Stop(command.Context(), stop, prompter.ConfirmStop, renderer.DeploymentEvent)
			}
			if err != nil {
				return err
			}
			return renderer.Stop(result)
		},
	}
	command.Flags().BoolVarP(&stop.Yes, "yes", "y", false, "Stop without confirmation")
	command.Flags().DurationVar(&stop.Timeout, "timeout", 2*time.Minute, "Maximum time to wait for the application to stop")
	return command
}

// stopWithSteps runs stop as a checklist: the request, then the wait for
// exited with the last status seen beside it. The question comes before the
// checklist is drawn, so the service is always handed the confirmation and
// --yes answers it here; either way, accepting is where the request starts.
func stopWithSteps(ctx context.Context, app Application, options service.StopOptions, prompter *ui.Prompter, steps *ui.Steps) (service.StopResult, error) {
	defer steps.Close()
	yes := options.Yes
	options.Yes = false
	current := -1
	confirm := func(ctx context.Context, plan service.StopPlan) (bool, error) {
		if !yes {
			if accepted, err := prompter.ConfirmStop(ctx, plan); err != nil || !accepted {
				return accepted, err
			}
		}
		steps.Start(0)
		current = 0
		return true, nil
	}
	emit := func(event service.Event) error {
		switch {
		case event.Type == "warning":
			return steps.Warn(event.Message)
		case event.Type == "application" && event.Message != "":
			// The server's receipt ends the request.
			steps.Done(0, event.Message)
			steps.Start(1)
			current = 1
			steps.Note(1, event.Status)
		case event.Type == "application":
			steps.Note(1, event.Status)
		}
		return nil
	}
	result, err := app.Stop(ctx, options, confirm, emit)
	switch {
	case err != nil && current >= 0:
		steps.Fail(current, err)
	case err == nil && current == 1:
		steps.Done(1, result.Status)
	}
	return result, err
}

func newStartCommand(app Application, options *commandOptions, streams ui.Streams, prefs preferences.Preferences) *cobra.Command {
	var start service.StartOptions
	var logs logFlags
	command := &cobra.Command{
		Use:   "start [target]",
		Short: "Start the linked application by deploying it again",
		Long: `Start the linked application. Coolify brings a stopped application back by
deploying its configured source and branch again, so this queues a deployment
and observes it exactly like deploy, build log included. It does not upload
your worktree or push local commits.

Use --no-wait to return the queued deployment UUID immediately. In a terminal
the deployment is shown as a stage checklist with the build log collapsed, as
deploy does; --logs streams the log, --no-logs keeps it collapsed, the
build_logs preference decides when neither is given, and without that the
verbosity: collapsed when normal, streamed with --verbose or --debug.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if start.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			showLogs, err := buildLogs(logs, prefs.BuildLogs, options.verbosity)
			if err != nil {
				return err
			}
			start.Options = options.Options
			return observeDeployment(streams, options, start.NoWait, showLogs, func(emit service.Emitter) (service.DeployResult, error) {
				return app.Start(command.Context(), start, emit)
			})
		},
	}
	command.Flags().BoolVar(&start.Force, "force", false, "Force Coolify to rebuild without cache")
	command.Flags().BoolVar(&start.NoWait, "no-wait", false, "Return after submission without observing completion")
	command.Flags().DurationVar(&start.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for the deployment to finish")
	logs.register(command.Flags())
	return command
}

func newRestartCommand(app Application, options *commandOptions, streams ui.Streams, prefs preferences.Preferences) *cobra.Command {
	var restart service.StartOptions
	var logs logFlags
	command := &cobra.Command{
		Use:   "restart [target]",
		Short: "Restart the linked application's containers",
		Long: `Restart the linked application. Coolify queues a restart-only deployment and
this command observes it exactly like deploy. Applications built from a
Dockerfile or a Docker image are deployed in full; the other build packs reuse
the image already built for the commit when it still exists.

The restart is confirmed first, or --yes skips the question. Use --no-wait to
return the queued deployment UUID immediately. In a terminal the deployment is
shown as a stage checklist with the build log collapsed, as deploy does;
--logs streams the log, --no-logs keeps it collapsed, the build_logs
preference decides when neither is given, and without that the verbosity:
collapsed when normal, streamed with --verbose or --debug.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if restart.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			showLogs, err := buildLogs(logs, prefs.BuildLogs, options.verbosity)
			if err != nil {
				return err
			}
			restart.Options = options.Options
			return observeDeployment(streams, options, restart.NoWait, showLogs, func(emit service.Emitter) (service.DeployResult, error) {
				return app.Restart(command.Context(), restart, ui.NewPrompter(streams).ConfirmRestart, emit)
			})
		},
	}
	command.Flags().BoolVarP(&restart.Yes, "yes", "y", false, "Restart without confirmation")
	command.Flags().BoolVar(&restart.NoWait, "no-wait", false, "Return after submission without observing completion")
	command.Flags().DurationVar(&restart.Timeout, "timeout", 10*time.Minute, "Maximum time to wait for the restart to finish")
	logs.register(command.Flags())
	return command
}
