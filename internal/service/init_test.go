package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/models"
)

func TestInitPlansConfirmsCreatesAndBinds(t *testing.T) {
	f := newBackend()
	app, credentials, factories := testApp(f)
	dir := unlinkedDirectory(t)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	options := InitOptions{Options: Options{CWD: dir}, Project: "Personal"}

	// Noninteractive without --yes: the plan is complete, nothing is created.
	if _, err := app.Init(context.Background(), options, nil, nil, nil); !errors.Is(err, ErrInput) || f.calls["create-application"] != 0 {
		t.Fatalf("noninteractive without --yes: err=%v calls=%v", err, f.calls)
	}
	// Declined: nothing is created, nothing is written.
	var plan InitPlan
	declined := func(_ context.Context, p InitPlan) (bool, error) { plan = p; return false, nil }
	if _, err := app.Init(context.Background(), options, nil, declined, nil); !errors.Is(err, ErrCancelled) || f.calls["create-application"] != 0 {
		t.Fatalf("declined: err=%v calls=%v", err, f.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled init wrote configuration")
	}
	want := InitPlan{Path: filepath.Join(dir, "coolship.toml"), Target: "default", Root: ".", Repository: "https://github.com/owner/new-app", Branch: "main",
		BuildPack: "dockerfile", Port: 80, Dockerfile: "/Dockerfile", Name: "new-app", Instance: "home", Project: "Personal", Environment: "production", Server: "Master"}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan=%+v\nwant %+v", plan, want)
	}
	if !reflect.DeepEqual(f.inspected, []string{dir, dir}) {
		t.Fatalf("Git inspected in %v", f.inspected)
	}

	// Accepted: one request creates the application, the binding is written and verified.
	accepted := func(context.Context, InitPlan) (bool, error) { return true, nil }
	result, err := app.Init(context.Background(), options, nil, accepted, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Plan, want) || result.Target.ApplicationUUID != "app-new-app" || result.Target.Application != "new-app" || result.URL != "https://app-new-app.example.com" || result.Deployment != nil {
		t.Fatalf("result %+v", result)
	}
	healthCheck := false
	if len(f.created) != 1 || !reflect.DeepEqual(f.created[0], models.ApplicationSpec{ProjectUUID: "project-1", EnvironmentName: "production", ServerUUID: "server-1",
		Name: "new-app", GitRepository: "https://github.com/owner/new-app", GitBranch: "main", BuildPack: "dockerfile", PortsExposes: "80", Source: SourcePublic, HealthCheckEnabled: &healthCheck}) {
		t.Fatalf("created %+v", f.created)
	}
	// A public repository was probed once per attempt and needed no source prompt.
	if !reflect.DeepEqual(f.probed, []string{"https://github.com/owner/new-app", "https://github.com/owner/new-app", "https://github.com/owner/new-app"}) || f.calls["github-apps"] != 0 || f.calls["keys"] != 0 {
		t.Fatalf("probed=%v calls=%v", f.probed, f.calls)
	}
	if f.calls["deploy"] != 0 || *credentials != 3 || *factories != 3 {
		t.Fatalf("calls=%v credentials=%d factories=%d", f.calls, *credentials, *factories)
	}
	data, err := os.ReadFile(result.Target.Root + "/coolship.toml")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Parse(data)
	if err != nil || parsed.Project != (config.Binding{Context: "home", Project: "Personal", Environment: "production", Application: "new-app", Root: "."}) {
		t.Fatalf("binding %+v: %v", parsed.Project, err)
	}
	// Every later command resolves the new application from that file.
	status, err := app.Status(context.Background(), Options{CWD: dir})
	if err != nil || status.Target.ApplicationUUID != "app-new-app" {
		t.Fatalf("status %+v err=%v", status, err)
	}
	// A linked directory is refused before any request, even with --yes.
	if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", Name: "another", Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "already binds") || f.calls["create-application"] != 1 {
		t.Fatalf("already linked: err=%v calls=%v", err, f.calls)
	}
}

