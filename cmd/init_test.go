package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

func TestInitFlagsAndDefaults(t *testing.T) {
	var seen service.InitOptions
	app := fakeApplication{init: func(_ context.Context, options service.InitOptions, _ service.Selector, _ service.ConfirmInit, _ service.Emitter) (service.InitResult, error) {
		seen = options
		return service.InitResult{Plan: service.InitPlan{Path: "/p/coolship.toml", Name: options.Name, Repository: "https://github.com/o/r", Branch: "main", BuildPack: "static", Port: 8080},
			Target: service.TargetInfo{Application: options.Name, ApplicationUUID: "a-9", Environment: "production", Project: "Personal", Instance: "home"}, URL: "https://a-9.example.com"}, nil
	}}
	out, diagnostic, err := execute(t, app, "init", "--repo", "git@github.com:o/r.git", "--branch", "main", "--build-pack", "static", "--port", "8080", "--static",
		"--name", "site", "--project", "Personal", "--create-project", "--server", "Master", "--yes", "--deploy", "--timeout", "3m", "-t", "web", "-e", "staging", "--format", "json")
	if err != nil || diagnostic != "" {
		t.Fatalf("stderr=%q err=%v", diagnostic, err)
	}
	want := service.InitOptions{Options: service.Options{Target: "web", Environment: "staging"}, Repository: "git@github.com:o/r.git", Branch: "main", BuildPack: "static", Port: 8080, Static: true,
		Name: "site", Project: "Personal", CreateProject: true, Server: "Master", Yes: true, Deploy: true, Timeout: 3 * time.Minute}
	if seen != want {
		t.Fatalf("options = %#v\nwant %#v", seen, want)
	}
	var result service.InitResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Target.ApplicationUUID != "a-9" || result.Plan.Name != "site" {
		t.Fatalf("result=%q err=%v", out, err)
	}
	// Defaults leave detection to the service; the timeout matches deploy.
	if _, _, err := execute(t, app, "init", "--yes"); err != nil {
		t.Fatal(err)
	}
	if seen.Repository != "" || seen.Branch != "" || seen.BuildPack != "" || seen.Port != 0 || seen.Static || seen.Name != "" || seen.Timeout != 10*time.Minute || seen.Deploy {
		t.Fatalf("defaults = %#v", seen)
	}
	for _, args := range [][]string{{"init", "--timeout", "0s"}, {"init", "--port", "eighty"}, {"init", "--target", "bad name"}, {"init", "extra"}} {
		if _, _, err := execute(t, app, args...); !errors.Is(err, service.ErrInput) {
			t.Errorf("%v: %v", args, err)
		}
	}
	// Human output names what was created and where it was linked.
	out, _, err = execute(t, app, "init", "--yes", "--name", "site")
	if err != nil || !strings.Contains(out, "Created application site (a-9) from https://github.com/o/r at main") || !strings.Contains(out, "port 8080") ||
		!strings.Contains(out, "URL: https://a-9.example.com") || !strings.Contains(out, "Linked project in /p/coolship.toml") || !strings.Contains(out, "Application: site (a-9)") {
		t.Fatalf("human output %q err=%v", out, err)
	}
}

func TestInitConfirmsOnlyInteractively(t *testing.T) {
	app := fakeApplication{init: func(ctx context.Context, options service.InitOptions, _ service.Selector, confirm service.ConfirmInit, emit service.Emitter) (service.InitResult, error) {
		plan := service.InitPlan{Path: "/p/coolship.toml", Target: "web", Repository: "https://github.com/o/r", Branch: "main", BuildPack: "dockerfile", Port: 80,
			Name: "r", Instance: "home", Project: "Fresh", NewProject: true, Environment: "production", Server: "Master", Deploy: options.Deploy}
		if !options.Yes {
			accepted, err := confirm(ctx, plan)
			if err != nil {
				return service.InitResult{}, err
			}
			if !accepted {
				return service.InitResult{}, service.ErrCancelled
			}
		}
		result := service.InitResult{Plan: plan, Target: service.TargetInfo{Target: "web", Application: "r", ApplicationUUID: "a-1"}}
		if options.Deploy {
			if err := emit(service.Event{Type: "deployment", DeploymentUUID: "d-1", Status: "queued"}); err != nil {
				return result, err
			}
			result.Deployment = &service.DeployResult{DeploymentUUID: "d-1", Status: "finished", Target: result.Target}
		}
		return result, nil
	}}
	// Noninteractive without --yes is an input error; nothing reaches stdout.
	out, _, err := execute(t, app, "init")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--yes") || out != "" {
		t.Fatalf("noninteractive: out=%q err=%v", out, err)
	}
	// Interactive: the plan goes to stderr, the answer decides.
	var stdout, stderr bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("n\n"), Out: &stdout, Err: &stderr, Interactive: true}, "test")
	root.SetArgs([]string{"init"})
	if err := root.ExecuteContext(context.Background()); !errors.Is(err, service.ErrCancelled) || stdout.Len() != 0 {
		t.Fatalf("declined: out=%q err=%v", stdout.String(), err)
	}
	for _, want := range []string{"Create application r on home?", "https://github.com/o/r (branch main)", "dockerfile, port 80", "Fresh (new)", "Master", "/p/coolship.toml [apps.web]", "Confirm [y/N]:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("plan omits %q: %q", want, stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	root = cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader("y\n"), Out: &stdout, Err: &stderr, Interactive: true}, "test")
	root.SetArgs([]string{"init", "--deploy"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "Then:") || !strings.Contains(stderr.String(), "Deployment d-1: queued") ||
		!strings.Contains(stdout.String(), "Target: web") || !strings.Contains(stdout.String(), "Deployment: d-1") || !strings.Contains(stdout.String(), "Status: finished") {
		t.Fatalf("accepted with deploy: out=%q err=%q", stdout.String(), stderr.String())
	}
	// JSON mode keeps progress on stderr and the deployment inside the result.
	stdout.Reset()
	root = cmd.NewRootCommand(app, ui.Streams{Out: &stdout, Err: &stderr}, "test")
	root.SetArgs([]string{"init", "--yes", "--deploy", "--format", "json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var result service.InitResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Deployment == nil || result.Deployment.DeploymentUUID != "d-1" {
		t.Fatalf("json=%q err=%v", stdout.String(), err)
	}
}
