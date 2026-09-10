package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

type fakeApplication struct {
	status func(context.Context, service.Options) (service.StatusResult, error)
	deploy func(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error)
	logs   func(context.Context, service.LogsOptions, service.Emitter) error
	link   func(context.Context, service.LinkOptions, service.Selector, service.Confirm) (service.LinkResult, error)
	init   func(context.Context, service.InitOptions, service.Selector, service.ConfirmInit, service.Emitter) (service.InitResult, error)
	open   func(context.Context, service.OpenOptions) (service.OpenResult, error)
	unlink func(context.Context, service.UnlinkOptions, service.ConfirmUnlink) (service.UnlinkResult, error)
	config func(context.Context, service.Options) (service.ConfigResult, error)
	doctor func(context.Context, service.Options) (service.DoctorResult, error)
	pull   func(context.Context, service.EnvOptions) (service.EnvPullResult, error)
	diff   func(context.Context, service.EnvOptions) (service.EnvDiffResult, error)
	push   func(context.Context, service.EnvPushOptions, service.ConfirmPush) (service.EnvPushResult, error)
	dev    func(context.Context, service.DevOptions, service.Emitter) error
	domain func(context.Context, service.Options) (service.DomainResult, error)
	setDom func(context.Context, service.DomainSetOptions, service.ConfirmDomain) (service.DomainSetResult, error)
	login  func(context.Context, service.LoginOptions) (service.LoginResult, error)
	logout func(context.Context, service.LogoutOptions) (service.LogoutResult, error)
	stop   func(context.Context, service.StopOptions, service.ConfirmStop, service.Emitter) (service.StopResult, error)
	start  func(context.Context, service.StartOptions, service.Emitter) (service.DeployResult, error)
	rstart func(context.Context, service.StartOptions, service.ConfirmRestart, service.Emitter) (service.DeployResult, error)
	list   func(context.Context, service.DeploymentsOptions) (service.DeploymentsResult, error)
	cancel func(context.Context, service.CancelOptions, service.ConfirmCancel) (service.CancelResult, error)
}

func (f fakeApplication) Stop(ctx context.Context, options service.StopOptions, confirm service.ConfirmStop, emit service.Emitter) (service.StopResult, error) {
	return f.stop(ctx, options, confirm, emit)
}
func (f fakeApplication) Start(ctx context.Context, options service.StartOptions, emit service.Emitter) (service.DeployResult, error) {
	return f.start(ctx, options, emit)
}
func (f fakeApplication) Restart(ctx context.Context, options service.StartOptions, confirm service.ConfirmRestart, emit service.Emitter) (service.DeployResult, error) {
	return f.rstart(ctx, options, confirm, emit)
}
func (f fakeApplication) Deployments(ctx context.Context, options service.DeploymentsOptions) (service.DeploymentsResult, error) {
	return f.list(ctx, options)
}
func (f fakeApplication) Cancel(ctx context.Context, options service.CancelOptions, confirm service.ConfirmCancel) (service.CancelResult, error) {
	return f.cancel(ctx, options, confirm)
}

func (f fakeApplication) Login(ctx context.Context, options service.LoginOptions) (service.LoginResult, error) {
	return f.login(ctx, options)
}
func (f fakeApplication) Logout(ctx context.Context, options service.LogoutOptions) (service.LogoutResult, error) {
	return f.logout(ctx, options)
}

func (f fakeApplication) Domain(ctx context.Context, options service.Options) (service.DomainResult, error) {
	return f.domain(ctx, options)
}
func (f fakeApplication) DomainSet(ctx context.Context, options service.DomainSetOptions, confirm service.ConfirmDomain) (service.DomainSetResult, error) {
	return f.setDom(ctx, options, confirm)
}

func (f fakeApplication) Dev(ctx context.Context, options service.DevOptions, emit service.Emitter) error {
	return f.dev(ctx, options, emit)
}

func (f fakeApplication) EnvPull(ctx context.Context, options service.EnvOptions) (service.EnvPullResult, error) {
	return f.pull(ctx, options)
}
func (f fakeApplication) EnvDiff(ctx context.Context, options service.EnvOptions) (service.EnvDiffResult, error) {
	return f.diff(ctx, options)
}
func (f fakeApplication) EnvPush(ctx context.Context, options service.EnvPushOptions, confirm service.ConfirmPush) (service.EnvPushResult, error) {
	return f.push(ctx, options, confirm)
}