func TestInitSettlesBuildPack(t *testing.T) {
	off := false
	web := ComposeDomain{Service: "web", Domain: "https://web.example.com"}
	for _, test := range []struct {
		name    string
		files   []string
		build   BuildOptions
		plan    InitPlan               // build fields only
		spec    models.ApplicationSpec // build fields only
		warning string
		refused string
	}{
		{"railpack by default", nil, BuildOptions{}, InitPlan{BuildPack: "railpack", Port: 3000}, models.ApplicationSpec{BuildPack: "railpack", PortsExposes: "3000"}, "", ""},
		{"dockerfile", []string{"Dockerfile"}, BuildOptions{}, InitPlan{BuildPack: "dockerfile", Port: 80, Dockerfile: "/Dockerfile"}, models.ApplicationSpec{BuildPack: "dockerfile", PortsExposes: "80", HealthCheckEnabled: &off}, "", ""},
		{"named dockerfile", []string{"Dockerfile", "deploy/Dockerfile.prod"}, BuildOptions{Dockerfile: "deploy/Dockerfile.prod", Port: 8080}, InitPlan{BuildPack: "dockerfile", Port: 8080, Dockerfile: "/deploy/Dockerfile.prod"}, models.ApplicationSpec{BuildPack: "dockerfile", PortsExposes: "8080", DockerfileLocation: "/deploy/Dockerfile.prod", HealthCheckEnabled: &off}, "", ""},
		{"compose before dockerfile", []string{"compose.yaml", "Dockerfile"}, BuildOptions{}, InitPlan{BuildPack: "dockercompose", ComposeFile: "/compose.yaml"}, models.ApplicationSpec{BuildPack: "dockercompose", PortsExposes: "80", DockerComposeLocation: "/compose.yaml"}, "No service has a domain", ""},
		{"compose in coolify's order", []string{"compose.yml", "docker-compose.yml"}, BuildOptions{}, InitPlan{BuildPack: "dockercompose", ComposeFile: "/docker-compose.yml"}, models.ApplicationSpec{BuildPack: "dockercompose", PortsExposes: "80", DockerComposeLocation: "/docker-compose.yml"}, "No service has a domain", ""},
		{"compose with domains", []string{"docker-compose.yaml", "deploy/stack.yml"}, BuildOptions{ComposeFile: "deploy/stack.yml", ComposeDomains: []ComposeDomain{web}}, InitPlan{BuildPack: "dockercompose", ComposeFile: "/deploy/stack.yml", ComposeDomains: []ComposeDomain{web}}, models.ApplicationSpec{BuildPack: "dockercompose", PortsExposes: "80", DockerComposeLocation: "/deploy/stack.yml", DockerComposeDomains: []models.ComposeDomain{{Name: "web", Domain: "https://web.example.com"}}}, "", ""},
		{"static page", []string{"index.html"}, BuildOptions{}, InitPlan{BuildPack: "static", Port: 80}, models.ApplicationSpec{BuildPack: "static", PortsExposes: "80"}, "", ""},
		{"page with a manifest builds", []string{"index.html", "package.json"}, BuildOptions{}, InitPlan{BuildPack: "railpack", Port: 3000}, models.ApplicationSpec{BuildPack: "railpack", PortsExposes: "3000"}, "", ""},
		{"static pack with publish dir", nil, BuildOptions{BuildPack: "static", PublishDirectory: "public/", Port: 8080}, InitPlan{BuildPack: "static", Port: 8080, PublishDirectory: "/public"}, models.ApplicationSpec{BuildPack: "static", PortsExposes: "8080", PublishDirectory: "/public"}, "", ""},
		{"static build", []string{"package.json"}, BuildOptions{Static: true}, InitPlan{BuildPack: "railpack", Port: 80, Static: true, PublishDirectory: "/dist"}, models.ApplicationSpec{BuildPack: "railpack", PortsExposes: "80", IsStatic: true, PublishDirectory: "/dist"}, "", ""},
		{"nixpacks with commands", nil, BuildOptions{BuildPack: "nixpacks", Static: true, PublishDirectory: "build", InstallCommand: "npm ci", BuildCommand: "npm run build", StartCommand: "npm start"}, InitPlan{BuildPack: "nixpacks", Port: 80, Static: true, PublishDirectory: "/build", InstallCommand: "npm ci", BuildCommand: "npm run build", StartCommand: "npm start"}, models.ApplicationSpec{BuildPack: "nixpacks", PortsExposes: "80", IsStatic: true, PublishDirectory: "/build", InstallCommand: "npm ci", BuildCommand: "npm run build", StartCommand: "npm start"}, "", ""},
		{"unknown pack", nil, BuildOptions{BuildPack: "buildpacks"}, InitPlan{}, models.ApplicationSpec{}, "", "--build-pack"},
		{"bad port", nil, BuildOptions{Port: 70000}, InitPlan{}, models.ApplicationSpec{}, "", "--port"},
		{"static twice", nil, BuildOptions{BuildPack: "static", Static: true}, InitPlan{}, models.ApplicationSpec{}, "", "--static is implied"},
		{"static dockerfile", []string{"Dockerfile"}, BuildOptions{Static: true}, InitPlan{}, models.ApplicationSpec{}, "", "--static applies"},
		{"publish dir without static", nil, BuildOptions{PublishDirectory: "dist"}, InitPlan{}, models.ApplicationSpec{}, "", "--publish-dir applies"},
		{"commands on dockerfile", []string{"Dockerfile"}, BuildOptions{StartCommand: "run"}, InitPlan{}, models.ApplicationSpec{}, "", "--start-command apply"},
		{"dockerfile flag on railpack", nil, BuildOptions{Dockerfile: "Dockerfile"}, InitPlan{}, models.ApplicationSpec{}, "", "--dockerfile applies"},
		{"missing dockerfile", []string{"Dockerfile"}, BuildOptions{Dockerfile: "Dockerfile.prod"}, InitPlan{}, models.ApplicationSpec{}, "", "no such file"},
		{"dockerfile outside the root", []string{"Dockerfile"}, BuildOptions{Dockerfile: "../Dockerfile"}, InitPlan{}, models.ApplicationSpec{}, "", "inside the application root"},
		{"compose flags on dockerfile", []string{"Dockerfile"}, BuildOptions{ComposeDomains: []ComposeDomain{web}}, InitPlan{}, models.ApplicationSpec{}, "", "--compose-domain apply"},
		{"compose without a file", nil, BuildOptions{BuildPack: "dockercompose"}, InitPlan{}, models.ApplicationSpec{}, "", "--compose-file PATH"},
		{"missing compose file", []string{"compose.yml"}, BuildOptions{ComposeFile: "stack.yml"}, InitPlan{}, models.ApplicationSpec{}, "", "no such file"},
		{"compose port", []string{"compose.yml"}, BuildOptions{Port: 80}, InitPlan{}, models.ApplicationSpec{}, "", "--port does not apply"},
		{"compose domain without scheme", []string{"compose.yml"}, BuildOptions{ComposeDomains: []ComposeDomain{{Service: "web", Domain: "web.example.com"}}}, InitPlan{}, models.ApplicationSpec{}, "", "http://"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newBackend()
			app, _, _ := testApp(f)
			dir := unlinkedDirectory(t)
			for _, name := range test.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			options := InitOptions{Options: Options{CWD: dir}, BuildOptions: test.build, Project: "Personal", Yes: true}
			result, err := app.Init(context.Background(), options, nil, nil, nil)
			if test.refused != "" {
				if !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), test.refused) || f.calls["projects"] != 0 || f.calls["create-application"] != 0 {
					t.Fatalf("err=%v calls=%v", err, f.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			plan := result.Plan
			got := InitPlan{BuildPack: plan.BuildPack, Port: plan.Port, Static: plan.Static, PublishDirectory: plan.PublishDirectory, Dockerfile: plan.Dockerfile,
				ComposeFile: plan.ComposeFile, ComposeDomains: plan.ComposeDomains, InstallCommand: plan.InstallCommand, BuildCommand: plan.BuildCommand, StartCommand: plan.StartCommand}
			if !reflect.DeepEqual(got, test.plan) {
				t.Fatalf("plan=%+v\nwant %+v", got, test.plan)
			}
			created := f.created[0]
			sent := models.ApplicationSpec{BuildPack: created.BuildPack, PortsExposes: created.PortsExposes, IsStatic: created.IsStatic, PublishDirectory: created.PublishDirectory,
				InstallCommand: created.InstallCommand, BuildCommand: created.BuildCommand, StartCommand: created.StartCommand, DockerfileLocation: created.DockerfileLocation,
				DockerComposeLocation: created.DockerComposeLocation, DockerComposeDomains: created.DockerComposeDomains, HealthCheckEnabled: created.HealthCheckEnabled}
			if !reflect.DeepEqual(sent, test.spec) {
				t.Fatalf("created=%+v\nwant %+v", sent, test.spec)
			}
			if test.warning == "" && len(result.Warnings) != 0 || test.warning != "" && (len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], test.warning)) {
				t.Fatalf("warnings=%q want %q", result.Warnings, test.warning)
			}
		})
	}
}

