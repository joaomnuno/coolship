package resolver

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
)

type fakeCatalog struct {
	projects     []models.Project
	environments map[string][]models.Environment
	details      map[string]models.Environment
	applications map[string]models.Application
	mu           sync.Mutex
	calls        []string
	failAt       string
	err          error
}

// notFoundError is how the client reports a 404, seen through the same
// interface the resolver checks.
type notFoundError struct{}

func (notFoundError) Error() string       { return "HTTP 404 Not Found" }
func (notFoundError) HTTPStatusCode() int { return 404 }

func (f *fakeCatalog) record(ctx context.Context, call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if err := ctx.Err(); err != nil {
		return err
	}
	if call == f.failAt {
		return f.err
	}
	return nil
}

// recorded is the calls made so far, sorted: reads that run concurrently
// have no order.
func (f *fakeCatalog) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Sorted(slices.Values(f.calls))
}

func (f *fakeCatalog) ListProjects(ctx context.Context) ([]models.Project, error) {
	err := f.record(ctx, "projects")
	return f.projects, err
}

// GetProject answers like Coolify: the project with its environments, or a
// 404 for a UUID the instance does not hold.
func (f *fakeCatalog) GetProject(ctx context.Context, uuid string) (models.Project, error) {
	if err := f.record(ctx, "project:"+uuid); err != nil {
		return models.Project{}, err
	}
	for _, value := range f.projects {
		if value.UUID == uuid {
			value.Environments = f.environments[uuid]
			return value, nil
		}
	}
	return models.Project{}, notFoundError{}
}

func (f *fakeCatalog) ListEnvironments(ctx context.Context, uuid string) ([]models.Environment, error) {
	err := f.record(ctx, "environments:"+uuid)
	return f.environments[uuid], err
}

func (f *fakeCatalog) GetEnvironment(ctx context.Context, parent, uuid string) (models.Environment, error) {
	if err := f.record(ctx, "environment:"+parent+"/"+uuid); err != nil {
		return models.Environment{}, err
	}
	value, ok := f.details[parent+"/"+uuid]
	if !ok {
		return models.Environment{}, notFoundError{}
	}
	return value, nil
}

func (f *fakeCatalog) GetApplication(ctx context.Context, uuid string) (models.Application, error) {
	if err := f.record(ctx, "application:"+uuid); err != nil {
		return models.Application{}, err
	}
	value, ok := f.applications[uuid]
	if !ok {
		return models.Application{}, notFoundError{}
	}
	return value, nil
}

func catalogFixture() (*fakeCatalog, project.Target) {
	return &fakeCatalog{
		projects: []models.Project{{UUID: "p2", Name: "Other"}, {UUID: "p1", Name: "Personal"}},
		environments: map[string][]models.Environment{
			"p1": {{UUID: "e1", Name: "production"}, {UUID: "e2", Name: "staging"}},
			"p2": {{UUID: "e3", Name: "production"}},
		},
		details: map[string]models.Environment{
			"p1/e1": {UUID: "e1", Name: "production", Applications: []models.Application{{UUID: "a1", Name: "web"}}},
			"p1/e2": {UUID: "e2", Name: "staging", Applications: []models.Application{{UUID: "a2", Name: "web"}}},
			"p2/e3": {UUID: "e3", Name: "production", Applications: []models.Application{{UUID: "a3", Name: "web"}}},
		},
		applications: map[string]models.Application{
			"a1": {UUID: "a1", Name: "web", Status: "running:healthy"},
			"a2": {UUID: "a2", Name: "web", Status: "exited"},
			"a3": {UUID: "a3", Name: "web", Status: "running"},
		},
	}, project.Target{Key: "default", Binding: config.Binding{Project: "Personal", Environment: "production", Application: "web"}}
}

