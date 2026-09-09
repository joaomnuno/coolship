package ui

import (
	"context"
	"errors"
	"fmt"
	"io"

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
func PrintError(w io.Writer, err error) error {
	if err == nil {
		return nil
	}
	_, writeErr := fmt.Fprintf(w, "Error: %s\n", singleLine(err.Error()))
	return writeErr
}
