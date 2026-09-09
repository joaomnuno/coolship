package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
)

type fakeBackend struct {
	projects     []models.Project
	environments []models.Environment
	application  models.Application
	receipts     []models.DeploymentReceipt
	deployments  []models.Deployment
	snapshots    []string
	calls        map[string]int
	readError    error
	versionError error
	variables    []models.EnvironmentVariable
	upserts      [][]models.EnvironmentVariableInput
	deleted      []string
	lastDeploy   models.DeployRequest
	more         map[string]models.Application // applications beyond app-1
	processes    []ProcessSpec
	exitCode     int
}

func newBackend() *fakeBackend {
	application := models.Application{UUID: "app-1", Name: "api", Status: "running:healthy", FQDN: "https://app.example.com"}
	return &fakeBackend{
		projects:     []models.Project{{UUID: "project-1", Name: "Personal"}},
		environments: []models.Environment{{UUID: "env-1", Name: "production", Applications: []models.Application{application}}},
		application:  application,
		receipts:     []models.DeploymentReceipt{{ResourceUUID: "app-1", DeploymentUUID: "deploy-1"}},
		deployments:  []models.Deployment{{UUID: "deploy-1", Status: "finished"}},
		snapshots:    []string{"2026-09-09T10:00:00Z hello\n"}, calls: map[string]int{},
	}
}

func (f *fakeBackend) ListEnvironmentVariables(_ context.Context, id string) ([]models.EnvironmentVariable, error) {
	f.calls["envs"]++
	if id != "app-1" {
		return nil, errors.New("wrong application")
	}
	return f.variables, nil
}

func (f *fakeBackend) UpsertEnvironmentVariables(_ context.Context, id string, items []models.EnvironmentVariableInput) error {
	f.calls["upsert"]++
	if id != "app-1" {
		return errors.New("wrong application")
	}
	f.upserts = append(f.upserts, items)
	return nil
}

func (f *fakeBackend) DeleteEnvironmentVariable(_ context.Context, id, variableUUID string) error {
	f.calls["delete"]++
	if id != "app-1" {
		return errors.New("wrong application")
	}
	f.deleted = append(f.deleted, variableUUID)
	return nil
}

func (f *fakeBackend) Version(context.Context) (string, error) {
	f.calls["version"]++
	if f.versionError != nil {
		return "", f.versionError
	}
	return "4.3.18", nil
}

func (f *fakeBackend) ListProjects(context.Context) ([]models.Project, error) {
	f.calls["projects"]++
	return f.projects, nil
}
func (f *fakeBackend) ListEnvironments(_ context.Context, id string) ([]models.Environment, error) {
	f.calls["environments"]++
	if id != "project-1" {
		return nil, errors.New("wrong project")
	}
	return f.environments, nil
}
func (f *fakeBackend) GetEnvironment(_ context.Context, projectID, id string) (models.Environment, error) {
	f.calls["environment"]++
	if projectID != "project-1" || id != "env-1" {
		return models.Environment{}, errors.New("wrong environment")
	}
	return f.environments[0], nil
}
func (f *fakeBackend) GetApplication(_ context.Context, id string) (models.Application, error) {
	f.calls["application"]++
	if id == "app-1" {
		return f.application, nil
	}
	if application, ok := f.more[id]; ok {
		return application, nil
	}
	return models.Application{}, errors.New("wrong application")
}
func (f *fakeBackend) Deploy(_ context.Context, request models.DeployRequest) ([]models.DeploymentReceipt, error) {
	f.calls["deploy"]++
	f.lastDeploy = request
	if request.ApplicationUUID != "app-1" {
		return nil, errors.New("wrong deployment target")
	}
	return f.receipts, nil
}
func (f *fakeBackend) GetDeployment(_ context.Context, id string) (models.Deployment, error) {
	index := f.calls["deployment"]
	f.calls["deployment"]++
	if id != "deploy-1" {
		return models.Deployment{}, errors.New("wrong deployment identity")
	}
	if f.readError != nil {
		return models.Deployment{}, f.readError
	}
	return f.deployments[min(index, len(f.deployments)-1)], nil
}
func (f *fakeBackend) Logs(_ context.Context, id string, _ int) (models.LogSnapshot, error) {
	index := f.calls["logs"]
	f.calls["logs"]++
	if id != "app-1" {
		return models.LogSnapshot{}, errors.New("wrong log target")
	}
	return models.LogSnapshot{Logs: f.snapshots[min(index, len(f.snapshots)-1)]}, nil
}

