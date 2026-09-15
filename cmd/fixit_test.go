package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/problem"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// fixRun is one invocation through cmd.Execute, as the executable makes it,
// with an answering stdin and credentials from a Coolify CLI file.
type fixRun struct {
	out, err string
	result   error
	opened   []string
}

func executeWithFix(t *testing.T, configPath, dir, in string, args ...string) fixRun {
	t.Helper()
	var out, diagnostic bytes.Buffer
	app := service.New(service.Dependencies{
		NewBackend: func(credentials auth.Credentials) (service.Backend, error) {
			return coolify.NewClient(credentials.URL, credentials.Token)
		},
		PollInterval: time.Millisecond,
	})
	var run fixRun
	streams := ui.Streams{In: strings.NewReader(in), Out: &out, Err: &diagnostic, Interactive: true}
	run.result = cmd.Execute(context.Background(), app, streams, "test-version",
		append([]string{"--cwd", dir, "--coolify-config", configPath}, args...),
		cmd.WithOpener(func(url string) error { run.opened = append(run.opened, url); return nil }),
		cmd.WithEnvironment(func(string) string { return "" }))
	run.out, run.err = out.String(), diagnostic.String()
	return run
}

func saveContext(t *testing.T, path, name, url, token string) {
	t.Helper()
	if _, err := auth.Save(path, auth.Stored{Name: name, URL: url, Token: token}, true); err != nil {
		t.Fatal(err)
	}
}

func TestFixLoginRunsLoginThenTheCommandAgain(t *testing.T) {
	instance := newServer(t, &server{})
	dir := linkedDirectory(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	// Enter accepts the fix; then login's URL, context name, and token.
	run := executeWithFix(t, configPath, dir, "\n"+instance.URL+"\nhome\n"+testToken+"\n", "status")
	if run.result != nil {
		t.Fatalf("status after login: %v\nstderr: %s", run.result, run.err)
	}
	for _, want := range []string{"Error [no_credentials]:", "Coolship can run coolship login here", "Set it up now? [Y/n]", "Coolify URL:"} {
		if !strings.Contains(run.err, want) {
			t.Errorf("stderr lacks %q: %s", want, run.err)
		}
	}
	if !strings.Contains(run.out, "Logged in to home") || !strings.Contains(run.out, "fenix-bot") {
		t.Fatalf("stdout: %s", run.out)
	}
	// A read runs again at once, without asking.
	if strings.Contains(run.err, "Continue with") {
		t.Fatalf("a read asked before running again: %s", run.err)
	}
}

func TestFixReLoginReplacesTheRejectedToken(t *testing.T) {
	instance := newServer(t, &server{})
	dir := linkedDirectory(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	saveContext(t, configPath, "home", instance.URL, "revoked-token")
	run := executeWithFix(t, configPath, dir, "y\n"+testToken+"\n", "status")
	if run.result != nil {
		t.Fatalf("status after logging in again: %v\nstderr: %s", run.result, run.err)
	}
	if !strings.Contains(run.err, "Error [unauthorized]:") || !strings.Contains(run.err, "log in to home again") {
		t.Fatalf("stderr: %s", run.err)
	}
	if strings.Contains(run.err, "Coolify URL:") || strings.Contains(run.err, "Context name") {
		t.Fatalf("login asked for what the context already says: %s", run.err)
	}
	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), testToken) || strings.Contains(string(data), "revoked-token") {
		t.Fatalf("credentials file: %s", data)
	}
}

