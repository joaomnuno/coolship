package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

type fakeApplication struct {
	status func(context.Context, service.Options) (service.StatusResult, error)
	deploy func(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error)
	logs   func(context.Context, service.LogsOptions, service.Emitter) error
	link   func(context.Context, service.LinkOptions, service.Selector, service.Confirm) (service.LinkResult, error)
}

func (f fakeApplication) Status(ctx context.Context, options service.Options) (service.StatusResult, error) {
	return f.status(ctx, options)
}
func (f fakeApplication) Deploy(ctx context.Context, options service.DeployOptions, emit service.Emitter) (service.DeployResult, error) {
	return f.deploy(ctx, options, emit)
}
func (f fakeApplication) Logs(ctx context.Context, options service.LogsOptions, emit service.Emitter) error {
	return f.logs(ctx, options, emit)
}
func (f fakeApplication) Link(ctx context.Context, options service.LinkOptions, selectChoice service.Selector, confirm service.Confirm) (service.LinkResult, error) {
	return f.link(ctx, options, selectChoice, confirm)
}

func execute(t *testing.T, app cmd.Application, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic}, "test-version")
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}

func TestHelpAndVersionAreOffline(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"help"}, {"help", "deploy"}, {"status", "--help"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, diagnostic, err := execute(t, nil, args...)
			if err != nil || out == "" || diagnostic != "" {
				t.Fatalf("offline output = %q; stderr = %q; err = %v", out, diagnostic, err)
			}
		})
	}
	out, _, _ := execute(t, nil, "--help")
	for _, command := range []string{"link", "status", "deploy", "logs"} {
		if !strings.Contains(out, command) {
			t.Errorf("help omits %s", command)
		}
	}
	for _, command := range []string{"preview", "doctor", "completion"} {
		if strings.Contains(out, command) {
			t.Errorf("help advertises unimplemented command %s", command)
		}
	}
	out, _, _ = execute(t, nil, "--version")
	if out != "coolship test-version\n" {
		t.Fatalf("version = %q", out)
	}
	out, _, _ = execute(t, nil, "deploy", "--help")
	if !strings.Contains(out, "does not upload your worktree") || !strings.Contains(out, "remote deployment continues") {
		t.Fatalf("deploy help omits source or cancellation semantics: %s", out)
	}
}

func TestInvalidInputReturnsOneUnprintedError(t *testing.T) {
	for _, args := range [][]string{
		{"unknown"}, {"help", "unknown"}, {"help", "status", "extra"}, {"status", "extra"},
		{"status", "--unknown"}, {"status", "--config"}, {"status", "--format", "yaml"},
		{"logs", "-n", "0"}, {"logs", "-n", "-1"}, {"logs", "-n", "many"},
		{"deploy", "--timeout", "0s"}, {"deploy", "--timeout=-1s"}, {"deploy", "--timeout", "eventually"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, diagnostic, err := execute(t, nil, args...)
			if !errors.Is(err, service.ErrInput) || ui.ExitCode(err) != 2 {
				t.Fatalf("error = %v; want input error", err)
			}
			if out != "" || diagnostic != "" {
				t.Fatalf("command printed error before boundary: stdout=%q stderr=%q", out, diagnostic)
			}
		})
	}
}

func TestSharedFlagsAndStatusJSON(t *testing.T) {
	expected := service.Options{CWD: "nested", ConfigPath: "binding.toml", Context: "work", CoolifyConfig: "credentials.json", Environment: "staging"}
	app := fakeApplication{status: func(_ context.Context, options service.Options) (service.StatusResult, error) {
		if options != expected {
			t.Fatalf("options = %#v; want %#v", options, expected)
		}
		return service.StatusResult{Target: service.TargetInfo{Application: "web", ApplicationUUID: "app-1"}, Status: "running:healthy", Warnings: []string{"name changed"}}, nil
	}}
	out, diagnostic, err := execute(t, app, "--cwd", "nested", "status", "--config", "binding.toml", "--context", "work", "--coolify-config", "credentials.json", "-e", "staging", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var result service.StatusResult
	decoder := json.NewDecoder(strings.NewReader(out))
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if result.Status != "running:healthy" || result.Target.ApplicationUUID != "app-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if err := decoder.Decode(&result); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains data after result: %q", out)
	}
	if diagnostic != "Warning: name changed\n" {
		t.Fatalf("stderr = %q", diagnostic)
	}
}

