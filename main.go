// Command coolship runs project-local Coolify workflows for the current repository.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// version is replaced at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() { os.Exit(run()) }

// run owns process concerns: signals, environment, streams, and the single
// diagnostic written before an exit code is chosen.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	streams := ui.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Interactive: interactive()}
	app := service.New(service.Dependencies{
		NewBackend:      newBackend,
		CredentialURL:   os.Getenv("COOLSHIP_URL"),
		CredentialToken: os.Getenv("COOLSHIP_TOKEN"),
	})
	err := cmd.NewRootCommand(app, streams, version, cmd.WithOpener(ui.OpenBrowser)).ExecuteContext(ctx)
	if err != nil {
		// A failed diagnostic write cannot be reported anywhere else.
		_ = ui.PrintError(streams.Err, err)
	}
	return ui.ExitCode(err)
}

func newBackend(credentials auth.Credentials) (service.Backend, error) {
	client, err := coolify.NewClient(credentials.URL, credentials.Token, coolify.WithUserAgent("coolship/"+version))
	if err != nil {
		return nil, err
	}
	return client, nil
}

// interactive reports whether prompts can be both written and answered. Prompts
// go to stderr, so a piped stdout alone does not disable them.
func interactive() bool {
	if os.Getenv("CI") != "" {
		return false
	}
	return isTerminal(os.Stdin) && isTerminal(os.Stderr)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