func TestInitRepositoryFromFlagsOrGit(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	dir := unlinkedDirectory(t)
	// Flags win and skip Git entirely; SSH forms are normalized.
	result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", Yes: true,
		Repository: "git@github.com:owner/flagged.git", Branch: "release"}, nil, nil, nil)
	if err != nil || result.Plan.Repository != "https://github.com/owner/flagged" || result.Plan.Branch != "release" || result.Plan.Name != "flagged" || len(f.inspected) != 0 {
		t.Fatalf("result=%+v err=%v inspected=%v", result, err, f.inspected)
	}
	// One flag fills in the other from Git.
	f.created = nil
	dir = unlinkedDirectory(t)
	result, err = app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", Yes: true, Branch: "develop", Name: "custom"}, nil, nil, nil)
	if err != nil || result.Plan.Repository != "https://github.com/owner/new-app" || result.Plan.Branch != "develop" || result.Plan.Name != "custom" || len(f.inspected) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// Git failures are input errors that name the flags.
	f.repositoryError = errors.New("HEAD is detached")
	if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--branch") {
		t.Fatalf("git failure: %v", err)
	}
	f.repositoryError = nil
	for _, bad := range []InitOptions{
		{Repository: "/srv/repo.git", Branch: "main"},
		{Repository: "https://github.com/owner/repo", Branch: "-x"},
		{Repository: "https://github.com/owner/repo", Branch: "main", Name: "has space"},
	} {
		bad.Options, bad.Project, bad.Yes = Options{CWD: unlinkedDirectory(t)}, "Personal", true
		if _, err := app.Init(context.Background(), bad, nil, nil, nil); !errors.Is(err, ErrInput) {
			t.Fatalf("%+v accepted: %v", bad, err)
		}
	}
	// Without an inspector, both flags are required.
	bare := New(Dependencies{NewBackend: func(auth.Credentials) (Backend, error) { return f, nil }})
	if _, err := bare.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("no inspector: %v", err)
	}
}