func TestFixLinkOffersLinkThenRunsTheCommandAgain(t *testing.T) {
	instance := newServer(t, &server{})
	dir := projectDirectory(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	saveContext(t, configPath, "home", instance.URL, testToken)
	// 1 picks link; link then asks only for the application, fenix-bot first.
	run := executeWithFix(t, configPath, dir, "1\n1\n", "status")
	if run.result != nil {
		t.Fatalf("status after link: %v\nstderr: %s", run.result, run.err)
	}
	if !strings.Contains(run.err, "Error [not_linked]:") || !strings.Contains(run.err, "Select how to link this directory:") {
		t.Fatalf("stderr: %s", run.err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); err != nil {
		t.Fatalf("link wrote no binding: %v", err)
	}
	if !strings.Contains(run.out, "Linked project") || !strings.Contains(run.out, "running") {
		t.Fatalf("stdout: %s", run.out)
	}
}

func TestFixLinkLeaveKeepsTheOriginalFailure(t *testing.T) {
	dir := projectDirectory(t)
	run := executeWithFix(t, filepath.Join(t.TempDir(), "config.json"), dir, "3\n", "status")
	if !errors.Is(run.result, service.ErrInput) || ui.ExitCode(run.result) != 2 {
		t.Fatalf("result = %v (exit %d)", run.result, ui.ExitCode(run.result))
	}
	if found, ok := problem.Classify(run.result); !ok || found.Code != problem.CodeNotLinked {
		t.Fatalf("the original failure was replaced: %v", run.result)
	}
	// The failure is already above the question; the boundary prints nothing more.
	var stderr bytes.Buffer
	if err := ui.ReportError(ui.Streams{Err: &stderr}, "human", run.result); err != nil || stderr.Len() != 0 {
		t.Fatalf("printed again: %q", stderr.String())
	}
}

func TestFixPickContextRunsAgainWithTheChosenContext(t *testing.T) {
	instance := newServer(t, &server{})
	dir := projectDirectory(t)
	data, err := config.Marshal(config.Config{Version: 1, Project: config.Binding{Context: "gone", Project: "Personal", Environment: "production", Application: "fenix-bot", Root: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coolship.toml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	saveContext(t, configPath, "home", instance.URL, testToken)
	run := executeWithFix(t, configPath, dir, "1\n", "status")
	if run.result != nil {
		t.Fatalf("status with the chosen context: %v\nstderr: %s", run.result, run.err)
	}
	for _, want := range []string{"Error [unknown_context]:", "Select context:", "Log in and save it as gone", "Using context home for this run"} {
		if !strings.Contains(run.err, want) {
			t.Errorf("stderr lacks %q: %s", want, run.err)
		}
	}
}

func TestFixOpenURLOpensThePageAndOffersOneRetry(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API is disabled."}`))
	}))
	t.Cleanup(refusing.Close)
	dir := linkedDirectory(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	saveContext(t, configPath, "home", refusing.URL, testToken)
	run := executeWithFix(t, configPath, dir, "\n\n", "status")
	if len(run.opened) != 1 || run.opened[0] != refusing.URL+"/settings/advanced" {
		t.Fatalf("opened = %q", run.opened)
	}
	if !strings.Contains(run.err, "Continue with coolship status? [Y/n]") {
		t.Fatalf("no question before the retry: %s", run.err)
	}
	// The retry failed the same way and is returned for the boundary to
	// print: no second offer.
	if strings.Count(run.err, "Set it up now?") != 1 {
		t.Fatalf("offered more than once: %s", run.err)
	}
	found, ok := problem.Classify(run.result)
	if !ok || found.Code != problem.CodeAPIDisabled {
		t.Fatalf("result = %v", run.result)
	}
	var stderr bytes.Buffer
	if err := ui.ReportError(ui.Streams{Err: &stderr}, "human", run.result); err != nil || !strings.Contains(stderr.String(), "Error [api_disabled]") {
		t.Fatalf("the retry's failure would not be printed: %q", stderr.String())
	}
}

// contextFailure is how the service reports a --context or committed context
// that is not saved.
func contextFailure() error {
	return &service.InputError{Err: &auth.ContextNotFoundError{Name: "gone", Saved: []string{"home"}}}
}

func TestFixBeforeAMutatingCommandAsksToContinue(t *testing.T) {
	for _, test := range []struct {
		answer string
		calls  int
	}{{"n\n", 1}, {"\n", 2}} {
		var contexts []string
		app := fakeApplication{
			contexts: func(service.Options) ([]auth.Instance, error) {
				return []auth.Instance{{Name: "home", URL: "https://coolify.example.com"}}, nil
			},
			deploy: func(_ context.Context, options service.DeployOptions, _ service.Emitter) (service.DeployResult, error) {
				contexts = append(contexts, options.Context)
				if options.Context != "home" {
					return service.DeployResult{}, contextFailure()
				}
				return service.DeployResult{DeploymentUUID: "d-1", Status: "finished"}, nil
			},
		}
		var out, diagnostic bytes.Buffer
		err := cmd.Execute(context.Background(), app, ui.Streams{In: strings.NewReader("1\n" + test.answer), Out: &out, Err: &diagnostic, Interactive: true},
			"test", []string{"deploy", "--context", "gone"})
		if len(contexts) != test.calls {
			t.Fatalf("answer %q: deploy ran with contexts %q\nstderr: %s", test.answer, contexts, diagnostic.String())
		}
		if !strings.Contains(diagnostic.String(), "Continue with coolship deploy? [Y/n]") {
			t.Fatalf("answer %q: stderr %s", test.answer, diagnostic.String())
		}
		switch test.calls {
		case 1:
			if ui.ExitCode(err) != 2 || !errors.Is(err, auth.ErrContextNotFound) {
				t.Fatalf("declined: err %v, exit %d", err, ui.ExitCode(err))
			}
		case 2:
			if err != nil || contexts[1] != "home" {
				t.Fatalf("accepted: err %v, contexts %q", err, contexts)
			}
		}
	}
}

func TestFixIsOfferedOnlyToAPersonWithHintsOn(t *testing.T) {
	off := false
	failure := &service.InputError{Err: auth.MissingCredentials("/nowhere/config.json")}
	for _, test := range []struct {
		name        string
		interactive bool
		args        []string
		opts        []cmd.Option
	}{
		{"noninteractive", false, []string{"status"}, nil},
		{"json", true, []string{"status", "--format", "json"}, nil},
		{"hints off", true, []string{"status"}, []cmd.Option{cmd.WithPreferences(preferences.Report{Preferences: preferences.Preferences{Hints: &off}})}},
		{"environment credentials", true, []string{"status"}, []cmd.Option{cmd.WithEnvironment(func(name string) string {
			if name == "COOLSHIP_TOKEN" {
				return "t"
			}
			return ""
		})}},
		{"login itself", true, []string{"logout", "home"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fakeApplication{
				status: func(context.Context, service.Options) (service.StatusResult, error) {
					return service.StatusResult{}, failure
				},
				logout: func(context.Context, service.LogoutOptions) (service.LogoutResult, error) {
					return service.LogoutResult{}, failure
				},
			}
			var out, diagnostic bytes.Buffer
			err := cmd.Execute(context.Background(), app, ui.Streams{In: strings.NewReader("\n"), Out: &out, Err: &diagnostic, Interactive: test.interactive},
				"test", test.args, test.opts...)
			if !errors.Is(err, failure) || diagnostic.Len() != 0 {
				t.Fatalf("err %v, stderr %q", err, diagnostic.String())
			}
			var stderr bytes.Buffer
			if _ = ui.ReportError(ui.Streams{Err: &stderr}, "human", err); stderr.Len() == 0 {
				t.Fatal("an unoffered failure is not printed by the boundary")
			}
		})
	}
}

func TestInitAsksToDeployAndDeploysTheSameTarget(t *testing.T) {
	var deployed []string
	app := fakeApplication{
		init: func(context.Context, service.InitOptions, service.Selector, service.ConfirmInit, service.Emitter) (service.InitResult, error) {
			return service.InitResult{Target: service.TargetInfo{Target: "web", ApplicationUUID: "a-1"}}, nil
		},
		state: func(context.Context, service.Options) (service.ProjectState, error) {
			return service.ProjectState{EnvFile: ".env", LocalVariables: 2}, nil
		},
		deploy: func(_ context.Context, options service.DeployOptions, _ service.Emitter) (service.DeployResult, error) {
			deployed = append(deployed, options.Target)
			return service.DeployResult{DeploymentUUID: "d-1", Status: "finished"}, nil
		},
	}
	for _, test := range []struct {
		answer string
		want   []string
	}{{"\n", nil}, {"y\n", []string{"web"}}} {
		deployed = nil
		out, diagnostic, err := executeInteractive(t, app, test.answer, "init", "--yes", "--target", "web")
		if err != nil || len(deployed) != len(test.want) || (len(deployed) == 1 && deployed[0] != "web") {
			t.Fatalf("answer %q: deployed %q, err %v, out %s", test.answer, deployed, err, out)
		}
		want := "Next:\n  coolship env push --target web        send the 2 variables in .env to Coolify\n  coolship domain set URL --target web  give the application a domain\nDeploy now? [y/N] "
		if !strings.HasPrefix(diagnostic, want) {
			t.Fatalf("answer %q: stderr %q, want prefix %q", test.answer, diagnostic, want)
		}
	}
}

func TestLinkHintsComeFromTheApplicationState(t *testing.T) {
	app := fakeApplication{
		link: func(context.Context, service.LinkOptions, service.Selector, service.Confirm) (service.LinkResult, error) {
			return service.LinkResult{Path: "coolship.toml", Target: service.TargetInfo{Application: "web"}}, nil
		},
		state: func(context.Context, service.Options) (service.ProjectState, error) {
			return service.ProjectState{Domains: []string{"https://web.example.com"}, Deployed: true}, nil
		},
	}
	_, diagnostic, err := executeInteractive(t, app, "", "link")
	if err != nil || diagnostic != "Next:\n  coolship logs  read its logs\n  coolship open  open it in a browser\n" {
		t.Fatalf("stderr %q, err %v", diagnostic, err)
	}
}
