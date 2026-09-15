// Command coolship runs project-local Coolify workflows for the current repository.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
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
	"github.com/joaomnuno/coolship/internal/update"
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
		ColorOut:    colorEnabled(isTerminal(os.Stdout), os.Getenv, os.Args[1:]),
		ColorErr:    colorEnabled(isTerminal(os.Stderr), os.Getenv, os.Args[1:]),
		OutTerminal: isTerminal(os.Stdout), ErrTerminal: isTerminal(os.Stderr), Width: terminalWidth(os.Stdout),
		// The command tree sets the level from --verbose, --debug,
		// COOLSHIP_VERBOSITY, or the preferences before any backend exists.
		Trace: ui.NewTrace(os.Stderr)}
	runner := process.New(os.Stdin, os.Stdout, os.Stderr)
	// Preferences are read once here; a broken file becomes a warning the
	// command tree prints, never a failure. The service gets the same
	// report so Config assembles it into the result without a second read.
	prefs := preferences.Discover(os.Getenv)
	app := service.New(service.Dependencies{
		NewBackend: func(credentials auth.Credentials) (service.Backend, error) {
			return newBackend(credentials, streams.Trace)
		},
		CheckHealth: func(ctx context.Context, url string) error {
			return coolify.CheckHealth(ctx, url, clientOptions(streams.Trace)...)
		},
		CredentialURL:   os.Getenv("COOLSHIP_URL"),
		CredentialToken: os.Getenv("COOLSHIP_TOKEN"),
		RunProcess: func(ctx context.Context, spec service.ProcessSpec) (int, error) {
			return runner.Run(ctx, process.Spec{Dir: spec.Dir, Args: spec.Args, Shell: spec.Shell, Env: spec.Env})
		},
		InspectRepository: gitinfo.Inspect,
		ProbeRemote:       gitinfo.Heads,
		Preferences:       prefs,
	})
	info, ok := debug.ReadBuildInfo()
	resolved := resolveVersion(version, info, ok)
	// The release check runs beside the command and is abandoned, not
	// waited for, when the command finishes first.
	notifier := updateNotifier(prefs, resolved, os.Getenv, isTerminal(os.Stderr), os.Args[1:])
	if notifier != nil {
		notifier.Start(ctx)
	}
	// A failure Coolship can set right is offered a fix in there, once.
	err := cmd.Execute(ctx, app, streams, resolved, os.Args[1:], cmd.WithOpener(ui.OpenBrowser), cmd.WithEnvironment(os.Getenv),
		cmd.WithPreferences(prefs))
	// Repeated request lines still held back are summarized before anything
	// else reaches stderr.
	streams.Trace.Flush()
	if err != nil {
		// A failed diagnostic write cannot be reported anywhere else.
		format := "human"
		if jsonFormat(os.Args[1:]) {
			format = "json"
		}
		_ = ui.ReportError(streams, format, err)
	}
	if notifier != nil {
		// Last, after the output and any diagnostic; an interrupted run
		// gets no notice.
		if notice := notifier.Finish(ctx.Err() == nil); notice != "" {
			_, _ = fmt.Fprintln(os.Stderr, notice)
		}
	}
	return ui.ExitCode(err)
}

// updateNotifier returns the release notifier for this run, or nil when the
// run must not check: see update.Enabled, plus shell completion, whose stderr
// is not read by a person. The cache sits next to the preferences file.
func updateNotifier(prefs preferences.Report, version string, env func(string) string, stderrTerminal bool, args []string) *update.Notifier {
	if len(args) > 0 && (args[0] == "completion" || strings.HasPrefix(args[0], "__complete")) {
		return nil
	}
	if !update.Enabled(update.Conditions{Version: version, StderrTerminal: stderrTerminal, JSON: jsonFormat(args), Env: env,
		Preference: prefs.Preferences.UpdateCheck}) {
		return nil
	}
	if prefs.Path == "" {
		return nil
	}
	return &update.Notifier{Current: version, StatePath: filepath.Join(filepath.Dir(prefs.Path), update.StateFile),
		URL: update.DefaultURL, Client: &http.Client{}, UserAgent: "coolship/" + version}
}

// jsonFormat reports whether the arguments ask for --format json, read from
// raw argv as colorEnabled reads --no-color: the last occurrence wins and
// nothing after "--" counts.
func jsonFormat(args []string) bool {
	format := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--format" && i+1 < len(args) {
			format = args[i+1]
			i++
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--format="); ok {
			format = value
		}
	}
	return format == "json"
}

// debugUnredactedEnv, set to 1, makes debug output show request and response
// bodies with secret values in them, which are redacted otherwise.
const debugUnredactedEnv = "COOLSHIP_DEBUG_UNREDACTED"

// newBackend builds the Coolify client. Above normal verbosity every request
// attempt is reported to trace; at normal the client is given no trace at all.
func newBackend(credentials auth.Credentials, trace *ui.Trace) (service.Backend, error) {
	client, err := coolify.NewClient(credentials.URL, credentials.Token, clientOptions(trace)...)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// clientOptions identifies Coolship to the server and, above normal
// verbosity, reports every request attempt to trace, for the backend and the
// token-free health check alike.
func clientOptions(trace *ui.Trace) []coolify.Option {
	options := []coolify.Option{coolify.WithUserAgent("coolship/" + userAgentVersion())}
	if trace.Level() != ui.VerbosityNormal {
		options = append(options, coolify.WithTrace(func(exchange coolify.Exchange) { trace.Exchange(ui.Exchange(exchange)) }))
		if os.Getenv(debugUnredactedEnv) == "1" {
			options = append(options, coolify.WithUnredactedTrace())
		}
	}
	return options
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
//
// This scan happens on raw argv, before Cobra/pflag parse the command line,
// so --no-color=VALUE is decoded the same way pflag decodes any bool flag:
// via strconv.ParseBool, which accepts 1/t/T/TRUE/true/True and
// 0/f/F/FALSE/false/False. An explicit false form (or a value ParseBool does
// not recognize) leaves color as-is rather than forcing it off, and a later
// occurrence overrides an earlier one, matching how repeated flags behave.
func colorEnabled(terminal bool, env func(string) string, args []string) bool {
	if !terminal || env("NO_COLOR") != "" || env("TERM") == "dumb" || env("CI") != "" {
		return false
	}
	optOut := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--no-color" {
			optOut = true
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--no-color="); ok {
			if parsed, err := strconv.ParseBool(value); err == nil {
				optOut = parsed
			}
			continue
		}
	}
	return !optOut
}

// isTerminal asks the terminal driver, not the file mode: /dev/null is a
// character device too, and a redirect from it must count as noninteractive.
func isTerminal(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}

// terminalWidth is the width of a terminal in columns, or zero when the file
// is not one or reports no size.
func terminalWidth(file *os.File) int {
	if !isTerminal(file) {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return width
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