func TestInitSelectsProjectEnvironmentAndServer(t *testing.T) {
	t.Run("missing project needs --create-project", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		options := InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Fresh", Yes: true}
		if _, err := app.Init(context.Background(), options, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "--create-project") || f.calls["create-project"] != 0 {
			t.Fatalf("err=%v calls=%v", err, f.calls)
		}
		if _, err := app.Init(context.Background(), InitOptions{Options: options.Options, CreateProject: true, Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) {
			t.Fatalf("--create-project without --project: %v", err)
		}
	})
	t.Run("creates the project after confirmation", func(t *testing.T) {
		f := newBackend()
		// The fake lists env-1 for every project, so the new project has a production environment.
		app, _, _ := testApp(f)
		var plan InitPlan
		confirm := func(_ context.Context, p InitPlan) (bool, error) {
			plan = p
			if f.calls["create-project"] != 0 {
				t.Fatal("project created before confirmation")
			}
			return true, nil
		}
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Fresh", CreateProject: true}, nil, confirm, nil)
		if err != nil || !plan.NewProject || plan.Project != "Fresh" || f.calls["create-project"] != 1 || result.Target.Project != "Fresh" || f.created[0].ProjectUUID != "project-Fresh" {
			t.Fatalf("result=%+v plan=%+v err=%v calls=%v", result, plan, err, f.calls)
		}
	})
	t.Run("prompts among projects and servers", func(t *testing.T) {
		f := newBackend()
		f.projects = append(f.projects, models.Project{UUID: "project-2", Name: "Work"})
		f.servers = append(f.servers, models.Server{UUID: "server-2", Name: "Second", IsUsable: true, IsReachable: true},
			models.Server{UUID: "server-3", Name: "Down", IsUsable: false, IsReachable: false})
		app, _, _ := testApp(f)
		var kinds []string
		selector := func(_ context.Context, kind string, choices []Choice) (string, error) {
			kinds = append(kinds, kind)
			switch kind {
			case "project":
				return "project-1", nil
			case "server":
				if len(choices) != 2 {
					t.Fatalf("unusable server offered: %+v", choices)
				}
				return "server-2", nil
			}
			return "", errors.New("unexpected prompt " + kind)
		}
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Yes: true}, selector, nil, nil)
		if err != nil || !reflect.DeepEqual(kinds, []string{"project", "server"}) || result.Plan.Server != "Second" || f.created[0].ServerUUID != "server-2" {
			t.Fatalf("result=%+v kinds=%v err=%v", result, kinds, err)
		}
		// Noninteractive with several projects is an input error that names the flag.
		if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) {
			t.Fatalf("ambiguous project: %v", err)
		}
	})
	t.Run("named server and unusable warning", func(t *testing.T) {
		f := newBackend()
		f.servers = append(f.servers, models.Server{UUID: "server-3", Name: "Down"})
		app, _, _ := testApp(f)
		result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Server: "Down", Yes: true}, nil, nil, nil)
		if err != nil || f.created[0].ServerUUID != "server-3" || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Down") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Server: "Nowhere", Name: "other", Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "Master") {
			t.Fatalf("unknown server: %v", err)
		}
	})
	t.Run("environment and name conflicts", func(t *testing.T) {
		f := newBackend()
		app, _, _ := testApp(f)
		if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t), Environment: "staging"}, Project: "Personal", Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) || f.calls["create-application"] != 0 {
			t.Fatalf("missing environment: %v", err)
		}
		if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: unlinkedDirectory(t)}, Project: "Personal", Name: "api", Yes: true}, nil, nil, nil); !errors.Is(err, ErrInput) || !strings.Contains(err.Error(), "already exists") || f.calls["create-application"] != 0 {
			t.Fatalf("name conflict: %v", err)
		}
	})
}

