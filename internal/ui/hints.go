package ui

import (
	"fmt"
	"strings"
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

func withTarget(command, target string) string {
	if target == "" || target == "default" {
		return command
	}
	return command + " --target " + shellQuote(target)
}