func (f fakeApplication) Open(ctx context.Context, options service.OpenOptions) (service.OpenResult, error) {
	return f.open(ctx, options)
}
func (f fakeApplication) Unlink(ctx context.Context, options service.UnlinkOptions, confirm service.ConfirmUnlink) (service.UnlinkResult, error) {
	return f.unlink(ctx, options, confirm)
}
func (f fakeApplication) Config(ctx context.Context, options service.Options) (service.ConfigResult, error) {
	return f.config(ctx, options)
}
func (f fakeApplication) Doctor(ctx context.Context, options service.Options) (service.DoctorResult, error) {
	return f.doctor(ctx, options)
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
func (f fakeApplication) Init(ctx context.Context, options service.InitOptions, selectChoice service.Selector, confirm service.ConfirmInit, emit service.Emitter) (service.InitResult, error) {
	return f.init(ctx, options, selectChoice, confirm, emit)
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
	for _, command := range []string{"init", "link", "status", "deploy", "logs", "open", "unlink", "config", "doctor", "env", "preview", "dev", "domain", "login", "logout", "completion"} {
	for _, command := range []string{"init", "link", "status", "deploy", "deployments", "cancel", "stop", "start", "restart", "logs", "open", "unlink", "config", "doctor", "env", "preview", "dev", "domain", "login", "logout"} {
		if !strings.Contains(out, "\n  "+command+" ") {
			t.Errorf("help omits %s", command)
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
		{"unknown"}, {"help", "unknown"}, {"help", "status", "extra"}, {"help", "env", "nope"}, {"status", "a", "b"},
		{"status", "--unknown"}, {"status", "--config"}, {"status", "--format", "yaml"},
		{"logs", "-n", "0"}, {"logs", "-n", "-1"}, {"logs", "-n", "many"}, {"logs", "-n", "10001"},
		{"deploy", "--timeout", "0s"}, {"deploy", "--timeout=-1s"}, {"deploy", "--timeout", "eventually"},
		{"dev", "npm", "run", "dev"}, {"preview", "a", "b", "--pr", "1"},
		{"completion", "nope"}, {"completion", "bash", "extra"},
		{"login", "--url", "coolify.example.com", "--name", "home", "--token-stdin"},
		{"login", "--token-stdin"},
		{"stop", "--timeout", "0s"}, {"start", "--timeout", "0s"}, {"restart", "--timeout=-1s"}, {"stop", "a", "b"},
		{"deployments", "-n", "0"}, {"deployments", "--limit", "-1"}, {"cancel", "a", "b", "c"}, {"cancel", "--target", "web", "api", "d-1"},
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

func TestOpenPrintsURLAndLaunchesOnlyInteractively(t *testing.T) {
	app := fakeApplication{open: func(_ context.Context, options service.OpenOptions) (service.OpenResult, error) {
		kind, url := "application", "https://app.example.com"
		if options.Dashboard {
			kind, url = "dashboard", "https://coolify.example.com/project/p/environment/e/application/a"
		}
		return service.OpenResult{Kind: kind, URL: url}, nil
	}}
	var launched []string
	opener := cmd.WithOpener(func(url string) error { launched = append(launched, url); return nil })

	// Noninteractive: print only.
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic}, "test", opener)
	root.SetArgs([]string{"open"})
	if err := root.ExecuteContext(context.Background()); err != nil || out.String() != "https://app.example.com\n" || len(launched) != 0 || diagnostic.String() != "" {
		t.Fatalf("noninteractive: out=%q err=%v launched=%v diagnostic=%q", out.String(), err, launched, diagnostic.String())
	}
	// Interactive: print and launch, reporting the launch on stderr.
	out.Reset()
	root = cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic, Interactive: true}, "test", opener)
	root.SetArgs([]string{"open", "--dashboard"})
	if err := root.ExecuteContext(context.Background()); err != nil || !strings.HasPrefix(out.String(), "https://coolify.example.com/") || len(launched) != 1 || !strings.Contains(diagnostic.String(), "Opening dashboard") {
		t.Fatalf("interactive: out=%q err=%v launched=%v diagnostic=%q", out.String(), err, launched, diagnostic.String())
	}
	// --print never launches, even interactively.
	out.Reset()
	root = cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic, Interactive: true}, "test", opener)
	root.SetArgs([]string{"open", "--print"})
	if err := root.ExecuteContext(context.Background()); err != nil || len(launched) != 1 {
		t.Fatalf("--print launched: err=%v launched=%v", err, launched)
	}
	// A launcher failure is reported, with the URL already printed.
	out.Reset()
	root = cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic, Interactive: true}, "test",
		cmd.WithOpener(func(string) error { return errors.New("no display") }))
	root.SetArgs([]string{"open"})
	if err := root.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "no display") || out.String() != "https://app.example.com\n" {
		t.Fatalf("launch failure: out=%q err=%v", out.String(), err)
	}
}