func TestResolveUsesExactScopedNames(t *testing.T) {
	catalog, target := catalogFixture()
	result, err := Resolve(context.Background(), catalog, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Project.UUID != "p1" || result.Environment.UUID != "e1" || result.Application.UUID != "a1" || result.Application.Status != "running:healthy" || len(result.Warnings) != 0 {
		t.Fatalf("binding = %#v", result)
	}
	wantCalls := []string{"projects", "environments:p1", "environment:p1/e1", "application:a1"}
	if !reflect.DeepEqual(catalog.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", catalog.calls, wantCalls)
	}
	for _, change := range []func(*project.Target){
		func(target *project.Target) { target.Binding.Project = "personal" },
		func(target *project.Target) { target.Binding.Environment = "Production" },
		func(target *project.Target) { target.Binding.Application = "WEB" },
		func(target *project.Target) { target.Binding.Application = "a1" },
	} {
		catalog, target := catalogFixture()
		change(&target)
		_, err := Resolve(context.Background(), catalog, target)
		var missing *MissingError
		if !errors.As(err, &missing) {
			t.Errorf("case-sensitive/exact name lookup returned %v", err)
		}
	}
}

func TestAmbiguityNeverSelectsFirstMatch(t *testing.T) {
	for _, test := range []struct {
		resource string
		mutate   func(*fakeCatalog)
		calls    int
	}{
		{"project", func(f *fakeCatalog) { f.projects = append(f.projects, models.Project{UUID: "p3", Name: "Personal"}) }, 1},
		{"environment", func(f *fakeCatalog) {
			f.environments["p1"] = append(f.environments["p1"], models.Environment{UUID: "e4", Name: "production"})
		}, 2},
		{"application", func(f *fakeCatalog) {
			environment := f.details["p1/e1"]
			environment.Applications = append(environment.Applications, models.Application{UUID: "a4", Name: "web"})
			f.details["p1/e1"] = environment
		}, 3},
	} {
		t.Run(test.resource, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.mutate(catalog)
			_, err := Resolve(context.Background(), catalog, target)
			var ambiguous *AmbiguousError
			if !errors.As(err, &ambiguous) || ambiguous.Resource != test.resource || len(ambiguous.Choices) != 2 {
				t.Fatalf("error = %v", err)
			}
			if len(catalog.calls) != test.calls {
				t.Errorf("resolution continued after ambiguity: %v", catalog.calls)
			}
			if !strings.Contains(err.Error(), "explicit UUID") {
				t.Errorf("missing recovery guidance: %v", err)
			}
		})
	}
}

func TestPinnedIDsAreAuthoritativeAndWarnOnRename(t *testing.T) {
	catalog, target := catalogFixture()
	target.Binding.ProjectUUID = "p1"
	target.Binding.EnvironmentUUID = "e1"
	target.Binding.ApplicationUUID = "a1"
	target.Binding.Project = "Old project"
	target.Binding.Environment = "old environment"
	target.Binding.Application = "old application"
	result, err := Resolve(context.Background(), catalog, target)
	if err != nil || result.Project.UUID != "p1" || result.Environment.UUID != "e1" || result.Application.UUID != "a1" || len(result.Warnings) != 3 {
		t.Fatalf("pinned binding = %#v, error = %v", result, err)
	}
	for _, warning := range result.Warnings {
		if !strings.Contains(warning, "remains pinned") {
			t.Errorf("unexpected warning %q", warning)
		}
	}
	// UUID-only bindings do not manufacture label drift.
	target.Binding.Project, target.Binding.Environment, target.Binding.Application = "", "", ""
	result, err = Resolve(context.Background(), catalog, target)
	if err != nil || len(result.Warnings) != 0 {
		t.Errorf("UUID-only binding = %#v, error = %v", result, err)
	}
}