func TestDeployFlagsAndProgressSeparation(t *testing.T) {
	app := fakeApplication{deploy: func(_ context.Context, options service.DeployOptions, emit service.Emitter) (service.DeployResult, error) {
		if !options.Force || !options.NoWait || options.Timeout != 30*time.Second || options.Context != "home" {
			t.Fatalf("deploy options = %#v", options)
		}
		for _, event := range []service.Event{
			{Type: "deployment", DeploymentUUID: "deployment-1", Status: "queued"},
			{Type: "logs", Logs: "build output"},
		} {
			if err := emit(event); err != nil {
				return service.DeployResult{}, err
			}
		}
		return service.DeployResult{DeploymentUUID: "deployment-1", Status: "queued"}, nil
	}}
	out, diagnostic, err := execute(t, app, "deploy", "--force", "--no-wait", "--timeout", "30s", "--context", "home", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var result service.DeployResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.DeploymentUUID != "deployment-1" {
		t.Fatalf("result = %q; err = %v", out, err)
	}
	if strings.Contains(out, "build output") || diagnostic != "Deployment deployment-1: queued\nbuild output\n" {
		t.Fatalf("stdout=%q stderr=%q", out, diagnostic)
	}
}

func TestWorkflowDefaults(t *testing.T) {
	app := fakeApplication{
		deploy: func(_ context.Context, options service.DeployOptions, _ service.Emitter) (service.DeployResult, error) {
			if options.Timeout != 10*time.Minute || options.Force || options.NoWait {
				t.Fatalf("deploy defaults = %#v", options)
			}
			return service.DeployResult{}, nil
		},
		logs: func(_ context.Context, options service.LogsOptions, _ service.Emitter) error {
			if options.Lines != 100 || options.Follow {
				t.Fatalf("logs defaults = %#v", options)
			}
			return nil
		},
	}
	for _, name := range []string{"deploy", "logs"} {
		if _, _, err := execute(t, app, name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLogsHumanAndNDJSON(t *testing.T) {
	events := []service.Event{{Type: "logs", Logs: "first\n"}, {Type: "warning", Message: "snapshot reset"}, {Type: "logs", Logs: "second\n"}}
	app := fakeApplication{logs: func(_ context.Context, options service.LogsOptions, emit service.Emitter) error {
		if !options.Follow || options.Lines != 20 {
			t.Fatalf("logs options = %#v", options)
		}
		for _, event := range events {
			if err := emit(event); err != nil {
				return err
			}
		}
		return nil
	}}
	out, diagnostic, err := execute(t, app, "logs", "-f", "-n", "20")
	if err != nil || out != "first\nsecond\n" || diagnostic != "Warning: snapshot reset\n" {
		t.Fatalf("human logs stdout=%q stderr=%q err=%v", out, diagnostic, err)
	}
	out, diagnostic, err = execute(t, app, "logs", "--follow", "--lines", "20", "--format", "json")
	if err != nil || diagnostic != "" {
		t.Fatalf("JSON logs stderr=%q err=%v", diagnostic, err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(events) {
		t.Fatalf("NDJSON = %q", out)
	}
	for index, line := range lines {
		var event service.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil || event != events[index] {
			t.Fatalf("event %d = %q; err=%v", index, line, err)
		}
	}
}

func TestLinkSelectors(t *testing.T) {
	app := fakeApplication{link: func(_ context.Context, options service.LinkOptions, _ service.Selector, _ service.Confirm) (service.LinkResult, error) {
		if options.Project != "Personal" || options.Environment != "production" || options.Application != "web" || options.ProjectUUID != "p-1" || options.EnvironmentUUID != "e-1" || options.ApplicationUUID != "a-1" || options.Root != "apps/web" || !options.Replace {
			t.Fatalf("link options = %#v", options)
		}
		return service.LinkResult{Path: "/project/coolship.toml", Target: service.TargetInfo{ApplicationUUID: "a-1"}}, nil
	}}
	out, diagnostic, err := execute(t, app, "link", "--project", "Personal", "--environment", "production", "--application", "web", "--project-uuid", "p-1", "--environment-uuid", "e-1", "--application-uuid", "a-1", "--root", "apps/web", "--replace", "--format", "json")
	var result service.LinkResult
	if err != nil || diagnostic != "" {
		t.Fatalf("stderr=%q err=%v", diagnostic, err)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Path != "/project/coolship.toml" {
		t.Fatalf("link result=%q err=%v", out, err)
	}
}

func TestLinkInteractivePromptsStayOnStderr(t *testing.T) {
	app := fakeApplication{link: func(ctx context.Context, _ service.LinkOptions, selectChoice service.Selector, confirm service.Confirm) (service.LinkResult, error) {
		id, err := selectChoice(ctx, "application", []service.Choice{{ID: "a-1", Name: "web"}, {ID: "a-2", Name: "api"}})
		if err != nil || id != "a-2" {
			t.Fatalf("selected=%q err=%v", id, err)
		}
		plan := service.LinkPlan{Path: "coolship.toml", Target: service.TargetInfo{Application: "api"}, Replacing: true}
		accepted, err := confirm(ctx, plan)
		if err != nil || !accepted {
			t.Fatalf("accepted=%v err=%v", accepted, err)
		}
		return service.LinkResult{Path: plan.Path, Target: plan.Target}, nil
	}}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("2\ny\n"), Out: &out, Err: &diagnostic, Interactive: true}, "test")
	root.SetArgs([]string{"link", "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) || !strings.Contains(diagnostic.String(), "Select application") || !strings.Contains(diagnostic.String(), "Confirm [y/N]") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), diagnostic.String())
	}
}

func TestLinkNoninteractiveMissingSelection(t *testing.T) {
	app := fakeApplication{link: func(ctx context.Context, _ service.LinkOptions, selectChoice service.Selector, _ service.Confirm) (service.LinkResult, error) {
		_, err := selectChoice(ctx, "application", []service.Choice{{ID: "a-1", Name: "web"}})
		return service.LinkResult{}, err
	}}
	out, diagnostic, err := execute(t, app, "link")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--application") || out != "" || diagnostic != "" {
		t.Fatalf("stdout=%q stderr=%q err=%v", out, diagnostic, err)
	}
}

func TestContextAndOperationalErrorReachBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := fakeApplication{status: func(passed context.Context, _ service.Options) (service.StatusResult, error) {
		if passed != ctx {
			t.Fatal("command replaced the supplied context")
		}
		return service.StatusResult{}, passed.Err()
	}}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic}, "test")
	root.SetArgs([]string{"status"})
	err := root.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) || ui.ExitCode(err) != 130 || out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q err=%v", out.String(), diagnostic.String(), err)
	}
	operationErr := errors.New("backend unavailable")
	app.status = func(context.Context, service.Options) (service.StatusResult, error) {
		return service.StatusResult{}, operationErr
	}
	_, _, err = execute(t, app, "status")
	if !errors.Is(err, operationErr) || ui.ExitCode(err) != 1 {
		t.Fatalf("operation error = %v", err)
	}
}
