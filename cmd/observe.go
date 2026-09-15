package cmd

import (
	"errors"

	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"github.com/spf13/pflag"
)

// logFlags are --logs and --no-logs, the per-run switch for build log
// visibility while a deployment is observed in a terminal.
type logFlags struct {
	logs, noLogs bool
}

func (f *logFlags) register(flags *pflag.FlagSet) {
	flags.BoolVar(&f.logs, "logs", false, "Stream the build log above the stage checklist (terminal only)")
	flags.BoolVar(&f.noLogs, "no-logs", false, "Keep the build log collapsed; it prints in full if the deployment fails (terminal only)")
}

// buildLogs decides whether build log lines stream while a deployment is
// observed in a terminal. Three sources, and the first that speaks wins:
//
//  1. the run's --logs or --no-logs flag; both together is an input error;
//  2. the build_logs key of the preferences file, when the key is set;
//  3. the verbosity: normal collapses, verbose and debug stream.
//
// Off a terminal, and with --format json, the answer is moot: the build log
// streams as it always did. --no-wait observes nothing, so it is moot too.
func buildLogs(flags logFlags, preference *bool, verbosity ui.Verbosity) (bool, error) {
	switch {
	case flags.logs && flags.noLogs:
		return false, inputError(errors.New("--logs and --no-logs cannot be combined"))
	case flags.logs:
		return true, nil
	case flags.noLogs:
		return false, nil
	case preference != nil:
		return *preference, nil
	}
	return verbosity != ui.VerbosityNormal, nil
}

// observeDeployment runs one workflow that queues and observes a deployment
// (deploy, preview, start, restart) and renders its result the way every one
// of them does. In a terminal the events draw the stage checklist and a
// finished deployment ends in one summary line and its URL; off one, or with
// --format json, they print plainly as before. With --no-wait there is
// nothing to observe: the result names the queued deployment, so its queued
// status line is not printed on stderr as well.
func observeDeployment(streams ui.Streams, options *commandOptions, noWait, logs bool, run func(service.Emitter) (service.DeployResult, error)) error {
	renderer := ui.NewRenderer(streams, options.format)
	emit := func(event service.Event) error {
		if event.Type == "deployment" && event.Message == "" {
			return nil
		}
		return renderer.DeploymentEvent(event)
	}
	var checklist *ui.Checklist
	if !noWait {
		checklist = ui.NewChecklist(streams, options.format, logs)
		emit = checklist.Event
	}
	result, err := run(emit)
	if err != nil {
		if checklist != nil {
			// The failure is what the caller must learn; a lost write cannot displace it.
			_ = checklist.Close(deploymentOutcome(result.Status))
		}
		err = deploymentFailure(renderer, options.format, result, err)
		// A deployment the server failed has a next step; an observation
		// that only stopped (a timeout, Ctrl-C) does not. Hint prints only
		// for human output on interactive streams.
		if result.DeploymentUUID != "" && deploymentOutcome(result.Status) == ui.OutcomeFailed {
			options.hint(streams, ui.FailedDeployHint(options.Target))
		}
		return err
	}
	if checklist != nil {
		return checklist.Finish(result)
	}
	return renderer.Deploy(result)
}

// deploymentOutcome reads how observation ended from the deployment's last
// observed status, which is the only thing that says whether the deployment
// failed. A --timeout that elapses, a Ctrl-C, or a poll that fails all end
// observation with an error while the deployment is still queued or running
// on the server; none of them is a failed deployment, so the checklist says
// observation stopped and the build log stays collapsed. Only the server's
// own verdict prints the log.
func deploymentOutcome(status string) ui.Outcome {
	switch status {
	case "failed", "cancelled-by-user":
		return ui.OutcomeFailed
	}
	return ui.OutcomeStopped
}
