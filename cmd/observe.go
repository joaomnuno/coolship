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
//  3. the verbosity default, which is collapsed for now, since verbosity
//     does not exist yet (verbose and debug will stream once it lands).
//
// Off a terminal, and with --format json, the answer is moot: the build log
// streams as it always did. --no-wait observes nothing, so it is moot too.
func buildLogs(flags logFlags, preference *bool) (bool, error) {
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
	return false, nil
}

// observeDeployment runs one workflow that queues and observes a deployment
// (deploy, preview, start, restart) and renders its result the way every one
// of them does. In a terminal the events draw the stage checklist; off one,
// or with --format json, or with --no-wait, where there is nothing to
// observe, they print plainly as before.
func observeDeployment(streams ui.Streams, options *commandOptions, noWait, logs bool, run func(service.Emitter) (service.DeployResult, error)) error {
	renderer := ui.NewRenderer(streams, options.format)
	emit := renderer.DeploymentEvent
	var checklist *ui.Checklist
	if !noWait {
		checklist = ui.NewChecklist(streams, options.format, logs)
		emit = checklist.Event
	}
	result, err := run(emit)
	if err != nil {
		if checklist != nil {
			// The failure is what the caller must learn; a lost write cannot displace it.
			_ = checklist.Close(true)
		}
		return deploymentFailure(renderer, options.format, result, err)
	}
	if checklist != nil {
		if err := checklist.Close(false); err != nil {
			return err
		}
	}
	return renderer.Deploy(result)
}