func testApp(f *fakeBackend) (*App, *int, *int) {
	credentials, factories := 0, 0
	app := New(Dependencies{
		RunProcess: func(_ context.Context, spec ProcessSpec) (int, error) {
			f.processes = append(f.processes, spec)
			return f.exitCode, nil
		},
		ResolveCredentials: func(auth.Options) (auth.Credentials, error) {
			credentials++
			return auth.Credentials{Name: "home", URL: "https://coolify.example.com", Token: "private-token"}, nil
		},
		InspectCredentials: func(auth.Options) auth.Report {
			return auth.Report{Source: "file", Path: "/home/test/.config/coolify/config.json", Exists: true, Mode: 0o600,
				Instances: []auth.Instance{{Name: "home", URL: "https://coolify.example.com", Default: true}}, Default: "home"}
		},
		ListInstances: func(auth.Options) ([]auth.Instance, error) {
			return []auth.Instance{{Name: "home", URL: "https://coolify.example.com", Default: true}}, nil
		},
		NewBackend:   func(auth.Credentials) (Backend, error) { factories++; return f, nil },
		PollInterval: time.Millisecond,
	})
	return app, &credentials, &factories
}

func linkedOptions(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	data, err := config.Marshal(config.Config{Version: 1, Project: config.Binding{Context: "home", Project: "Personal", Environment: "production", Application: "api", Root: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coolship.toml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	// A Git marker bounds discovery to this directory, whatever contains it.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return Options{CWD: dir}
}

func unlinkedDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorkflowsShareOnePreparationPerInvocation(t *testing.T) {
	for _, operation := range []string{"status", "deploy", "logs"} {
		t.Run(operation, func(t *testing.T) {
			f := newBackend()
			app, credentials, factories := testApp(f)
			options := linkedOptions(t)
			var err error
			switch operation {
			case "status":
				var result StatusResult
				result, err = app.Status(context.Background(), options)
				if result.Status != "running:healthy" || result.Target.ApplicationUUID != "app-1" {
					t.Fatalf("unexpected status: %+v", result)
				}
			case "deploy":
				_, err = app.Deploy(context.Background(), DeployOptions{Options: options}, nil)
			case "logs":
				err = app.Logs(context.Background(), LogsOptions{Options: options, Lines: 100}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if *credentials != 1 || *factories != 1 {
				t.Fatalf("credentials=%d clients=%d", *credentials, *factories)
			}
			for _, read := range []string{"projects", "environments", "environment", "application"} {
				if f.calls[read] != 1 {
					t.Errorf("%s resolved %d times", read, f.calls[read])
				}
			}
		})
	}
}

func TestDeploymentObservesExactIdentityAndTerminalState(t *testing.T) {
	f := newBackend()
	f.deployments = []models.Deployment{{UUID: "deploy-1", Status: "queued"}, {UUID: "deploy-1", Status: "in_progress"}, {UUID: "deploy-1", Status: "finished"}}
	app, _, _ := testApp(f)
	var states []string
	result, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t)}, func(e Event) error { states = append(states, e.Status); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "finished" || !reflect.DeepEqual(states, []string{"queued", "in_progress", "finished"}) {
		t.Fatalf("result=%+v states=%v", result, states)
	}
	if f.calls["deploy"] != 1 || f.calls["projects"] != 1 {
		t.Fatalf("unexpected repeated preparation/submission: %v", f.calls)
	}
}

func TestDeploymentFailureContracts(t *testing.T) {
	for _, test := range []struct {
		name   string
		alter  func(*fakeBackend)
		noWait bool
		want   string
	}{
		{"missing receipt", func(f *fakeBackend) { f.receipts = nil }, false, "did not confirm"},
		{"wrong resource", func(f *fakeBackend) { f.receipts[0].ResourceUUID = "someone-else" }, false, "did not confirm"},
		{"message only", func(f *fakeBackend) { f.receipts[0].DeploymentUUID = "" }, false, "did not confirm"},
		{"multiple receipts", func(f *fakeBackend) { f.receipts = append(f.receipts, f.receipts[0]) }, false, "multiple deployments"},
		{"remote failed", func(f *fakeBackend) { f.deployments[0].Status = "failed" }, false, "deploy-1: ended with status failed"},
		{"remote cancelled", func(f *fakeBackend) { f.deployments[0].Status = "cancelled-by-user" }, false, "cancelled-by-user"},
		{"wrong identity", func(f *fakeBackend) { f.deployments[0].UUID = "another-deploy" }, false, "different deployment identity"},
		{"read failed", func(f *fakeBackend) { f.readError = errors.New("network unavailable") }, false, "deploy-1: observation stopped"},
		{"queued only", func(*fakeBackend) {}, true, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newBackend()
			test.alter(f)
			app, _, _ := testApp(f)
			result, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t), NoWait: test.noWait}, nil)
			if test.want == "" {
				if err != nil || result.Status != "queued" || f.calls["deployment"] != 0 {
					t.Fatalf("result=%+v err=%v calls=%v", result, err, f.calls)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
			if f.calls["deploy"] != 1 {
				t.Fatalf("submission repeated: %v", f.calls)
			}
		})
	}
}

func TestDeploymentCancellationRetainsRecoveryIdentity(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := app.Deploy(ctx, DeployOptions{Options: linkedOptions(t)}, func(Event) error { cancel(); return nil })
	var deploymentError *DeploymentError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &deploymentError) || result.DeploymentUUID != "deploy-1" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if f.calls["deployment"] != 0 || f.calls["deploy"] != 1 {
		t.Fatal(f.calls)
	}
}

func TestUnknownDeploymentStateTimesOut(t *testing.T) {
	f := newBackend()
	f.deployments[0].Status = "future-state"
	app, _, _ := testApp(f)
	_, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t), Timeout: 20 * time.Millisecond}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestOutputFailureStopsPolling(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	broken := errors.New("broken output")
	_, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t)}, func(Event) error { return broken })
	if !errors.Is(err, broken) || f.calls["deployment"] != 0 {
		t.Fatalf("error=%v calls=%v", err, f.calls)
	}
}