func TestDoctorExitCodeFollowsFailures(t *testing.T) {
	for _, failed := range []bool{false, true} {
		app := fakeApplication{doctor: func(context.Context, service.Options) (service.DoctorResult, error) {
			return service.DoctorResult{Checks: []service.Check{{Name: "Server", Status: "ok", Detail: "Coolify 4.3.18"}, {Name: "Binding", Status: map[bool]string{false: "warning", true: "failed"}[failed], Detail: "x"}}, Failed: failed}, nil
		}}
		out, _, err := execute(t, app, "doctor")
		if !strings.Contains(out, "[ok]   Server: Coolify 4.3.18") {
			t.Fatalf("output %q", out)
		}
		if failed && (!errors.Is(err, service.ErrChecksFailed) || ui.ExitCode(err) != 1) {
			t.Fatalf("failed checks: err=%v code=%d", err, ui.ExitCode(err))
		}
		if !failed && err != nil {
			t.Fatalf("warnings must not fail: %v", err)
		}
	}
}

func TestUnlinkAndConfigRender(t *testing.T) {
	app := fakeApplication{
		unlink: func(_ context.Context, options service.UnlinkOptions, confirm service.ConfirmUnlink) (service.UnlinkResult, error) {
			if !options.Yes {
				if _, err := confirm(context.Background(), service.UnlinkPlan{Path: "/p/coolship.toml"}); err != nil {
					return service.UnlinkResult{}, err
				}
			}
			return service.UnlinkResult{Path: "/p/coolship.toml"}, nil
		},
		config: func(context.Context, service.Options) (service.ConfigResult, error) {
			return service.ConfigResult{ConfigPath: "/p/coolship.toml", Target: "default", AppRoot: "/p", CredentialSource: "file", CredentialPath: "/home/u/.config/coolify/config.json", Instance: "home", InstanceURL: "https://coolify.example.com", Overrides: map[string]string{"environment": "staging"}}, nil
		},
	}
	// Noninteractive unlink without --yes is an input error and prints nothing on stdout.
	out, _, err := execute(t, app, "unlink")
	if !errors.Is(err, service.ErrInput) || out != "" {
		t.Fatalf("unlink without --yes: out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "unlink", "--yes")
	if err != nil || out != "Unlinked /p/coolship.toml\n" {
		t.Fatalf("unlink --yes: out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "config")
	if err != nil || !strings.Contains(out, "home at https://coolify.example.com") || !strings.Contains(out, "Override environment: staging") {
		t.Fatalf("config: out=%q err=%v", out, err)
	}
}

func TestEnvDiffMasksValuesUnlessRevealedAndHonorsExitCode(t *testing.T) {
	app := fakeApplication{diff: func(_ context.Context, options service.EnvOptions) (service.EnvDiffResult, error) {
		return service.EnvDiffResult{Scope: map[bool]string{false: "regular", true: "preview"}[options.Preview], File: options.File,
			Changed: []service.EnvChange{{Key: "TOKEN", Local: "local-secret", Remote: "remote-secret"}}, Unchanged: 2}, nil
	}}
	out, _, err := execute(t, app, "env", "diff")
	if err != nil || strings.Contains(out, "secret") || !strings.Contains(out, "~ TOKEN: local ********, remote ********") {
		t.Fatalf("masked diff: out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "env", "diff", "--show-values", "--preview", "--file", ".env.preview")
	if err != nil || !strings.Contains(out, "local local-secret, remote remote-secret") || !strings.Contains(out, ".env.preview with preview") {
		t.Fatalf("revealed diff: out=%q err=%v", out, err)
	}
	out, _, err = execute(t, app, "env", "diff", "--format", "json")
	if err != nil || strings.Contains(out, "secret") || !strings.Contains(out, `"key":"TOKEN"`) {
		t.Fatalf("json diff must mask by default: out=%q err=%v", out, err)
	}
	// --exit-code answers with the status alone, like git diff: the diff is
	// printed, and the boundary adds no diagnostic.
	out, _, err = execute(t, app, "env", "diff", "--exit-code")
	if !errors.Is(err, cmd.ErrDifferences) || ui.ExitCode(err) != 1 || !strings.Contains(out, "~ TOKEN") {
		t.Fatalf("--exit-code: out=%q err=%v code=%d", out, err, ui.ExitCode(err))
	}
	var diagnostic bytes.Buffer
	if err := ui.PrintError(ui.Streams{Err: &diagnostic}, err); err != nil || diagnostic.Len() != 0 {
		t.Fatalf("--exit-code printed a diagnostic: %q (%v)", diagnostic.String(), err)
	}
	app.diff = func(context.Context, service.EnvOptions) (service.EnvDiffResult, error) {
		return service.EnvDiffResult{Scope: "regular", File: ".env", Unchanged: 2}, nil
	}
	if out, _, err := execute(t, app, "env", "diff", "--exit-code"); err != nil || !strings.Contains(out, "No differences") {
		t.Fatalf("clean --exit-code: out=%q err=%v", out, err)
	}
}

func TestEnvPushConfirmsOnlyInteractively(t *testing.T) {
	app := fakeApplication{push: func(_ context.Context, options service.EnvPushOptions, confirm service.ConfirmPush) (service.EnvPushResult, error) {
		plan := service.EnvPushPlan{Scope: "regular", File: ".env", Create: []service.EnvChange{{Key: "NEW", Local: "secret"}}}
		if !options.Yes {
			if _, err := confirm(context.Background(), plan); err != nil {
				return service.EnvPushResult{}, err
			}
		}
		return service.EnvPushResult{Plan: plan}, nil
	}}
	if _, _, err := execute(t, app, "env", "push"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("noninteractive push without --yes: %v", err)
	}
	out, _, err := execute(t, app, "env", "push", "--yes")
	if err != nil || !strings.Contains(out, "1 created, 0 updated, 0 deleted") {
		t.Fatalf("push --yes: out=%q err=%v", out, err)
	}
	// Interactive confirmation shows keys, never values.
	var out2, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("y\n"), Out: &out2, Err: &diagnostic, Interactive: true}, "test")
	root.SetArgs([]string{"env", "push"})
	if err := root.ExecuteContext(context.Background()); err != nil || strings.Contains(diagnostic.String(), "secret") || !strings.Contains(diagnostic.String(), "create NEW") {
		t.Fatalf("interactive push: err=%v stderr=%q", err, diagnostic.String())
	}
}

func TestPreviewTakesPullRequestFromFlagOrGitHubActions(t *testing.T) {
	var seen []int
	app := fakeApplication{deploy: func(_ context.Context, options service.DeployOptions, _ service.Emitter) (service.DeployResult, error) {
		seen = append(seen, options.PullRequest)
		return service.DeployResult{DeploymentUUID: "d1", PullRequest: options.PullRequest, Status: "finished"}, nil
	}}
	out, _, err := execute(t, app, "preview", "--pr", "12")
	if err != nil || !strings.Contains(out, "Pull request: 12") {
		t.Fatalf("--pr: out=%q err=%v", out, err)
	}
	if _, _, err := execute(t, app, "preview"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("no pull request and no environment: %v", err)
	}
	for ref, want := range map[string]int{"refs/pull/34/merge": 34, "refs/pull/56/head": 56} {
		var out bytes.Buffer
		root := cmd.NewRootCommand(app, ui.Streams{Out: &out}, "test", cmd.WithEnvironment(func(name string) string {
			if name == "GITHUB_REF" {
				return ref
			}
			return ""
		}))
		root.SetArgs([]string{"preview"})
		if err := root.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), fmt.Sprintf("Pull request: %d", want)) {
			t.Fatalf("%s: out=%q err=%v", ref, out.String(), err)
		}
	}
	if !reflect.DeepEqual(seen, []int{12, 34, 56}) && !reflect.DeepEqual(seen, []int{12, 56, 34}) {
		t.Fatalf("pull requests seen %v", seen)
	}
	if _, _, err := execute(t, app, "preview", "--pr", "0"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("--pr 0: %v", err)
	}
}

func TestPositionalTargetSelectsAndCannotConflict(t *testing.T) {
	var targets []string
	app := fakeApplication{status: func(_ context.Context, options service.Options) (service.StatusResult, error) {
		targets = append(targets, options.Target)
		return service.StatusResult{Target: service.TargetInfo{Target: options.Target, Application: "x", ApplicationUUID: "u"}, Status: "running"}, nil
	}}
	if _, _, err := execute(t, app, "status", "api"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "status", "--target", "web"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "status", "-t", "web", "web"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "status", "-t", "web", "api"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("conflicting targets: %v", err)
	}
	if _, _, err := execute(t, app, "status", "a", "b"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("two positional targets: %v", err)
	}
	if !reflect.DeepEqual(targets, []string{"api", "web", "web"}) {
		t.Fatalf("targets %v", targets)
	}
	out, _, _ := execute(t, app, "status", "api")
	if !strings.HasPrefix(out, "Target: api\n") {
		t.Fatalf("named target not shown: %q", out)
	}
}

func TestDevSplitsTargetFromCommandAndPropagatesExitStatus(t *testing.T) {
	var seen []service.DevOptions
	app := fakeApplication{dev: func(_ context.Context, options service.DevOptions, _ service.Emitter) error {
		seen = append(seen, options)
		if len(options.Command) > 0 && options.Command[0] == "false" {
			return &service.ExitError{Code: 7}
		}
		return nil
	}}
	if _, _, err := execute(t, app, "dev", "--", "npm", "run", "dev", "--port", "3000"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "dev", "api", "--preview", "--", "make"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "dev"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen[0].Command, []string{"npm", "run", "dev", "--port", "3000"}) || seen[0].Target != "" ||
		!reflect.DeepEqual(seen[1].Command, []string{"make"}) || seen[1].Target != "api" || !seen[1].Preview ||
		len(seen[2].Command) != 0 {
		t.Fatalf("options %+v", seen)
	}
	out, diagnostic, err := execute(t, app, "dev", "--", "false")
	var exit *service.ExitError
	if !errors.As(err, &exit) || ui.ExitCode(err) != 7 || out != "" || diagnostic != "" {
		t.Fatalf("exit status: err=%v code=%d out=%q diag=%q", err, ui.ExitCode(err), out, diagnostic)
	}
	var buffer bytes.Buffer
	if err := ui.PrintError(ui.Streams{Err: &buffer}, err); err != nil || buffer.Len() != 0 {
		t.Fatalf("child exit printed a diagnostic: %q", buffer.String())
	}
	if _, _, err := execute(t, app, "dev", "a", "b", "--", "x"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("two targets: %v", err)
	}
}

func TestDomainCommandsRenderAndPassArguments(t *testing.T) {
	app := fakeApplication{
		domain: func(_ context.Context, options service.Options) (service.DomainResult, error) {
			return service.DomainResult{Target: service.TargetInfo{Application: "web"}, Domains: []string{"https://u.coolify.example.com"}, Generated: true}, nil
		},
		setDom: func(_ context.Context, options service.DomainSetOptions, confirm service.ConfirmDomain) (service.DomainSetResult, error) {
			if !options.Yes {
				if _, err := confirm(context.Background(), service.DomainPlan{}); err != nil {
					return service.DomainSetResult{}, err
				}
			}
			return service.DomainSetResult{Plan: service.DomainPlan{Target: service.TargetInfo{Application: "web"}, Domains: options.Domains, Redirect: options.Redirect}}, nil
		},
	}
	out, _, err := execute(t, app, "domain")
	if err != nil || out != "https://u.coolify.example.com  (generated by Coolify)\n" {
		t.Fatalf("show: out=%q err=%v", out, err)
	}
	if _, _, err := execute(t, app, "domain", "set", "app.example.com"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("noninteractive set without --yes: %v", err)
	}
	out, _, err = execute(t, app, "domain", "set", "app.example.com", "www.example.com", "--redirect", "non-www", "--yes")
	if err != nil || !strings.Contains(out, "app.example.com, www.example.com") {
		t.Fatalf("set: out=%q err=%v", out, err)
	}
	if _, _, err := execute(t, app, "domain", "set", "--yes"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("set without domains: %v", err)
	}
}

func TestLoginNeverTakesTheTokenAsAFlagAndReadsStdin(t *testing.T) {
	var seen service.LoginOptions
	app := fakeApplication{login: func(_ context.Context, options service.LoginOptions) (service.LoginResult, error) {
		seen = options
		return service.LoginResult{Name: options.Name, URL: options.URL, Team: "Platform", Server: "4.3.18", Path: "/c.json", Default: true}, nil
	}}
	if _, _, err := execute(t, app, "login", "--url", "https://coolify.example.com", "--name", "home", "--token", "leak"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("--token must not exist: %v", err)
	}
	if _, _, err := execute(t, app, "login", "--url", "https://coolify.example.com", "--name", "home"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("noninteractive without --token-stdin: %v", err)
	}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("piped-token\n"), Out: &out, Err: &diagnostic}, "test")
	root.SetArgs([]string{"login", "--url", "https://coolify.example.com", "--name", "home", "--token-stdin", "--default", "--coolify-config", "/c.json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.Token != "piped-token" || !seen.Default || seen.ConfigPath != "/c.json" || strings.Contains(out.String()+diagnostic.String(), "piped-token") || !strings.Contains(out.String(), "as team Platform on Coolify 4.3.18, now the default") {
		t.Fatalf("seen=%+v out=%q diag=%q", seen, out.String(), diagnostic.String())
	}
	// Interactive: URL and name are asked, the name defaults from the host, the token is read from input.
	out.Reset()
	diagnostic.Reset()
	root = cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("https://coolify.example.com\n\ntyped-token\n"), Out: &out, Err: &diagnostic, Interactive: true}, "test")
	root.SetArgs([]string{"login"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.URL != "https://coolify.example.com" || seen.Name != "coolify" || seen.Token != "typed-token" || !strings.Contains(diagnostic.String(), "Context name [coolify]:") {
		t.Fatalf("interactive: seen=%+v diag=%q", seen, diagnostic.String())
	}
}

func TestColorCapabilityStylesHumanOutputOnly(t *testing.T) {
	app := fakeApplication{doctor: func(context.Context, service.Options) (service.DoctorResult, error) {
		return service.DoctorResult{Checks: []service.Check{{Name: "Server", Status: "ok", Detail: "Coolify 4.3.18"}}}, nil
	}}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic, ColorOut: true, ColorErr: true}, "test")
	root.SetArgs([]string{"doctor"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if out.String() != "\x1b[32m[ok]\x1b[0m   Server: Coolify 4.3.18\n" || diagnostic.Len() != 0 {
		t.Fatalf("styled doctor: out=%q diag=%q", out.String(), diagnostic.String())
	}
	out.Reset()
	root.SetArgs([]string{"doctor", "--format", "json", "--no-color"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.HasPrefix(out.String(), `{"checks":`) {
		t.Fatalf("json output must never carry escapes: %q", out.String())
	}
}

func TestHelpReachesNestedCommandsAndCompletionIsOffline(t *testing.T) {
	for _, args := range [][]string{{"help", "env", "pull"}, {"help", "domain", "set"}, {"completion"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, diagnostic, err := execute(t, nil, args...)
			if err != nil || out == "" || diagnostic != "" {
				t.Fatalf("out=%q stderr=%q err=%v", out, diagnostic, err)
			}
		})
	}
	out, _, _ := execute(t, nil, "help", "env", "pull")
	if !strings.Contains(out, "coolship env pull") {
		t.Fatalf("help env pull = %q", out)
	}
	// An unknown shell is invalid input that names the shells, not a help page.
	if _, _, err := execute(t, nil, "completion", "nope"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), `unknown shell "nope"; use one of bash, fish, powershell, zsh`) {
		t.Fatalf("completion nope: %v", err)
	}
}

func TestLogsLinesAreBoundedAtTheCommand(t *testing.T) {
	var seen []int
	app := fakeApplication{logs: func(_ context.Context, options service.LogsOptions, _ service.Emitter) error {
		seen = append(seen, options.Lines)
		return nil
	}}
	for _, lines := range []string{"1", "10000"} {
		if _, _, err := execute(t, app, "logs", "-n", lines); err != nil {
			t.Fatal(err)
		}
	}
	// Out of range never reaches the workflow, so no request is made first.
	_, _, err := execute(t, app, "logs", "--lines", "10001")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "between 1 and 10000") {
		t.Fatalf("10001 lines: %v", err)
	}
	if !reflect.DeepEqual(seen, []int{1, 10000}) {
		t.Fatalf("lines seen %v", seen)
	}
}

