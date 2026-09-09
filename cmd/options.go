package cmd

import (
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/spf13/cobra"
)

type commandOptions struct {
	service.Options
	format string
}

func noArgs(command *cobra.Command, args []string) error {
	if err := cobra.NoArgs(command, args); err != nil {
		return inputError(err)
	}
	return nil
}

func inputError(err error) error { return &service.InputError{Err: err} }
