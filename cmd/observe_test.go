package cmd_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// TestStageEventsPrintPlainlyOffATerminal drives deploy, start, restart, and
// preview with the events a real deployment produces, stderr being a buffer:
// each stage is one plain line among the status lines and the build log, in
// the order it arrived, and the target on the first event prints nothing.
func TestStageEventsPrintPlainlyOffATerminal(t *testing.T) {
	target := service.TargetInfo{Application: "web", ApplicationUUID: "app-1", Environment: "production"}
	events := []service.Event{
		{Type: "warning", Message: "the proxy learns the domain later"},
		{Type: "deployment", DeploymentUUID: "d-1", Status: "queued", Target: &target},
		{Type: "deployment", DeploymentUUID: "d-1", Status: "in_progress"},
		{Type: "build", DeploymentUUID: "d-1", Logs: "Starting deployment.\nBuilding docker image started.\n"},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageStarted},
		{Type: "build", DeploymentUUID: "d-1", Logs: "Building docker image completed.\n"},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageBuild, Status: service.StageDone},
		{Type: "build", DeploymentUUID: "d-1", Logs: "Rolling update started.\nNew container started.\n"},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageRollingUpdate, Status: service.StageStarted},
		{Type: "stage", DeploymentUUID: "d-1", Stage: service.StageContainer, Status: service.StageStarted},
		{Type: "deployment", DeploymentUUID: "d-1", Status: "finished"},
	}
	result := service.DeployResult{Target: target, DeploymentUUID: "d-1", Status: "finished", URL: "https://web.example.com", URLKind: "application"}
	replay := func(emit service.Emitter) (service.DeployResult, error) {
		for _, event := range events {
			if err := emit(event); err != nil {
				return service.DeployResult{}, err
			}
		}
		return result, nil
	}
	app := fakeApplication{
		deploy: func(_ context.Context, _ service.DeployOptions, emit service.Emitter) (service.DeployResult, error) {
			return replay(emit)
		},
		start: func(_ context.Context, _ service.StartOptions, emit service.Emitter) (service.DeployResult, error) {
			return replay(emit)
		},
		rstart: func(_ context.Context, _ service.StartOptions, _ service.ConfirmRestart, emit service.Emitter) (service.DeployResult, error) {
			return replay(emit)
		},
	}
	want := "Warning: the proxy learns the domain later\n" +
		"Deployment d-1: queued\n" +
		"Deployment d-1: in_progress\n" +
		"Starting deployment.\nBuilding docker image started.\n" +
		"Stage build: started\n" +
		"Building docker image completed.\n" +
		"Stage build: done\n" +
		"Rolling update started.\nNew container started.\n" +
		"Stage rolling update: started\n" +
		"Stage container: started\n" +
		"Deployment d-1: finished\n"
	for _, args := range [][]string{{"deploy"}, {"deploy", "--logs"}, {"deploy", "--no-logs"}, {"start"}, {"restart", "--yes"}, {"preview", "--pr", "7"}} {
		out, diagnostic, err := execute(t, app, args...)
		if err != nil || diagnostic != want || out != "Deployment: d-1\nApplication: web (app-1)\nStatus: finished\nhttps://web.example.com\n" {
			t.Fatalf("%v: out=%q stderr=%q err=%v", args, out, diagnostic, err)
		}
		// --format json keeps the same stderr and prints the result on stdout.
		out, diagnostic, err = execute(t, app, append(args, "--format", "json")...)
		if err != nil || diagnostic != want || !strings.Contains(out, `"deployment_uuid":"d-1"`) || strings.Contains(out, "Stage") {
			t.Fatalf("%v json: out=%q stderr=%q err=%v", args, out, diagnostic, err)
		}
	}
	// A preference does not change piped output either.
	streams := ui.Streams{}
	yes := true
	root := cmd.NewRootCommand(app, streams, "test-version", cmd.WithPreferences(preferences.Report{Path: "prefs.toml", Present: true, Preferences: preferences.Preferences{BuildLogs: &yes}}))
	root.SetArgs([]string{"deploy"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestLogFlagsAreExclusiveAndDocumented checks the two flags on every
// command that observes a deployment: both at once is invalid input before
// anything runs, and the help names them.
func TestLogFlagsAreExclusiveAndDocumented(t *testing.T) {
	app := fakeApplication{
		deploy: func(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error) {
			t.Fatal("deploy ran with contradictory flags")
			return service.DeployResult{}, nil
		},
		start: func(context.Context, service.StartOptions, service.Emitter) (service.DeployResult, error) {
			t.Fatal("start ran with contradictory flags")
			return service.DeployResult{}, nil
		},
		rstart: func(context.Context, service.StartOptions, service.ConfirmRestart, service.Emitter) (service.DeployResult, error) {
			t.Fatal("restart ran with contradictory flags")
			return service.DeployResult{}, nil
		},
	}
	for _, args := range [][]string{{"deploy"}, {"start"}, {"restart", "--yes"}, {"preview", "--pr", "7"}} {
		_, _, err := execute(t, app, append(args, "--logs", "--no-logs")...)
		if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--logs and --no-logs cannot be combined") {
			t.Fatalf("%v with both flags: %v", args, err)
		}
		out, _, err := execute(t, app, append(args[:1:1], "--help")...)
		if err != nil || !strings.Contains(out, "--logs ") || !strings.Contains(out, "--no-logs ") {
			t.Fatalf("%s --help: err=%v out=%q", args[0], err, out)
		}
	}
	// stop observes no deployment and has no such flags.
	if _, _, err := execute(t, app, "stop", "--logs"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("stop --logs: %v", err)
	}
}
