package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	default:
		return 1
	}
}

// PrintError renders the single diagnostic owned by the executable boundary.
// An interrupt is reported as such, keeping any recovery detail wrapped around it.
func PrintError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if errors.Is(err, service.ErrCancelled) && !errors.Is(err, context.Canceled) {
		_, writeErr := fmt.Fprintln(w, "Cancelled")
		return writeErr
	}
	if errors.Is(err, context.Canceled) {
		text = strings.ReplaceAll(text, context.Canceled.Error(), "interrupted")
		if text == "interrupted" {
			_, writeErr := fmt.Fprintln(w, "Interrupted")
			return writeErr
		}
	}
	_, writeErr := fmt.Fprintf(w, "Error: %s\n", singleLine(text))
	return writeErr
}
