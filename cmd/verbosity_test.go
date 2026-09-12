package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// TestVerbositySetsTheTraceBeforeTheCommandRuns checks the wiring: the
// level the flags, COOLSHIP_VERBOSITY, or the preference choose reaches the
// streams' trace before the workflow is called, stdout is untouched, and a
// bad environment value is invalid input except for help.
func TestVerbositySetsTheTraceBeforeTheCommandRuns(t *testing.T) {
	status := service.StatusResult{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}, Status: "running"}
	run := func(env map[string]string, prefs preferences.Preferences, args ...string) (ui.Verbosity, string, string, error) {
		t.Helper()
		trace := ui.NewTrace(nil)
		seen := ui.Verbosity(-1)
		app := fakeApplication{status: func(context.Context, service.Options) (service.StatusResult, error) {
			seen = trace.Level()
			return status, nil
		}}
		var out, diagnostic bytes.Buffer
		root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic, Trace: trace}, "test-version",
			cmd.WithEnvironment(func(name string) string { return env[name] }),
			cmd.WithPreferences(preferences.Report{Path: "prefs.toml", Present: true, Preferences: prefs}))
		root.SetArgs(args)
		err := root.ExecuteContext(context.Background())
		return seen, out.String(), diagnostic.String(), err
	}

	normalOut := ""
	for _, test := range []struct {
		name  string
		env   map[string]string
		prefs preferences.Preferences
		args  []string
		want  ui.Verbosity
	}{
		{"default", nil, preferences.Preferences{}, []string{"status"}, ui.VerbosityNormal},
		{"--verbose", nil, preferences.Preferences{}, []string{"status", "--verbose"}, ui.VerbosityVerbose},
		{"--debug before the verb", nil, preferences.Preferences{}, []string{"--debug", "status"}, ui.VerbosityDebug},
		{"environment", map[string]string{"COOLSHIP_VERBOSITY": "verbose"}, preferences.Preferences{}, []string{"status"}, ui.VerbosityVerbose},
		{"preference", nil, preferences.Preferences{Verbosity: "debug"}, []string{"status"}, ui.VerbosityDebug},
		{"flag beats preference", nil, preferences.Preferences{Verbosity: "debug"}, []string{"status", "--verbose"}, ui.VerbosityVerbose},
	} {
		seen, out, diagnostic, err := run(test.env, test.prefs, test.args...)
		if err != nil || seen != test.want || diagnostic != "" {
			t.Fatalf("%s: level=%v stderr=%q err=%v; want %v", test.name, seen, diagnostic, err, test.want)
		}
		// Verbosity never changes stdout.
		if normalOut == "" {
			normalOut = out
		} else if out != normalOut {
			t.Fatalf("%s: stdout changed: %q, want %q", test.name, out, normalOut)
		}
	}

	bad := map[string]string{"COOLSHIP_VERBOSITY": "loud"}
	if _, _, _, err := run(bad, preferences.Preferences{}, "status"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "COOLSHIP_VERBOSITY") {
		t.Fatalf("bad environment: %v", err)
	}
	if _, out, _, err := run(bad, preferences.Preferences{}, "help", "status"); err != nil || !strings.Contains(out, "--verbose") || !strings.Contains(out, "--debug") {
		t.Fatalf("help with a bad environment: out=%q err=%v", out, err)
	}
	// -v stays the version.
	if _, _, _, err := run(nil, preferences.Preferences{}, "-v"); err != nil {
		t.Fatalf("-v: %v", err)
	}
}

// TestNoLogsStillCollapsesTheBuildLogAboveNormal checks that --no-logs wins
// over --verbose on an interactive run, where no checklist is drawn: the
// build log stays off stderr while the deployment succeeds, and prints in
// full once the server says it failed.
func TestNoLogsStillCollapsesTheBuildLogAboveNormal(t *testing.T) {
	const log = "Building docker image started.\n"
	for _, status := range []string{"finished", "failed"} {
		t.Run(status, func(t *testing.T) {
			app := fakeApplication{deploy: func(_ context.Context, _ service.DeployOptions, emit service.Emitter) (service.DeployResult, error) {
				for _, event := range []service.Event{
					{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"},
					{Type: "build", DeploymentUUID: "d-1", Logs: log},
					{Type: "deployment", DeploymentUUID: "d-1", Status: status},
				} {
					if err := emit(event); err != nil {
						return service.DeployResult{}, err
					}
				}
				result := service.DeployResult{DeploymentUUID: "d-1", Status: status}
				if status == "failed" {
					return result, errors.New("deployment d-1 failed")
				}
				return result, nil
			}}
			var out, diagnostic bytes.Buffer
			root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic, Interactive: true, Trace: ui.NewTrace(nil)}, "test-version")
			root.SetArgs([]string{"deploy", "--verbose", "--no-logs"})
			err := root.ExecuteContext(context.Background())
			if (err != nil) != (status == "failed") {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(diagnostic.String(), log) != (status == "failed") {
				t.Fatalf("stderr = %q", diagnostic.String())
			}
			if !strings.Contains(diagnostic.String(), "Deployment d-1: "+status) {
				t.Fatalf("status lines missing: %q", diagnostic.String())
			}
		})
	}
}