// A pin that is found is used without any list read: the pin hit.
func TestPinHitReadsNoLists(t *testing.T) {
	catalog, target := catalogFixture()
	target.Binding.ProjectUUID, target.Binding.EnvironmentUUID, target.Binding.ApplicationUUID = "p1", "e1", "a1"
	result, err := Resolve(context.Background(), catalog, target)
	if err != nil || result.Application.UUID != "a1" || len(result.Warnings) != 0 {
		t.Fatalf("binding = %#v, error = %v", result, err)
	}
	for _, call := range catalog.recorded() {
		if call == "projects" || strings.HasPrefix(call, "environments:") {
			t.Errorf("pin hit listed %q", call)
		}
	}
}

func TestPinMissWithNameHitFallsBackAndWarnsOnce(t *testing.T) {
	for _, test := range []struct {
		resource string
		change   func(*project.Target)
		warning  string
	}{
		{"project", func(target *project.Target) { target.Binding.ProjectUUID = "deleted" },
			`coolship.toml pins project deleted that no longer exists; found "Personal" by name. Run coolship link to refresh.`},
		{"environment", func(target *project.Target) { target.Binding.EnvironmentUUID = "deleted" },
			`coolship.toml pins environment deleted that no longer exists; found "production" by name. Run coolship link to refresh.`},
		{"application", func(target *project.Target) { target.Binding.ApplicationUUID = "deleted" },
			`coolship.toml pins application deleted that no longer exists; found "web" by name. Run coolship link to refresh.`},
		// A pin that exists but outside its bound parent is missing there too.
		{"environment in another project", func(target *project.Target) {
			target.Binding.ProjectUUID, target.Binding.EnvironmentUUID = "p1", "e3"
		}, `coolship.toml pins environment e3 that no longer exists; found "production" by name. Run coolship link to refresh.`},
		{"application in another environment", func(target *project.Target) { target.Binding.ApplicationUUID = "a2" },
			`coolship.toml pins application a2 that no longer exists; found "web" by name. Run coolship link to refresh.`},
		// A replaced project takes its environment and application with it:
		// one warning names all three.
		{"everything", func(target *project.Target) {
			target.Binding.ProjectUUID, target.Binding.EnvironmentUUID, target.Binding.ApplicationUUID = "deleted-p", "deleted-e", "deleted-a"
		}, `coolship.toml pins project deleted-p, environment deleted-e, application deleted-a that no longer exist; found "Personal", "production", "web" by name. Run coolship link to refresh.`},
	} {
		t.Run(test.resource, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.change(&target)
			var alongside []string
			result, err := ResolveAlongside(context.Background(), catalog, target, func(uuid string) { alongside = append(alongside, uuid) })
			if err != nil || result.Project.UUID != "p1" || result.Environment.UUID != "e1" || result.Application.UUID != "a1" || result.Application.Status != "running:healthy" {
				t.Fatalf("binding = %#v, error = %v", result, err)
			}
			if !reflect.DeepEqual(result.Warnings, []string{test.warning}) {
				t.Errorf("warnings = %q, want %q", result.Warnings, test.warning)
			}
			if len(alongside) == 0 || alongside[len(alongside)-1] != "a1" {
				t.Errorf("alongside = %v, want the last call with a1", alongside)
			}
		})
	}
}

func TestPinMissWithNameMissIsAnError(t *testing.T) {
	for _, test := range []struct {
		resource string
		change   func(*project.Target)
	}{
		{"project", func(target *project.Target) { target.Binding.ProjectUUID, target.Binding.Project = "deleted", "Gone" }},
		{"environment", func(target *project.Target) {
			target.Binding.EnvironmentUUID, target.Binding.Environment = "deleted", "gone"
		}},
		{"application", func(target *project.Target) {
			target.Binding.ApplicationUUID, target.Binding.Application = "deleted", "gone"
		}},
	} {
		t.Run(test.resource, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.change(&target)
			result, err := Resolve(context.Background(), catalog, target)
			var missing *MissingError
			if !errors.As(err, &missing) || missing.Resource != test.resource || missing.UUID != "deleted" || !missing.Fallback || result.Application.UUID != "" {
				t.Fatalf("binding = %#v, error = %v", result, err)
			}
			if message := err.Error(); !strings.Contains(message, `pinned UUID "deleted" no longer exists`) || !strings.Contains(message, "named") {
				t.Errorf("message does not name the pin and the name: %q", message)
			}
		})
	}
	// An ambiguous name stands in for nothing either.
	catalog, target := catalogFixture()
	catalog.projects = append(catalog.projects, models.Project{UUID: "p9", Name: "Personal"})
	target.Binding.ProjectUUID = "deleted"
	var ambiguous *AmbiguousError
	if _, err := Resolve(context.Background(), catalog, target); !errors.As(err, &ambiguous) || ambiguous.Resource != "project" {
		t.Fatalf("error = %v, want the project name ambiguous", err)
	}
}

