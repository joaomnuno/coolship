package cmd

import (
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
			result, err := app.Stop(command.Context(), stop, ui.NewPrompter(streams).ConfirmStop, renderer.DeploymentEvent)
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
deploy does; --logs streams the log, --no-logs keeps it collapsed, and the
build_logs preference decides when neither is given.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if start.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			showLogs, err := buildLogs(logs, prefs.BuildLogs)
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
--logs streams the log, --no-logs keeps it collapsed, and the build_logs
preference decides when neither is given.`,
		Args: targetArg(options),
		RunE: func(command *cobra.Command, _ []string) error {
			if restart.Timeout <= 0 {
				return inputError(errors.New("--timeout must be greater than zero"))
			}
			showLogs, err := buildLogs(logs, prefs.BuildLogs)
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
