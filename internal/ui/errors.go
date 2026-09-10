package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/service"
)

// ExitCode maps successful, interrupted, input, and operational outcomes.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled), errors.Is(err, service.ErrCancelled):
		return 130
	case errors.Is(err, service.ErrInput):
		return 2
	}
	var exit *service.ExitError
	if errors.As(err, &exit) && exit.Code > 0 {
		return exit.Code
	}
	return 1
}

// PrintError renders the single diagnostic owned by the executable boundary on
// stderr. An interrupt is reported as such, keeping any recovery detail
// wrapped around it.
func PrintError(streams Streams, err error) error {
	if err == nil {
		return nil
	}
	streams = streams.Normalized()
	w, style := streams.Err, streams.errPalette()
	// A child process has already said what it had to say; only its status is kept.
	var exit *service.ExitError
	if errors.As(err, &exit) && !errors.Is(err, context.Canceled) {
		return nil
	}
	text := err.Error()
	if errors.Is(err, service.ErrCancelled) && !errors.Is(err, context.Canceled) {
		_, writeErr := fmt.Fprintln(w, style.apply(dim, "Cancelled"))
		return writeErr
	}
	if errors.Is(err, context.Canceled) {
		text = strings.ReplaceAll(text, context.Canceled.Error(), "interrupted")
		if text == "interrupted" {
			_, writeErr := fmt.Fprintln(w, style.apply(dim, "Interrupted"))
			return writeErr
		}
	}
	// A rejected token, a missing ability, or a redirecting URL fails every
	// command the same way; the guidance is added here, once, so no workflow
	// has to know about HTTP.
	if hint := service.ServerHint(err); hint != "" {
		text += "; " + hint
	}
	_, writeErr := fmt.Fprintf(w, "%s %s\n", style.apply(redBold, "Error:"), singleLine(text))
	return writeErr
}