// A pin without a name has nothing to fall back to, in or out of scope.
func TestPinMissWithoutNameIsAnError(t *testing.T) {
	for _, test := range []struct {
		name     string
		resource string
		change   func(*project.Target)
	}{
		{"project", "project", func(target *project.Target) { target.Binding.ProjectUUID, target.Binding.Project = "deleted", "" }},
		{"environment", "environment", func(target *project.Target) {
			target.Binding.EnvironmentUUID, target.Binding.Environment = "deleted", ""
		}},
		{"application", "application", func(target *project.Target) {
			target.Binding.ApplicationUUID, target.Binding.Application = "deleted", ""
		}},
		{"environment in another project", "environment", func(target *project.Target) {
			target.Binding.EnvironmentUUID, target.Binding.Environment = "e3", ""
		}},
		{"application in another environment", "application", func(target *project.Target) {
			target.Binding.ApplicationUUID, target.Binding.Application = "a2", ""
		}},
		{"application in another project, all pinned", "application", func(target *project.Target) {
			target.Binding = config.Binding{ProjectUUID: "p1", EnvironmentUUID: "e1", ApplicationUUID: "a3"}
		}},
		{"environment in another project, all pinned", "environment", func(target *project.Target) {
			target.Binding = config.Binding{ProjectUUID: "p1", EnvironmentUUID: "e3", ApplicationUUID: "a3"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.change(&target)
			result, err := Resolve(context.Background(), catalog, target)
			var missing *MissingError
			if !errors.As(err, &missing) || missing.Resource != test.resource || missing.Fallback || result.Application.UUID != "" {
				t.Fatalf("binding = %#v, error = %v", result, err)
			}
		})
	}
}

func TestFullyPinnedBindingReadsDirectlyAndConcurrently(t *testing.T) {
	catalog, target := catalogFixture()
	target.Binding.ProjectUUID, target.Binding.EnvironmentUUID, target.Binding.ApplicationUUID = "p1", "e1", "a1"
	// Each read waits until all three have started; sequential reads would
	// never get there and time out.
	gate := &barrier{Catalog: catalog, want: 3, arrived: make(chan struct{}, 3)}
	var alongside []string
	result, err := ResolveAlongside(context.Background(), gate, target, func(uuid string) { alongside = append(alongside, uuid) })
	if err != nil || result.Project.Name != "Personal" || result.Environment.UUID != "e1" || result.Application.Status != "running:healthy" {
		t.Fatalf("binding = %#v, error = %v", result, err)
	}
	if result.Project.Environments != nil {
		t.Errorf("binding kept the embedded environments: %#v", result.Project)
	}
	want := []string{"application:a1", "environment:p1/e1", "project:p1"}
	if got := catalog.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v (no list reads)", got, want)
	}
	if !reflect.DeepEqual(alongside, []string{"a1"}) {
		t.Errorf("alongside = %v, want one call with a1", alongside)
	}
}