func TestDevWithoutDashSaysWhereTheCommandGoes(t *testing.T) {
	_, _, err := execute(t, nil, "dev", "npm", "run", "dev")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "coolship dev -- npm run dev") {
		t.Fatalf("err=%v", err)
	}
}

func TestPreviewTakesThePositionalTargetLikeDeploy(t *testing.T) {
	var seen []string
	app := fakeApplication{deploy: func(_ context.Context, options service.DeployOptions, _ service.Emitter) (service.DeployResult, error) {
		seen = append(seen, options.Target)
		return service.DeployResult{DeploymentUUID: "d1", PullRequest: options.PullRequest, Status: "finished"}, nil
	}}
	if _, _, err := execute(t, app, "preview", "api", "--pr", "3"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "preview", "--target", "web", "--pr", "3"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, app, "preview", "api", "--target", "web", "--pr", "3"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("conflicting targets: %v", err)
	}
	if !reflect.DeepEqual(seen, []string{"api", "web"}) {
		t.Fatalf("targets %v", seen)
	}
}

func TestFailedDeploymentStillPrintsItsJSONResult(t *testing.T) {
	failure := &service.DeploymentError{DeploymentUUID: "d1", Err: errors.New("ended with status failed")}
	app := fakeApplication{deploy: func(_ context.Context, options service.DeployOptions, _ service.Emitter) (service.DeployResult, error) {
		return service.DeployResult{DeploymentUUID: "d1", Status: "failed", PullRequest: options.PullRequest}, failure
	}}
	for _, args := range [][]string{{"deploy"}, {"preview", "--pr", "4"}} {
		t.Run(args[0], func(t *testing.T) {
			// JSON: the result a caller needs to follow up, then the failure's exit status.
			out, diagnostic, err := execute(t, app, append(args, "--format", "json")...)
			if !errors.Is(err, failure) || ui.ExitCode(err) != 1 || diagnostic != "" {
				t.Fatalf("stderr=%q err=%v code=%d", diagnostic, err, ui.ExitCode(err))
			}
			var result service.DeployResult
			if err := json.Unmarshal([]byte(out), &result); err != nil || result.DeploymentUUID != "d1" || result.Status != "failed" {
				t.Fatalf("result=%q err=%v", out, err)
			}
			if args[0] == "preview" && result.PullRequest != 4 {
				t.Fatalf("pull request lost: %q", out)
			}
			// Human output keeps the diagnostic alone.
			out, _, err = execute(t, app, args...)
			if !errors.Is(err, failure) || out != "" {
				t.Fatalf("human: out=%q err=%v", out, err)
			}
		})
	}
	// Without a deployment UUID there is nothing to follow up on.
	refused := errors.New("server did not confirm a deployment")
	app.deploy = func(context.Context, service.DeployOptions, service.Emitter) (service.DeployResult, error) {
		return service.DeployResult{}, refused
	}
	if out, _, err := execute(t, app, "deploy", "--format", "json"); !errors.Is(err, refused) || out != "" {
		t.Fatalf("no uuid: out=%q err=%v", out, err)
	}
}

