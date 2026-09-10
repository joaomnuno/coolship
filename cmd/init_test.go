package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
	out, diagnostic, err := execute(t, app, "init", "--repo", "git@github.com:o/r.git", "--branch", "main", "--build-pack", "static", "--port", "8080", "--publish-dir", "public",
		"--name", "site", "--project", "Personal", "--create-project", "--server", "Master", "--source", "github-app", "--github-app", "docs",
		"--yes", "--deploy", "--timeout", "3m", "-t", "web", "-e", "staging", "--format", "json")
	if err != nil || diagnostic != "" {
		t.Fatalf("stderr=%q err=%v", diagnostic, err)
	}
	want := service.InitOptions{Options: service.Options{Target: "web", Environment: "staging"}, BuildOptions: service.BuildOptions{BuildPack: "static", Port: 8080, PublishDirectory: "public"},
		Repository: "git@github.com:o/r.git", Branch: "main", Name: "site", Project: "Personal", CreateProject: true, Server: "Master", Source: "github-app", GitHubApp: "docs", Yes: true, Deploy: true, Timeout: 3 * time.Minute}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("options = %#v\nwant %#v", seen, want)
	}
	// Every build refinement reaches the service as given; compose domains are parsed.
	if _, _, err := execute(t, app, "init", "--yes", "--static", "--install-command", "npm ci", "--build-command", "npm run build", "--start-command", "node .",
		"--dockerfile", "deploy/Dockerfile", "--compose-file", "stack.yml", "--compose-domain", "web=https://web.example.com", "--compose-domain", " api = https://api.example.com "); err != nil {
		t.Fatal(err)
	}
	wantBuild := service.BuildOptions{Static: true, InstallCommand: "npm ci", BuildCommand: "npm run build", StartCommand: "node .", Dockerfile: "deploy/Dockerfile", ComposeFile: "stack.yml",
		ComposeDomains: []service.ComposeDomain{{Service: "web", Domain: "https://web.example.com"}, {Service: "api", Domain: "https://api.example.com"}}}
	if !reflect.DeepEqual(seen.BuildOptions, wantBuild) {
		t.Fatalf("build options = %#v\nwant %#v", seen.BuildOptions, wantBuild)
	}
	var result service.InitResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Target.ApplicationUUID != "a-9" || result.Plan.Name != "site" {
		t.Fatalf("result=%q err=%v", out, err)
	}
	// The key flags reach the service verbatim; the service settles what they imply.
	if _, _, err := execute(t, app, "init", "--deploy-key", "ci", "--create-deploy-key", "new", "--yes"); err != nil || seen.DeployKey != "ci" || seen.CreateDeployKey != "new" || seen.Source != "auto" {
		t.Fatalf("key flags = %#v err=%v", seen, err)
	}
	// Defaults leave detection to the service; the timeout matches deploy.
	if _, _, err := execute(t, app, "init", "--yes"); err != nil {
		t.Fatal(err)
	}
	if seen.Repository != "" || seen.Branch != "" || !reflect.DeepEqual(seen.BuildOptions, service.BuildOptions{}) || seen.Name != "" || seen.Timeout != 10*time.Minute || seen.Deploy ||
		seen.Source != "auto" || seen.GitHubApp != "" || seen.DeployKey != "" || seen.CreateDeployKey != "" {
		t.Fatalf("defaults = %#v", seen)
	}
	for _, args := range [][]string{{"init", "--timeout", "0s"}, {"init", "--port", "eighty"}, {"init", "--compose-domain", "web"}, {"init", "--compose-domain", "=https://x"}, {"init", "--target", "bad name"}, {"init", "extra"}} {
		if _, _, err := execute(t, app, args...); !errors.Is(err, service.ErrInput) {
			t.Errorf("%v: %v", args, err)
		}
	}
	// Human output names what was created and where it was linked.
	out, _, err = execute(t, app, "init", "--yes", "--name", "site")
	if err != nil || !strings.Contains(out, "Created application site (a-9) from https://github.com/o/r at main") || !strings.Contains(out, "port 8080") ||
		!strings.Contains(out, "URL: https://a-9.example.com") || !strings.Contains(out, "Linked project in /p/coolship.toml") || !strings.Contains(out, "Application: site (a-9)") || strings.Contains(out, "Source:") {
		t.Fatalf("human output %q err=%v", out, err)
	}
}