func TestPinnedProjectUsesItsEmbeddedEnvironments(t *testing.T) {
	catalog, target := catalogFixture()
	target.Binding.ProjectUUID = "p1"
	result, err := Resolve(context.Background(), catalog, target)
	if err != nil || result.Environment.UUID != "e1" || result.Application.UUID != "a1" {
		t.Fatalf("binding = %#v, error = %v", result, err)
	}
	want := []string{"application:a1", "environment:p1/e1", "project:p1"}
	if got := catalog.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	// Without an embedded list the environments are listed as before.
	catalog, target = catalogFixture()
	target.Binding.ProjectUUID = "p1"
	withoutList := &withoutEmbeddedEnvironments{catalog}
	if _, err := Resolve(context.Background(), withoutList, target); err != nil {
		t.Fatal(err)
	}
	if got := catalog.recorded(); !slices.Contains(got, "environments:p1") {
		t.Errorf("calls = %v, want the environment list", got)
	}
}

func TestNamedBindingStartsAlongsideWithTheApplicationRead(t *testing.T) {
	catalog, target := catalogFixture()
	var alongside []string
	if _, err := ResolveAlongside(context.Background(), catalog, target, func(uuid string) { alongside = append(alongside, uuid) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(alongside, []string{"a1"}) {
		t.Errorf("alongside = %v", alongside)
	}
	catalog, target = catalogFixture()
	target.Binding.Environment = "missing"
	alongside = nil
	if _, err := ResolveAlongside(context.Background(), catalog, target, func(uuid string) { alongside = append(alongside, uuid) }); err == nil || alongside != nil {
		t.Errorf("alongside ran for a failed resolution: %v, %v", alongside, err)
	}
}

func TestPinnedReadFailuresRemainFailures(t *testing.T) {
	cause := errors.New("read failed")
	for _, stage := range []string{"project:p1", "environment:p1/e1", "application:a1"} {
		t.Run(stage, func(t *testing.T) {
			catalog, target := catalogFixture()
			target.Binding.ProjectUUID, target.Binding.EnvironmentUUID, target.Binding.ApplicationUUID = "p1", "e1", "a1"
			catalog.failAt, catalog.err = stage, cause
			_, err := Resolve(context.Background(), catalog, target)
			var missing *MissingError
			if !errors.Is(err, cause) || errors.As(err, &missing) {
				t.Fatalf("read failure = %v", err)
			}
		})
	}
	// The parent's failure wins over its children's, whatever finishes first.
	catalog, target := catalogFixture()
	target.Binding = config.Binding{ProjectUUID: "gone", EnvironmentUUID: "gone", ApplicationUUID: "gone"}
	_, err := Resolve(context.Background(), catalog, target)
	var missing *MissingError
	if !errors.As(err, &missing) || missing.Resource != "project" {
		t.Fatalf("error = %v, want the project missing", err)
	}
}

// barrier holds every read until want reads have started.
type barrier struct {
	Catalog
	want    int
	arrived chan struct{}
	mu      sync.Mutex
	started int
	open    chan struct{}
}

func (b *barrier) enter(ctx context.Context) error {
	b.mu.Lock()
	if b.open == nil {
		b.open = make(chan struct{})
	}
	b.started++
	if b.started == b.want {
		close(b.open)
	}
	open := b.open
	b.mu.Unlock()
	select {
	case <-open:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return errors.New("reads did not run concurrently")
	}
}

func (b *barrier) GetProject(ctx context.Context, uuid string) (models.Project, error) {
	if err := b.enter(ctx); err != nil {
		return models.Project{}, err
	}
	return b.Catalog.GetProject(ctx, uuid)
}

func (b *barrier) GetEnvironment(ctx context.Context, parent, uuid string) (models.Environment, error) {
	if err := b.enter(ctx); err != nil {
		return models.Environment{}, err
	}
	return b.Catalog.GetEnvironment(ctx, parent, uuid)
}

func (b *barrier) GetApplication(ctx context.Context, uuid string) (models.Application, error) {
	if err := b.enter(ctx); err != nil {
		return models.Application{}, err
	}
	return b.Catalog.GetApplication(ctx, uuid)
}

// withoutEmbeddedEnvironments is a server whose project read omits the
// environments.
type withoutEmbeddedEnvironments struct{ Catalog }

func (w *withoutEmbeddedEnvironments) GetProject(ctx context.Context, uuid string) (models.Project, error) {
	value, err := w.Catalog.GetProject(ctx, uuid)
	value.Environments = nil
	return value, err
}

func TestResponsesMustIdentifySelectedResources(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*fakeCatalog)
	}{
		{"environment UUID differs", func(f *fakeCatalog) {
			value := f.details["p1/e1"]
			value.UUID = "e2"
			f.details["p1/e1"] = value
		}},
		{"environment renamed during lookup", func(f *fakeCatalog) {
			value := f.details["p1/e1"]
			value.Name = "staging"
			f.details["p1/e1"] = value
		}},
		{"application UUID differs", func(f *fakeCatalog) {
			value := f.applications["a1"]
			value.UUID = "a2"
			f.applications["a1"] = value
		}},
		{"application renamed during lookup", func(f *fakeCatalog) {
			value := f.applications["a1"]
			value.Name = "different"
			f.applications["a1"] = value
		}},
		{"candidate UUID absent", func(f *fakeCatalog) { f.projects[1].UUID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.mutate(catalog)
			_, err := Resolve(context.Background(), catalog, target)
			var identityError *IdentityError
			if !errors.As(err, &identityError) {
				t.Fatalf("identity error = %v", err)
			}
		})
	}
}

