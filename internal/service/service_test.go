package service

import (
	"context"
	"errors"
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
	if id != "app-1" {
		return models.Application{}, errors.New("wrong application")
	}
	return f.application, nil
}
func (f *fakeBackend) Deploy(_ context.Context, id string, _ bool) ([]models.DeploymentReceipt, error) {
	f.calls["deploy"]++
	if id != "app-1" {
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
		ResolveCredentials: func(auth.Options) (auth.Credentials, error) {
			credentials++
			return auth.Credentials{Name: "home", URL: "https://coolify.example.com", Token: "private-token"}, nil
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
