package resolver

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
)

type fakeCatalog struct {
	projects     []models.Project
	environments map[string][]models.Environment
	details      map[string]models.Environment
	applications map[string]models.Application
	calls        []string
	failAt       string
	err          error
}

func (f *fakeCatalog) record(ctx context.Context, call string) error {
	f.calls = append(f.calls, call)
	if err := ctx.Err(); err != nil {
		return err
	}
	if call == f.failAt {
		return f.err
	}
	return nil
}

func (f *fakeCatalog) ListProjects(ctx context.Context) ([]models.Project, error) {
	err := f.record(ctx, "projects")
	return f.projects, err
}

func (f *fakeCatalog) ListEnvironments(ctx context.Context, uuid string) ([]models.Environment, error) {
	err := f.record(ctx, "environments:"+uuid)
	return f.environments[uuid], err
}

func (f *fakeCatalog) GetEnvironment(ctx context.Context, parent, uuid string) (models.Environment, error) {
	err := f.record(ctx, "environment:"+parent+"/"+uuid)
	return f.details[parent+"/"+uuid], err
}

func (f *fakeCatalog) GetApplication(ctx context.Context, uuid string) (models.Application, error) {
	err := f.record(ctx, "application:"+uuid)
	return f.applications[uuid], err
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

func TestMissingPinsNeverFallbackToMatchingNames(t *testing.T) {
	for _, test := range []struct {
		resource string
		change   func(*project.Target)
	}{
		{"project", func(target *project.Target) { target.Binding.ProjectUUID = "deleted" }},
		{"environment", func(target *project.Target) { target.Binding.EnvironmentUUID = "deleted" }},
		{"application", func(target *project.Target) { target.Binding.ApplicationUUID = "deleted" }},
	} {
		t.Run(test.resource, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.change(&target)
			_, err := Resolve(context.Background(), catalog, target)
			var missing *MissingError
			if !errors.As(err, &missing) || missing.Resource != test.resource || missing.UUID != "deleted" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestPinsMustBelongToSelectedParents(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*project.Target)
	}{
		{"environment in another project", func(target *project.Target) { target.Binding.EnvironmentUUID = "e3" }},
		{"application in another environment", func(target *project.Target) { target.Binding.ApplicationUUID = "a2" }},
		{"application in another project", func(target *project.Target) { target.Binding.ApplicationUUID = "a3" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog, target := catalogFixture()
			test.change(&target)
			_, err := Resolve(context.Background(), catalog, target)
			var missing *MissingError
			if !errors.As(err, &missing) {
				t.Fatalf("out-of-scope pin error = %v", err)
			}
			for _, call := range catalog.calls {
				if strings.HasPrefix(call, "application:") {
					t.Errorf("inspected an out-of-scope application: %v", catalog.calls)
				}
			}
		})
	}
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