func TestReadFailuresRemainFailures(t *testing.T) {
	cause := errors.New("read failed")
	for _, stage := range []string{"projects", "environments:p1", "environment:p1/e1", "application:a1"} {
		t.Run(stage, func(t *testing.T) {
			catalog, target := catalogFixture()
			catalog.failAt, catalog.err = stage, cause
			_, err := Resolve(context.Background(), catalog, target)
			var missing *MissingError
			if !errors.Is(err, cause) || errors.As(err, &missing) {
				t.Fatalf("read failure = %v", err)
			}
		})
	}
	catalog, target := catalogFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Resolve(ctx, catalog, target)
	if !errors.Is(err, context.Canceled) || len(catalog.calls) != 0 {
		t.Errorf("canceled resolution: calls = %v, error = %v", catalog.calls, err)
	}
}

func TestLinkSelectionUsesSameRules(t *testing.T) {
	projects := []models.Project{{UUID: "p1", Name: "same"}, {UUID: "p2", Name: "same"}}
	if _, err := SelectProject(projects, "same", ""); err == nil {
		t.Error("project name ambiguity accepted")
	}
	if value, err := SelectProject(projects, "old name", "p2"); err != nil || value.UUID != "p2" {
		t.Errorf("project pin = %#v, %v", value, err)
	}
	environments := []models.Environment{{UUID: "e1", Name: "same"}, {UUID: "e2", Name: "same"}}
	if _, err := SelectEnvironment(environments, "same", ""); err == nil {
		t.Error("environment name ambiguity accepted")
	}
	if value, err := SelectEnvironment(environments, "old name", "e2"); err != nil || value.UUID != "e2" {
		t.Errorf("environment pin = %#v, %v", value, err)
	}
	applications := []models.Application{{UUID: "a1", Name: "same"}, {UUID: "a2", Name: "same"}}
	if _, err := SelectApplication(applications, "same", ""); err == nil {
		t.Error("application name ambiguity accepted")
	}
	if value, err := SelectApplication(applications, "old name", "a2"); err != nil || value.UUID != "a2" {
		t.Errorf("application pin = %#v, %v", value, err)
	}
	if _, err := SelectApplication(append(applications, applications[1]), "", "a2"); err == nil {
		t.Error("duplicate pinned identities accepted")
	}
}