func TestInitRendersSourcesAndCreatedKeys(t *testing.T) {
	app := fakeApplication{init: func(_ context.Context, options service.InitOptions, _ service.Selector, _ service.ConfirmInit, _ service.Emitter) (service.InitResult, error) {
		plan := service.InitPlan{Path: "/p/coolship.toml", Name: "r", Instance: "home", Repository: "git@github.com:o/r.git", Branch: "main", BuildPack: "dockerfile", Port: 80,
			Source: service.SourceDeployKey, DeployKey: options.DeployKey}
		if options.CreateDeployKey != "" {
			plan.DeployKey, plan.NewDeployKey = options.CreateDeployKey, true
			return service.InitResult{Plan: plan, DeployKey: &service.DeployKeyResult{Name: options.CreateDeployKey, UUID: "k-1", PublicKey: "ssh-ed25519 AAAA " + options.CreateDeployKey, Repository: "git@github.com:o/r.git"}}, nil
		}
		if options.GitHubApp != "" {
			plan.Source, plan.GitHubApp, plan.DeployKey, plan.Repository = service.SourceGitHubApp, options.GitHubApp, "", "https://github.com/o/r"
		}
		return service.InitResult{Plan: plan, Target: service.TargetInfo{Application: "r", ApplicationUUID: "a-1", Environment: "production", Project: "Personal", Instance: "home"}, URL: "https://a-1.example.com"}, nil
	}}
	// A created key is printed once, with what to do next, and nothing else.
	out, _, err := execute(t, app, "init", "--create-deploy-key", "ci")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Created deploy key ci (k-1) on home", "Public key:", "ssh-ed25519 AAAA ci", "git@github.com:o/r.git as a read-only deploy key", "coolship init --source deploy-key --deploy-key ci"} {
		if !strings.Contains(out, want) {
			t.Errorf("key output lacks %q: %q", want, out)
		}
	}
	if strings.Contains(out, "Created application") || strings.Contains(out, "Linked project") {
		t.Fatalf("key output announces an application: %q", out)
	}
	out, _, err = execute(t, app, "init", "--create-deploy-key", "ci", "--format", "json")
	var result service.InitResult
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.DeployKey == nil || result.DeployKey.PublicKey != "ssh-ed25519 AAAA ci" || result.Target.ApplicationUUID != "" {
		t.Fatalf("json key output %q err=%v", out, err)
	}
	// Applications say which private source they clone through.
	if out, _, err = execute(t, app, "init", "--deploy-key", "ci", "--yes"); err != nil || !strings.Contains(out, "from git@github.com:o/r.git at main") || !strings.Contains(out, "Source: deploy key ci") {
		t.Fatalf("deploy key output %q err=%v", out, err)
	}
	if out, _, err = execute(t, app, "init", "--github-app", "docs", "--yes"); err != nil || !strings.Contains(out, "from https://github.com/o/r at main") || !strings.Contains(out, "Source: GitHub App docs") {
		t.Fatalf("github app output %q err=%v", out, err)
	}
}

func TestInitPlanShowsTheSource(t *testing.T) {
	var seen service.InitPlan
	confirmWith := func(plan service.InitPlan) fakeApplication {
		return fakeApplication{init: func(ctx context.Context, _ service.InitOptions, _ service.Selector, confirm service.ConfirmInit, _ service.Emitter) (service.InitResult, error) {
			seen = plan
			accepted, err := confirm(ctx, plan)
			if err != nil {
				return service.InitResult{}, err
			}
			if !accepted {
				return service.InitResult{}, service.ErrCancelled
			}
			return service.InitResult{Plan: plan}, nil
		}}
	}
	base := service.InitPlan{Path: "/p/coolship.toml", Repository: "https://github.com/o/r", Branch: "main", BuildPack: "dockerfile", Port: 80, Name: "r", Instance: "home", Project: "Personal", Environment: "production", Server: "Master", Deploy: true}
	for _, test := range []struct {
		name     string
		plan     service.InitPlan
		question string
		rows     []string
		absent   []string
	}{
		{"public", base, "Create application r on home?", []string{"Source:      public (cloned without credentials)", "Then:"}, nil},
		{"github app", func() service.InitPlan { p := base; p.Source, p.GitHubApp = service.SourceGitHubApp, "docs"; return p }(),
			"Create application r on home?", []string{"Source:      GitHub App docs"}, nil},
		{"deploy key", func() service.InitPlan {
			p := base
			p.Source, p.DeployKey, p.Repository = service.SourceDeployKey, "ci", "git@github.com:o/r.git"
			return p
		}(), "Create application r on home?", []string{"Source:      deploy key ci", "git@github.com:o/r.git (branch main)"}, nil},
		{"new deploy key", func() service.InitPlan {
			p := base
			p.Source, p.DeployKey, p.NewDeployKey, p.Repository = service.SourceDeployKey, "ci", true, "git@github.com:o/r.git"
			return p
		}(), "Create deploy key ci on home for git@github.com:o/r.git?", []string{"deploy key ci (new; the application is created once the key is registered on the repository)"}, []string{"Then:", "Build pack:", "Server:"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := cmd.NewRootCommand(confirmWith(test.plan), ui.Streams{In: strings.NewReader("n\n"), Out: &stdout, Err: &stderr, Interactive: true}, "test")
			root.SetArgs([]string{"init"})
			if err := root.ExecuteContext(context.Background()); !errors.Is(err, service.ErrCancelled) || stdout.Len() != 0 || seen.Name != "r" {
				t.Fatalf("declined: out=%q err=%v", stdout.String(), err)
			}
			for _, want := range append(test.rows, test.question, "Confirm [y/N]:") {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("plan omits %q:\n%s", want, stderr.String())
				}
			}
			for _, absent := range test.absent {
				if strings.Contains(stderr.String(), absent) {
					t.Errorf("plan shows %q:\n%s", absent, stderr.String())
				}
			}
		})
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
