// Package process runs one local child process on behalf of a workflow. It
// owns the platform shell, inherited environment, signal forwarding, and the
// exit status, and nothing about Coolify.
package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Spec describes the child. Exactly one of Args or Shell is set: Args runs
// directly, Shell runs through the platform shell so pipes and quoting work
// the way a configured command line expects.
type Spec struct {
	Dir   string
	Args  []string
	Shell string
	Env   []string // KEY=VALUE pairs layered over the inherited environment
}

// Runner attaches the given streams to every child it runs.
type Runner struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	grace  time.Duration
}

func New(stdin io.Reader, stdout, stderr io.Writer) *Runner {
	return &Runner{stdin: stdin, stdout: stdout, stderr: stderr, grace: 5 * time.Second}
}

// Run waits for the child and returns its exit status. Cancelling the context
// sends an interrupt and, after a grace period, kills the child. A status of
// -1 with an error means the child could not be started or was killed.
func (r *Runner) Run(ctx context.Context, spec Spec) (int, error) {
	if len(spec.Args) == 0 && spec.Shell == "" {
		return -1, errors.New("no command to run")
	}
	if len(spec.Args) > 0 && spec.Shell != "" {
		return -1, errors.New("a command runs either directly or through the shell, not both")
	}
	args := spec.Args
	if spec.Shell != "" {
		if runtime.GOOS == "windows" {
			args = []string{"cmd", "/C", spec.Shell}
		} else {
			args = []string{"sh", "-c", spec.Shell}
		}
	}
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = spec.Dir
	command.Env = append(os.Environ(), spec.Env...)
	command.Stdin, command.Stdout, command.Stderr = r.stdin, r.stdout, r.stderr
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
	command.WaitDelay = r.grace
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		if ctx.Err() != nil {
			return exit.ExitCode(), ctx.Err()
		}
		return exit.ExitCode(), nil
	default:
		return -1, err
	}
}
