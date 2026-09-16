package ui

import (
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/service"
)

// Hint writes one next-step suggestion on stderr, dimmed, for a person at a
// terminal: only for human output on interactive streams, which the
// executable already turns off for pipes and CI. JSON, piped, and CI runs
// never see it, so no script has to filter it out. A hint that cannot be
// written is not worth failing a command that succeeded, so write errors
// are dropped.
func Hint(streams Streams, format, text string) {
	streams = streams.Normalized()
	if format != "human" || !streams.Interactive || strings.TrimSpace(text) == "" {
		return
	}
	_, _ = fmt.Fprintln(streams.Err, streams.errPalette().apply(dim, singleLine(text)))
}

// NextDeploy is the hint after a directory is bound, or when it has nothing
// deployed yet. A named target is repeated so the command works as printed
// even when the target was chosen with --target.
func NextDeploy(target string) string {
	return "Next: " + withTarget("coolship deploy", target)
}

// FailedDeployHint points at the two places a failed deployment is followed
// up: the list of recent deployments and the application in Coolify.
func FailedDeployHint(target string) string {
	return "Next: " + withTarget("coolship deployments", target) + " to compare with earlier runs, or " + withTarget("coolship open --dashboard", target) + " to retry from Coolify"
}

// NextStep is one suggested command and what it does.
type NextStep struct {
	Command string
	Purpose string
}

// NextStepOptions shape the choice of next steps: Compose names the
// SERVICE=URL form of domain set that a Compose application takes, which
// the state reports too once the application was read, and NoDeploy leaves
// out deploy when a question about deploying follows instead.
type NextStepOptions struct {
	Target   string
	Compose  bool
	NoDeploy bool
}

// maxNextSteps keeps the suggestions short enough to be read.
const maxNextSteps = 3

// NextSteps chooses at most three suggestions from what the application has:
// variables in a local .env the server lacks, no domain of its own, and
// then deploy when it was never deployed, or logs and open when it was.
func NextSteps(state service.ProjectState, options NextStepOptions) []NextStep {
	var steps []NextStep
	if state.EnvFile != "" && state.RemoteVariables == 0 {
		noun := "variables"
		if state.LocalVariables == 1 {
			noun = "variable"
		}
		steps = append(steps, NextStep{withTarget("coolship env push", options.Target),
			fmt.Sprintf("send the %d %s in %s to Coolify", state.LocalVariables, noun, singleLine(state.EnvFile))})
	}
	if len(state.Domains) == 0 || state.Generated {
		command, purpose := "coolship domain set URL", "give the application a domain"
		switch {
		case options.Compose || state.Compose:
			command, purpose = "coolship domain set SERVICE=URL", "give each service a domain"
		case state.Generated:
			purpose = "replace the generated domain with your own"
		}
		steps = append(steps, NextStep{withTarget(command, options.Target), purpose})
	}
	switch {
	case !state.Deployed && !options.NoDeploy:
		steps = append(steps, NextStep{withTarget("coolship deploy", options.Target), "build and start it"})
	case state.Deployed:
		steps = append(steps,
			NextStep{withTarget("coolship logs", options.Target), "read its logs"},
			NextStep{withTarget("coolship open", options.Target), "open it in a browser"})
	}
	if len(steps) > maxNextSteps {
		steps = steps[:maxNextSteps]
	}
	return steps
}

// Hints writes next steps as one dimmed block on stderr, with the commands
// aligned, under the same conditions as Hint.
func Hints(streams Streams, format string, steps []NextStep) {
	streams = streams.Normalized()
	if format != "human" || !streams.Interactive || len(steps) == 0 {
		return
	}
	width := 0
	for _, step := range steps {
		width = max(width, len([]rune(singleLine(step.Command))))
	}
	lines := []string{"Next:"}
	for _, step := range steps {
		command := singleLine(step.Command)
		lines = append(lines, "  "+command+strings.Repeat(" ", width-len([]rune(command)))+"  "+singleLine(step.Purpose))
	}
	style := streams.errPalette()
	for _, line := range lines {
		_, _ = fmt.Fprintln(streams.Err, style.apply(dim, line))
	}
}

func withTarget(command, target string) string {
	if target == "" || target == "default" {
		return command
	}
	return command + " --target " + shellQuote(target)
}