func TestLogFollowOverlapAndCancellation(t *testing.T) {
	f := newBackend()
	f.snapshots = []string{"t1 a\nt2 b\n", "t2 b\nt3 b\n", "t8 restart\n"}
	app, credentials, factories := testApp(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logs []string
	warnings := 0
	err := app.Logs(ctx, LogsOptions{Options: linkedOptions(t), Lines: 2, Follow: true}, func(event Event) error {
		if event.Type == "warning" {
			warnings++
		}
		if event.Type == "logs" {
			logs = append(logs, event.Logs)
			if len(logs) == 3 {
				cancel()
			}
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(logs, []string{"t1 a\nt2 b\n", "t3 b\n", "t8 restart\n"}) || warnings != 1 {
		t.Fatalf("logs=%q warnings=%d error=%v", logs, warnings, err)
	}
	if *credentials != 1 || *factories != 1 {
		t.Fatalf("credentials=%d factories=%d", *credentials, *factories)
	}
}

func TestSnapshotDelta(t *testing.T) {
	for _, test := range []struct {
		before, after, want string
		reset               bool
	}{
		{"", "t1 a\n", "t1 a\n", false},
		{"t1 a\n", "t1 a\n", "", false},
		{"t1 a\nt2 b\n", "t2 b\nt3 b\n", "t3 b\n", false},
		{"t1 a\n", "t1 a\nt1 a\n", "t1 a\n", false},
		{"t1 a\n", "t2 b\n", "t2 b\n", true},
		{"t1 a\n", "", "", true},
		// Coolify 4.3.18 omits the newline after the final line; the same line
		// must still match once a later snapshot moves it earlier.
		{"t1 a\nt2 b", "t2 b\nt3 c", "t3 c\n", false},
		{"t1 a", "t1 a", "", false},
		{"t1 a", "t1 a\nt2 b", "t2 b\n", false},
		{"", "t1 a", "t1 a\n", false},
	} {
		got, reset := snapshotDelta(test.before, test.after)
		if got != test.want || reset != test.reset {
			t.Errorf("delta(%q,%q)=(%q,%t), want (%q,%t)", test.before, test.after, got, reset, test.want, test.reset)
		}
	}
}

func TestLinkPersistsBindingAndRequiresReviewedReplacement(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	dir := unlinkedDirectory(t)
	options := LinkOptions{Options: Options{CWD: dir}, Project: "Personal", Application: "api"}
	result, err := app.Link(context.Background(), options, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.ApplicationUUID != "app-1" {
		t.Fatal(result)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-token") {
		t.Fatal("credential persisted")
	}
	parsed, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Project.Context != "home" || parsed.Project.Application != "api" {
		t.Fatal(parsed)
	}
	// Same binding preserves the existing document, including comments.
	original := append([]byte("# preserve this comment\n"), data...)
	if err := os.WriteFile(result.Path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Link(context.Background(), options, nil, nil); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(result.Path)
	if string(unchanged) != string(original) {
		t.Fatal("idempotent link rewrote comments")
	}
	options.ApplicationUUID = "app-1"
	_, err = app.Link(context.Background(), options, nil, nil)
	if !errors.Is(err, project.ErrReplacementRequired) {
		t.Fatalf("expected replacement guard, got %v", err)
	}
	_, err = app.Link(context.Background(), options, nil, func(context.Context, LinkPlan) (bool, error) { return false, nil })
	if !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	options.Replace = true
	if _, err = app.Link(context.Background(), options, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestLinkCancellationDoesNotWrite(t *testing.T) {
	f := newBackend()
	f.environments[0].Applications = append(f.environments[0].Applications, models.Application{UUID: "app-2", Name: "other"})
	app, _, _ := testApp(f)
	dir := unlinkedDirectory(t)
	_, err := app.Link(context.Background(), LinkOptions{Options: Options{CWD: dir}}, func(context.Context, string, []Choice) (string, error) { return "", ErrCancelled }, nil)
	if !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled link wrote config: %v", err)
	}
}

func TestCredentialPairOverridesCommittedContextButNotExplicitFlags(t *testing.T) {
	app, _, _ := testApp(newBackend())
	app.deps.CredentialURL, app.deps.CredentialToken = "https://ci.example.com", "private-token"
	if got := app.authOptions(Options{}, "home"); got.Context != "" || got.URL != "https://ci.example.com" {
		t.Fatal(got.Context)
	}
	if got := app.authOptions(Options{Context: "explicit"}, "home"); got.Context != "explicit" {
		t.Fatal(got.Context)
	}
}

func TestDeploymentStreamsVisibleBuildLogsOnce(t *testing.T) {
	f := newBackend()
	first := `[{"command":null,"output":"Starting deployment.","type":"stdout","hidden":false},{"command":"docker build","output":"internal","type":"stdout","hidden":true}]`
	second := first[:len(first)-1] + `,{"command":null,"output":"Building docker image started.\n","type":"stdout","hidden":false}]`
	f.deployments = []models.Deployment{
		{UUID: "deploy-1", Status: "in_progress", Logs: &first},
		{UUID: "deploy-1", Status: "in_progress", Logs: &second},
		{UUID: "deploy-1", Status: "finished", Logs: &second},
	}
	app, _, _ := testApp(f)
	var builds []string
	warnings := 0
	_, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t)}, func(e Event) error {
		switch e.Type {
		case "build":
			builds = append(builds, e.Logs)
		case "warning":
			warnings++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(builds, []string{"Starting deployment.\n", "Building docker image started.\n"}) || warnings != 0 {
		t.Fatalf("builds=%q warnings=%d", builds, warnings)
	}
}

func TestDeploymentToleratesWithheldAndUnreadableBuildLogs(t *testing.T) {
	broken := `{"not":"an array"}`
	for _, test := range []struct {
		name     string
		logs     *string
		warnings int
	}{
		{"withheld", nil, 0},
		{"unreadable", &broken, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newBackend()
			f.deployments = []models.Deployment{
				{UUID: "deploy-1", Status: "in_progress", Logs: test.logs},
				{UUID: "deploy-1", Status: "in_progress", Logs: test.logs},
				{UUID: "deploy-1", Status: "finished", Logs: test.logs},
			}
			app, _, _ := testApp(f)
			builds, warnings := 0, 0
			result, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t)}, func(e Event) error {
				switch e.Type {
				case "build":
					builds++
				case "warning":
					warnings++
				}
				return nil
			})
			if err != nil || result.Status != "finished" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			// An unreadable document warns exactly once across all polls and never fails the deployment.
			if builds != 0 || warnings != test.warnings {
				t.Fatalf("builds=%d warnings=%d", builds, warnings)
			}
		})
	}
}

type statusError struct{ code int }

func (e statusError) Error() string       { return fmt.Sprintf("http %d", e.code) }
func (e statusError) HTTPStatusCode() int { return e.code }

func TestOpenResolvesApplicationOrDashboardURL(t *testing.T) {
	f := newBackend()
	f.application.FQDN = "https://app.example.com, https://alias.example.com,javascript:alert(1)"
	f.environments[0].Applications[0] = f.application
	app, _, _ := testApp(f)
	result, err := app.Open(context.Background(), OpenOptions{Options: linkedOptions(t)})
	if err != nil || result.Kind != "application" || result.URL != "https://app.example.com" || len(result.Warnings) != 1 || strings.Contains(result.Warnings[0], "javascript") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = app.Open(context.Background(), OpenOptions{Options: linkedOptions(t), Dashboard: true})
	if err != nil || result.Kind != "dashboard" || result.URL != "https://coolify.example.com/project/project-1/environment/env-1/application/app-1" {
		t.Fatalf("dashboard result=%+v err=%v", result, err)
	}
	f.application.FQDN = ""
	f.environments[0].Applications[0] = f.application
	if _, err := app.Open(context.Background(), OpenOptions{Options: linkedOptions(t)}); !errors.Is(err, ErrInput) {
		t.Fatalf("no domain should be an input error, got %v", err)
	}
}

func TestUnlinkRemovesOnlyTheReviewedFile(t *testing.T) {
	f := newBackend()
	app, credentials, _ := testApp(f)
	options := linkedOptions(t)
	path := filepath.Join(options.CWD, "coolship.toml")
	if _, err := app.Unlink(context.Background(), UnlinkOptions{Options: options}, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("nil confirm must be an input error, got %v", err)
	}
	declined := func(context.Context, UnlinkPlan) (bool, error) { return false, nil }
	if _, err := app.Unlink(context.Background(), UnlinkOptions{Options: options}, declined); !errors.Is(err, ErrCancelled) {
		t.Fatalf("declined confirm: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("declined unlink removed the file")
	}
	var plan UnlinkPlan
	accepted := func(_ context.Context, p UnlinkPlan) (bool, error) { plan = p; return true, nil }
	result, err := app.Unlink(context.Background(), UnlinkOptions{Options: options}, accepted)
	if err != nil || result.Path != path || plan.Binding.Application != "api" {
		t.Fatalf("result=%+v plan=%+v err=%v", result, plan, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file still exists after unlink")
	}
	if *credentials != 0 || f.calls["projects"] != 0 {
		t.Fatal("unlink must not touch credentials or the server")
	}
	if _, err := app.Unlink(context.Background(), UnlinkOptions{Options: options, Yes: true}, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("unlinking an unlinked project: %v", err)
	}
}

func TestConfigIsLocalAndReportsCredentialProblemsAsWarnings(t *testing.T) {
	f := newBackend()
	app, _, factories := testApp(f)
	options := linkedOptions(t)
	options.Environment = "staging"
	options.Context = "other"
	result, err := app.Config(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Binding.Environment != "staging" || result.Overrides["environment"] != "staging" || result.Overrides["context"] != "other" {
		t.Fatalf("overrides not applied: %+v", result)
	}
	if result.Instance != "home" || result.CredentialSource != "file" || *factories != 0 || f.calls["projects"] != 0 {
		t.Fatalf("config must resolve credentials locally and never build a backend: %+v factories=%d calls=%v", result, *factories, f.calls)
	}
	broken := New(Dependencies{
		ResolveCredentials: func(auth.Options) (auth.Credentials, error) {
			return auth.Credentials{}, errors.New("no default instance")
		},
		InspectCredentials: func(auth.Options) auth.Report { return auth.Report{Source: "file", Path: "/nowhere"} },
		NewBackend:         func(auth.Credentials) (Backend, error) { return f, nil },
	})
	result, err = broken.Config(context.Background(), linkedOptions(t))
	if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "no default instance") {
		t.Fatalf("credential failure should be a warning: result=%+v err=%v", result, err)
	}
}

func TestDoctorReportsEveryStep(t *testing.T) {
	statuses := func(result DoctorResult) map[string]string {
		out := map[string]string{}
		for _, check := range result.Checks {
			if _, seen := out[check.Name]; !seen {
				out[check.Name] = check.Status
			}
		}
		return out
	}
	t.Run("healthy", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		result, err := app.Doctor(context.Background(), linkedOptions(t))
		if err != nil || result.Failed {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		got := statuses(result)
		for _, name := range []string{"Project configuration", "Binding", "Credentials", "Context", "Server", "Application"} {
			if got[name] != "ok" {
				t.Errorf("%s = %q, want ok (all: %v)", name, got[name], got)
			}
		}
		if got["Git repository"] != "ok" {
			t.Errorf("Git repository = %q", got["Git repository"])
		}
	})
	t.Run("unlinked still checks credentials and server", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		result, err := app.Doctor(context.Background(), Options{CWD: unlinkedDirectory(t)})
		if err != nil || !result.Failed {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		got := statuses(result)
		if got["Project configuration"] != "failed" || got["Server"] != "ok" || got["Application"] != "skipped" {
			t.Fatalf("unexpected statuses %v", got)
		}
	})
	t.Run("rejected token stops at the server", func(t *testing.T) {
		f := newBackend()
		f.versionError = statusError{code: 401}
		app, _, _ := testApp(f)
		result, _ := app.Doctor(context.Background(), linkedOptions(t))
		got := statuses(result)
		if got["Server"] != "failed" || got["Application"] != "" || !result.Failed {
			t.Fatalf("unexpected statuses %v", got)
		}
		for _, check := range result.Checks {
			if check.Name == "Server" && !strings.Contains(check.Detail, "401") {
				t.Fatalf("server detail should classify the status: %q", check.Detail)
			}
		}
	})
	t.Run("missing credentials file", func(t *testing.T) {
		f := newBackend()
		app := New(Dependencies{
			ResolveCredentials: func(auth.Options) (auth.Credentials, error) {
				return auth.Credentials{}, errors.New("read Coolify CLI configuration: no such file")
			},
			InspectCredentials: func(auth.Options) auth.Report {
				return auth.Report{Source: "file", Path: "/nowhere/config.json", Err: os.ErrNotExist}
			},
			NewBackend: func(auth.Credentials) (Backend, error) { return f, nil },
		})
		result, _ := app.Doctor(context.Background(), linkedOptions(t))
		got := statuses(result)
		if got["Credentials"] != "failed" || got["Context"] != "failed" || got["Server"] != "" {
			t.Fatalf("unexpected statuses %v", got)
		}
	})
}

func str(value string) *string { return &value }

func variable(key, value string, preview bool) models.EnvironmentVariable {
	return models.EnvironmentVariable{UUID: "env-" + key + "-" + scopeName(preview), Key: key, Value: str(value), RealValue: str(value), IsPreview: preview, IsBuildTime: true, IsRuntime: true}
}

func TestEnvDiffClassifiesByScopeAndNeverComparesWithheldValues(t *testing.T) {
	f := newBackend()
	withheld := variable("SECRET", "", false)
	withheld.Value, withheld.RealValue, withheld.IsShownOnce = nil, nil, true
	f.variables = []models.EnvironmentVariable{
		variable("SAME", "1", false), variable("CHANGED", "remote", false), variable("REMOTE_ONLY", "r", false), withheld,
		variable("SAME", "preview-1", true), variable("CHANGED", "preview", true),
	}
	app, _, _ := testApp(f)
	options := linkedOptions(t)
	if err := os.WriteFile(filepath.Join(options.CWD, ".env"), []byte("# local\nSAME=1\nCHANGED=local\nLOCAL_ONLY=l\nSECRET=mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := app.EnvDiff(context.Background(), EnvOptions{Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scope != "regular" || result.Unchanged != 1 ||
		!reflect.DeepEqual(result.Added, []EnvChange{{Key: "LOCAL_ONLY", Local: "l"}}) ||
		!reflect.DeepEqual(result.Changed, []EnvChange{{Key: "CHANGED", Local: "local", Remote: "remote"}}) ||
		!reflect.DeepEqual(result.Removed, []EnvChange{{Key: "REMOTE_ONLY", Remote: "r"}}) ||
		!reflect.DeepEqual(result.Withheld, []string{"SECRET"}) {
		t.Fatalf("unexpected diff %+v", result)
	}
	preview, err := app.EnvDiff(context.Background(), EnvOptions{Options: options, Preview: true})
	if err != nil || preview.Scope != "preview" || len(preview.Changed) != 2 || len(preview.Removed) != 0 || len(preview.Withheld) != 0 {
		t.Fatalf("preview diff %+v err=%v", preview, err)
	}
}

func TestEnvPullKeepsLocalKeysAndNeverInventsWithheldValues(t *testing.T) {
	f := newBackend()
	withheld := variable("SECRET", "", false)
	withheld.Value, withheld.RealValue, withheld.IsShownOnce = nil, nil, true
	shared := variable("SHARED", "{{team.TOKEN}}", false)
	shared.RealValue, shared.IsShared = str("resolved-secret"), true
	f.variables = []models.EnvironmentVariable{variable("A", "remote a", false), withheld, shared, variable("A", "preview a", true)}
	app, _, _ := testApp(f)
	options := linkedOptions(t)
	path := filepath.Join(options.CWD, ".env")
	if err := os.WriteFile(path, []byte("# keep this comment\nLOCAL=l\nA=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := app.EnvPull(context.Background(), EnvOptions{Options: options})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# keep this comment\nLOCAL=l\nA=\"remote a\"\n# SECRET is withheld by Coolify (shown once); set it here yourself\nSHARED={{team.TOKEN}}\n"
	if string(data) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", data, want)
	}
	if !reflect.DeepEqual(result.Written, []string{"A", "SHARED"}) || !reflect.DeepEqual(result.Kept, []string{"LOCAL"}) || !reflect.DeepEqual(result.Withheld, []string{"SECRET"}) {
		t.Fatalf("result %+v", result)
	}
	if strings.Contains(string(data), "resolved-secret") {
		t.Fatal("pull wrote a resolved shared value instead of the reference")
	}
	// A withheld key the developer already has locally is left alone.
	if err := os.WriteFile(path, []byte("SECRET=mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.EnvPull(context.Background(), EnvOptions{Options: options}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(data), "SECRET=mine\n") || strings.Contains(string(data), "SECRET is withheld") {
		t.Fatalf("existing local secret disturbed: %s", data)
	}
}

func TestEnvPushPlansConfirmsAndAppliesWithinScope(t *testing.T) {
	f := newBackend()
	withheld := variable("SECRET", "", false)
	withheld.Value, withheld.RealValue, withheld.IsShownOnce = nil, nil, true
	f.variables = []models.EnvironmentVariable{variable("SAME", "1", false), variable("CHANGED", "remote", false), variable("REMOTE_ONLY", "r", false), withheld}
	app, _, _ := testApp(f)
	options := linkedOptions(t)
	path := filepath.Join(options.CWD, ".env")
	if _, err := app.EnvPush(context.Background(), EnvPushOptions{EnvOptions: EnvOptions{Options: options}, Yes: true}, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("missing file should be an input error: %v", err)
	}
	if err := os.WriteFile(path, []byte("SAME=1\nCHANGED=local\nNEW=n\nSECRET=mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	push := EnvPushOptions{EnvOptions: EnvOptions{Options: options}}
	if _, err := app.EnvPush(context.Background(), push, nil); !errors.Is(err, ErrInput) || f.calls["upsert"] != 0 {
		t.Fatalf("noninteractive push without --yes: err=%v upserts=%d", err, f.calls["upsert"])
	}
	declined := func(context.Context, EnvPushPlan) (bool, error) { return false, nil }
	if _, err := app.EnvPush(context.Background(), push, declined); !errors.Is(err, ErrCancelled) || f.calls["upsert"] != 0 {
		t.Fatalf("declined push: err=%v upserts=%d", err, f.calls["upsert"])
	}
	var plan EnvPushPlan
	accepted := func(_ context.Context, p EnvPushPlan) (bool, error) { plan = p; return true, nil }
	result, err := app.EnvPush(context.Background(), push, accepted)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Create, []EnvChange{{Key: "NEW", Local: "n"}}) || !reflect.DeepEqual(plan.Update, []EnvChange{{Key: "CHANGED", Local: "local", Remote: "remote"}}) ||
		len(plan.Delete) != 0 || !reflect.DeepEqual(plan.Skipped, []string{"SECRET"}) {
		t.Fatalf("plan %+v", plan)
	}
	no := false
	if len(f.upserts) != 1 || !reflect.DeepEqual(f.upserts[0], []models.EnvironmentVariableInput{
		{Key: "NEW", Value: "n"},
		{Key: "CHANGED", Value: "local", IsLiteral: &no, IsMultiline: &no, IsShownOnce: &no},
	}) || len(f.deleted) != 0 {
		t.Fatalf("upserts=%+v deleted=%v", f.upserts, f.deleted)
	}
	if len(result.Warnings) < 2 || !strings.Contains(strings.Join(result.Warnings, " "), "--prune") || !strings.Contains(strings.Join(result.Warnings, " "), "--force") {
		t.Fatalf("warnings %v", result.Warnings)
	}
	// --prune deletes by identity; --force overwrites the withheld key; preview scope is carried on every item.
	f.upserts, f.deleted = nil, nil
	push.Prune, push.Force, push.Yes, push.Preview = true, true, true, true
	f.variables = append(f.variables, variable("PREVIEW_ONLY", "p", true))
	if _, err := app.EnvPush(context.Background(), push, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.deleted, []string{"env-PREVIEW_ONLY-preview"}) {
		t.Fatalf("deleted %v", f.deleted)
	}
	for _, item := range f.upserts[0] {
		if !item.IsPreview {
			t.Fatalf("preview scope not carried: %+v", item)
		}
	}
	// Updating a withheld (shown-once) key with --force restates is_shown_once,
	// and a literal flag survives; the server resets both when absent.
	f.upserts = nil
	literal := variable("LITERAL", "$old", false)
	literal.IsLiteral = true
	f.variables = []models.EnvironmentVariable{literal, withheld}
	if err := os.WriteFile(path, []byte("LITERAL=$new\nSECRET=rotated\nMULTI=\"a\nb\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.EnvPush(context.Background(), EnvPushOptions{EnvOptions: EnvOptions{Options: options}, Yes: true, Force: true}, nil); err != nil {
		t.Fatal(err)
	}
	flags := map[string][3]bool{}
	for _, item := range f.upserts[0] {
		deref := func(b *bool) bool { return b != nil && *b }
		flags[item.Key] = [3]bool{deref(item.IsLiteral), deref(item.IsMultiline), deref(item.IsShownOnce)}
	}
	if flags["LITERAL"] != [3]bool{true, false, false} || flags["SECRET"] != [3]bool{false, false, true} || flags["MULTI"] != [3]bool{false, true, false} {
		t.Fatalf("flags not preserved: %v", flags)
	}
	// Nothing to do is not an error and makes no write.
	f.upserts = nil
	f.variables = []models.EnvironmentVariable{variable("ONLY", "1", false)}
	if err := os.WriteFile(path, []byte("ONLY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = app.EnvPush(context.Background(), EnvPushOptions{EnvOptions: EnvOptions{Options: options}, Yes: true}, nil)
	if err != nil || !result.Plan.Empty() || len(f.upserts) != 0 {
		t.Fatalf("empty plan: result=%+v err=%v upserts=%v", result, err, f.upserts)
	}
}

func TestPreviewDeploymentCarriesPullRequestAndSurfacesRefusal(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	result, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t), PullRequest: 42, Force: true}, nil)
	if err != nil || result.PullRequest != 42 || f.lastDeploy != (models.DeployRequest{ApplicationUUID: "app-1", Force: true, PullRequest: 42}) {
		t.Fatalf("result=%+v request=%+v err=%v", result, f.lastDeploy, err)
	}
	// Coolify 4.3.18 answers an unknown pull request with HTTP 200 and a receipt
	// that has a message but no deployment UUID.
	f.receipts = []models.DeploymentReceipt{{ResourceUUID: "app-1", Message: "Pull request 42 not found for this resource."}}
	_, err = app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t), PullRequest: 42}, nil)
	if err == nil || !strings.Contains(err.Error(), "Pull request 42 not found") || !strings.Contains(err.Error(), "enable preview deployments") {
		t.Fatalf("refusal not surfaced: %v", err)
	}
	if _, err := app.Deploy(context.Background(), DeployOptions{Options: linkedOptions(t), PullRequest: -1}, nil); !errors.Is(err, ErrInput) {
		t.Fatalf("negative pull request: %v", err)
	}
}

func TestLinkWritesNamedTargetsWithDirectoryRoots(t *testing.T) {
	f := newBackend()
	backend := models.Application{UUID: "app-2", Name: "backend", Status: "running:healthy"}
	f.environments[0].Applications = append(f.environments[0].Applications, backend)
	f.more = map[string]models.Application{"app-2": backend}
	app, _, _ := testApp(f)
	root := unlinkedDirectory(t)
	for _, dir := range []string{"apps/web", "apps/api"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Linking a target from its directory records that directory as the root.
	first := LinkOptions{Options: Options{CWD: filepath.Join(root, "apps/web"), Target: "web"}, Project: "Personal", Application: "api"}
	result, err := app.Link(context.Background(), first, nil, nil)
	if err != nil || result.Target.Target != "web" || result.Path != filepath.Join(root, "coolship.toml") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// A second target is added without review, and the first survives.
	second := LinkOptions{Options: Options{CWD: filepath.Join(root, "apps/api"), Target: "api"}, Project: "Personal", ApplicationUUID: "app-2"}
	if _, err := app.Link(context.Background(), second, nil, nil); err != nil {
		t.Fatalf("second target: %v", err)
	}
	data, _ := os.ReadFile(result.Path)
	parsed, err := config.Parse(data)
	if err != nil || len(parsed.Apps) != 2 || parsed.Apps["web"].Root != "apps/web" || parsed.Apps["api"].Root != "apps/api" || parsed.Project.IsSet() {
		t.Fatalf("configuration %s: %v", data, err)
	}
	// Selection follows the working directory, and TargetInfo names the target.
	status, err := app.Status(context.Background(), Options{CWD: filepath.Join(root, "apps/api")})
	if err != nil || status.Target.Target != "api" || status.Target.ApplicationUUID != "app-2" {
		t.Fatalf("status from apps/api: %+v err=%v", status, err)
	}
	if _, err := app.Status(context.Background(), Options{CWD: root}); !errors.Is(err, ErrInput) {
		t.Fatalf("repository root without a target must ask: %v", err)
	}
	// Converting to the single form requires review, and the plan says so.
	var plan LinkPlan
	convert := LinkOptions{Options: Options{CWD: root}, Project: "Personal", ApplicationUUID: "app-2"}
	_, err = app.Link(context.Background(), convert, nil, func(_ context.Context, p LinkPlan) (bool, error) { plan = p; return false, nil })
	if !errors.Is(err, ErrCancelled) || !plan.Replacing || !plan.Converting {
		t.Fatalf("conversion plan=%+v err=%v", plan, err)
	}
}

func TestDevInjectsResolvedRuntimeVariablesAndPropagatesStatus(t *testing.T) {
	f := newBackend()
	shared := variable("SHARED", "{{team.TOKEN}}", false)
	shared.RealValue, shared.IsShared = str("resolved"), true
	buildOnly := variable("BUILD_ONLY", "b", false)
	buildOnly.IsRuntime = false
	withheld := variable("SECRET", "", false)
	withheld.Value, withheld.RealValue, withheld.IsShownOnce = nil, nil, true
	f.variables = []models.EnvironmentVariable{variable("PLAIN", "p", false), shared, buildOnly, withheld, variable("PLAIN", "preview-p", true)}
	app, _, _ := testApp(f)
	options := linkedOptions(t)
	var messages []string
	emit := func(e Event) error { messages = append(messages, e.Type+": "+e.Message); return nil }
	if err := app.Dev(context.Background(), DevOptions{Options: options, Command: []string{"printenv"}}, emit); err != nil {
		t.Fatal(err)
	}
	spec := f.processes[0]
	if !reflect.DeepEqual(spec.Args, []string{"printenv"}) || spec.Shell != "" || spec.Dir != options.CWD ||
		!reflect.DeepEqual(spec.Env, []string{"PLAIN=p", "SHARED=resolved"}) {
		t.Fatalf("spec %+v", spec)
	}
	if len(messages) != 2 || !strings.Contains(messages[0], "SECRET") || !strings.Contains(messages[1], "2 regular variable(s)") {
		t.Fatalf("messages %v", messages)
	}
	// Preview scope, configured shell command, and exit status.
	data, _ := config.Marshal(config.Config{Version: 1, Project: config.Binding{Context: "home", Project: "Personal", Environment: "production", Application: "api", Dev: "npm run dev"}})
	if err := os.WriteFile(filepath.Join(options.CWD, "coolship.toml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	f.exitCode = 5
	err := app.Dev(context.Background(), DevOptions{Options: options, Preview: true}, nil)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 5 {
		t.Fatalf("exit status: %v", err)
	}
	spec = f.processes[1]
	if spec.Shell != "npm run dev" || len(spec.Args) != 0 || !reflect.DeepEqual(spec.Env, []string{"PLAIN=preview-p"}) {
		t.Fatalf("configured spec %+v", spec)
	}
	// No command anywhere is an input error before any process runs.
	data, _ = config.Marshal(config.Config{Version: 1, Project: config.Binding{Context: "home", Project: "Personal", Environment: "production", Application: "api"}})
	if err := os.WriteFile(filepath.Join(options.CWD, "coolship.toml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.Dev(context.Background(), DevOptions{Options: options}, nil); !errors.Is(err, ErrInput) || len(f.processes) != 2 {
		t.Fatalf("no command: err=%v processes=%d", err, len(f.processes))
	}
}