func TestLoginChecksTheURLBeforeTheTokenAndNamesTheFlags(t *testing.T) {
	var seen service.LoginOptions
	app := fakeApplication{login: func(_ context.Context, options service.LoginOptions) (service.LoginResult, error) {
		seen = options
		return service.LoginResult{Name: options.Name, URL: options.URL, Path: "/c.json"}, nil
	}}
	// Noninteractive: a bare host is refused by flag name before stdin is read.
	_, _, err := execute(t, app, "login", "--url", "coolify.example.com", "--name", "home", "--token-stdin")
	if !errors.Is(err, service.ErrInput) || !strings.HasPrefix(err.Error(), "--url:") {
		t.Fatalf("bare host: %v", err)
	}
	// Without --url nothing can be asked, so the flags are named.
	_, _, err = execute(t, app, "login", "--token-stdin")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--url") || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("no url: %v", err)
	}
	run := func(in string, interactive bool, args ...string) (string, error) {
		var out, diagnostic bytes.Buffer
		root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader(in), Out: &out, Err: &diagnostic, Interactive: interactive}, "test")
		root.SetArgs(args)
		err := root.ExecuteContext(context.Background())
		return diagnostic.String(), err
	}
	if _, err := run("tok\n", false, "login", "--url", "https://coolify.example.com", "--token-stdin"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--name") || !strings.Contains(err.Error(), "--context") {
		t.Fatalf("no name: %v", err)
	}
	// The global --context names the instance being saved when --name is absent.
	if _, err := run("tok\n", false, "login", "--url", "https://coolify.example.com/", "--context", "lab", "--token-stdin"); err != nil || seen.Name != "lab" || seen.URL != "https://coolify.example.com" || seen.Token != "tok" {
		t.Fatalf("--context as name: seen=%+v err=%v", seen, err)
	}
	if _, err := run("tok\n", false, "login", "--url", "https://coolify.example.com", "--context", "lab", "--name", "home", "--token-stdin"); err != nil || seen.Name != "home" {
		t.Fatalf("--name wins: seen=%+v err=%v", seen, err)
	}
	// Interactive: a bare host is asked again, and the token prompt is preceded by where to get one.
	diagnostic, err := run("coolify.example.com\nhttps://Coolify.example.com/\n\ntyped\n", true, "login")
	if err != nil || seen.URL != "https://coolify.example.com" || seen.Name != "coolify" || seen.Token != "typed" {
		t.Fatalf("interactive: seen=%+v err=%v stderr=%q", seen, err, diagnostic)
	}
	if strings.Count(diagnostic, "Coolify URL:") != 2 || !strings.Contains(diagnostic, "full URL") || !strings.Contains(diagnostic, "Keys & Tokens") || strings.Contains(diagnostic, "typed") {
		t.Fatalf("interactive stderr=%q", diagnostic)
	}
	if strings.Index(diagnostic, "Keys & Tokens") < strings.Index(diagnostic, "Context name") {
		t.Fatalf("token hint must come with the token prompt: %q", diagnostic)
	}
}