func TestInitSurfacesServerRefusalAndDeploys(t *testing.T) {
	f := newBackend()
	f.createError = statusError{code: 422}
	app, _, _ := testApp(f)
	dir := unlinkedDirectory(t)
	_, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", Yes: true}, nil, nil, nil)
	if err == nil || !errors.Is(err, f.createError) || errors.Is(err, ErrInput) || !strings.Contains(err.Error(), `create application "new-app"`) {
		t.Fatalf("refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused creation wrote configuration")
	}
	// A transport failure says the application may exist.
	f.createError = errors.New("connection reset")
	if _, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", Yes: true}, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "may exist") && !strings.Contains(err.Error(), "the application exists") {
		t.Fatalf("uncertain: %v", err)
	}
	// --deploy submits through the same session and observes the deployment.
	f.createError = nil
	f.receipts = []models.DeploymentReceipt{{ResourceUUID: "app-new-app", DeploymentUUID: "deploy-1"}}
	resolved := f.calls["projects"]
	var events []string
	result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: dir}, Project: "Personal", Yes: true, Deploy: true}, nil, nil, func(e Event) error {
		events = append(events, e.Type+":"+e.Status)
		return nil
	})
	if err != nil || result.Deployment == nil || result.Deployment.DeploymentUUID != "deploy-1" || result.Deployment.Status != "finished" || f.lastDeploy.ApplicationUUID != "app-new-app" {
		t.Fatalf("result=%+v err=%v last=%+v", result, err, f.lastDeploy)
	}
	// init resolves once to select and once to verify its write, like link;
	// the deployment reuses that verified binding rather than resolving again.
	if !reflect.DeepEqual(events, []string{"deployment:queued", "deployment:finished"}) || f.calls["projects"] != resolved+2 {
		t.Fatalf("events=%v calls=%v", events, f.calls)
	}
}

func TestInitWritesNamedTargetWithBaseDirectory(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	root := unlinkedDirectory(t)
	if err := os.MkdirAll(filepath.Join(root, "apps/web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "apps/web/Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := app.Init(context.Background(), InitOptions{Options: Options{CWD: filepath.Join(root, "apps/web"), Target: "web"}, Project: "Personal", Yes: true}, nil, nil, nil)
	if err != nil || result.Plan.Target != "web" || result.Plan.Root != "apps/web" || result.Plan.BuildPack != "dockerfile" || result.Target.Target != "web" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if f.created[0].BaseDirectory != "/apps/web" || !reflect.DeepEqual(f.inspected, []string{filepath.Join(root, "apps/web")}) {
		t.Fatalf("created=%+v inspected=%v", f.created[0], f.inspected)
	}
	data, _ := os.ReadFile(filepath.Join(root, "coolship.toml"))
	parsed, err := config.Parse(data)
	if err != nil || parsed.Apps["web"].Root != "apps/web" || parsed.Apps["web"].Application != "new-app" {
		t.Fatalf("configuration %s: %v", data, err)
	}
}
