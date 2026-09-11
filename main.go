// Command coolship runs project-local Coolify workflows for the current repository.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/gitinfo"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/process"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
	"golang.org/x/term"
)

// version is set at build time with -ldflags "-X main.version=<tag>"; see
// scripts/build. A plain go build leaves it empty and the VCS revision Go
// records in the binary is reported instead.
var version string

func main() { os.Exit(run()) }

// resolveVersion prefers the build-time version, then the module version a
// `go install …@vX.Y.Z` records, then the VCS revision from build info, then
// "dev", so --version always says which code is running.
func resolveVersion(built string, info *debug.BuildInfo, ok bool) string {
	if built != "" {
		return built
	}
	if !ok || info == nil {
		return "dev"
	}
	if module := info.Main.Version; module != "" && module != "(devel)" {
		return module
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += "-dirty"
	}
	return "dev (" + revision + ")"
}

// run owns process concerns: signals, environment, streams, and the single
// diagnostic written before an exit code is chosen.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	streams := ui.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Interactive: interactive(),
		ColorOut: colorEnabled(isTerminal(os.Stdout), os.Getenv, os.Args[1:]),
		ColorErr: colorEnabled(isTerminal(os.Stderr), os.Getenv, os.Args[1:])}
	runner := process.New(os.Stdin, os.Stdout, os.Stderr)
	app := service.New(service.Dependencies{
		NewBackend:      newBackend,
		CredentialURL:   os.Getenv("COOLSHIP_URL"),
		CredentialToken: os.Getenv("COOLSHIP_TOKEN"),
		RunProcess: func(ctx context.Context, spec service.ProcessSpec) (int, error) {
			return runner.Run(ctx, process.Spec{Dir: spec.Dir, Args: spec.Args, Shell: spec.Shell, Env: spec.Env})
		},
		InspectRepository: gitinfo.Inspect,
		ProbeRemote:       gitinfo.Heads,
	})
	info, ok := debug.ReadBuildInfo()
	resolved := resolveVersion(version, info, ok)
	// Preferences are read once here; a broken file becomes a warning the
	// command tree prints, never a failure.
	err := cmd.NewRootCommand(app, streams, resolved, cmd.WithOpener(ui.OpenBrowser), cmd.WithEnvironment(os.Getenv),
		cmd.WithPreferences(preferences.Discover(os.Getenv))).ExecuteContext(ctx)
	if err != nil {
		// A failed diagnostic write cannot be reported anywhere else.
		_ = ui.PrintError(streams, err)
	}
	return ui.ExitCode(err)
}

func newBackend(credentials auth.Credentials) (service.Backend, error) {
	client, err := coolify.NewClient(credentials.URL, credentials.Token, coolify.WithUserAgent("coolship/"+userAgentVersion()))
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

// colorEnabled decides ANSI styling for one stream. Styling needs a terminal
// on that stream, and is turned off by NO_COLOR (https://no-color.org), a dumb
// terminal, CI, or --no-color. Only this function decides; ui receives the
// result as a capability and never inspects the environment.
func colorEnabled(terminal bool, env func(string) string, args []string) bool {
	if !terminal || env("NO_COLOR") != "" || env("TERM") == "dumb" || env("CI") != "" {
		return false
	}
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--no-color" || arg == "--no-color=true" {
			return false
		}
	}
	return true
}

// isTerminal asks the terminal driver, not the file mode: /dev/null is a
// character device too, and a redirect from it must count as noninteractive.
func isTerminal(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}

// userAgentVersion keeps the header short and free of spaces or parentheses.
func userAgentVersion() string {
	info, ok := debug.ReadBuildInfo()
	resolved := resolveVersion(version, info, ok)
	if strings.HasPrefix(resolved, "dev") {
		return "dev"
	}
	return resolved
}