func TestUnlinkPromptListsEveryTargetItRemoves(t *testing.T) {
	app := fakeApplication{unlink: func(ctx context.Context, _ service.UnlinkOptions, confirm service.ConfirmUnlink) (service.UnlinkResult, error) {
		plan := service.UnlinkPlan{Path: "/p/coolship.toml", Targets: []service.UnlinkTarget{
			{Name: "api", Binding: config.Binding{Project: "Personal", Environment: "production", Application: "backend"}},
			{Name: "web", Binding: config.Binding{Project: "Personal", Environment: "production", Application: "frontend"}},
		}}
		accepted, err := confirm(ctx, plan)
		if err != nil || !accepted {
			t.Fatalf("accepted=%v err=%v", accepted, err)
		}
		return service.UnlinkResult{Path: plan.Path}, nil
	}}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("y\n"), Out: &out, Err: &diagnostic, Interactive: true}, "test")
	root.SetArgs([]string{"unlink"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	text := diagnostic.String()
	for _, want := range []string{"Delete /p/coolship.toml?", "Every target in it is removed (2):", "  api: Personal / production / backend\n", "  web: Personal / production / frontend\n", "The remote application is not affected."} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt lacks %q: %q", want, text)
		}
	}
	if strings.Contains(text, "Current binding") {
		t.Errorf("monorepo prompt shows the empty [project] binding: %q", text)
	}
}

func TestEnvPushPromptShowsWhatItLeavesAlone(t *testing.T) {
	app := fakeApplication{push: func(ctx context.Context, _ service.EnvPushOptions, confirm service.ConfirmPush) (service.EnvPushResult, error) {
		plan := service.EnvPushPlan{Scope: "regular", File: ".env",
			Create: []service.EnvChange{{Key: "NEW", Local: "secret"}}, Update: []service.EnvChange{{Key: "CHANGED", Local: "secret"}}, Delete: []service.EnvChange{{Key: "OLD"}},
			Skipped: []string{"SECRET"}, Untouched: []string{"REMOTE_ONLY"}}
		if _, err := confirm(ctx, plan); err != nil {
			return service.EnvPushResult{}, err
		}
		return service.EnvPushResult{Plan: plan}, nil
	}}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("y\n"), Out: &out, Err: &diagnostic, Interactive: true}, "test")
	root.SetArgs([]string{"env", "push"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	text := diagnostic.String()
	last := -1
	for _, want := range []string{"  create NEW\n", "  update CHANGED\n", "  delete OLD\n", "  skip   SECRET (remote value withheld; --force overwrites it)\n", "  keep   REMOTE_ONLY (remote only; --prune deletes it)\n", "Confirm [y/N]:"} {
		index := strings.Index(text, want)
		if index < 0 || index < last {
			t.Fatalf("prompt lacks %q in order: %q", want, text)
		}
		last = index
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("prompt shows a value: %q", text)
	}
}
