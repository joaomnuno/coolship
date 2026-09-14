package resolver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
)

// Catalog is owned by the resolver, and contains only the reads needed to
// establish an application binding in its project and environment.
type Catalog interface {
	ListProjects(context.Context) ([]models.Project, error)
	GetProject(context.Context, string) (models.Project, error)
	ListEnvironments(context.Context, string) ([]models.Environment, error)
	GetEnvironment(context.Context, string, string) (models.Environment, error)
	GetApplication(context.Context, string) (models.Application, error)
}

type Binding struct {
	Project     models.Project
	Environment models.Environment
	Application models.Application
	Warnings    []string
}

// Alongside is a read that needs only the application's UUID, such as the
// deployment history status shows. Resolve calls it once, as soon as the UUID
// is known, so the caller can run it concurrently with the application read.
// It must not block; the caller owns its goroutine and discards its result
// when resolution fails.
type Alongside func(applicationUUID string)

// Resolve validates each child against its selected parent before inspecting the
// application. A missing pinned resource never falls back to a matching name.
func Resolve(ctx context.Context, catalog Catalog, target project.Target) (Binding, error) {
	return ResolveAlongside(ctx, catalog, target, nil)
}

// ResolveAlongside is Resolve with a read started beside the application read.
//
// A pinned UUID is read directly instead of being found in its parent's list:
// a pinned project with GET /projects/{uuid}, a pinned environment with the
// project-scoped GET /projects/{uuid}/{environment_uuid}, and a pinned
// application with GET /applications/{uuid}. Each read starts as soon as its
// UUID is known, so a fully pinned binding costs one round of concurrent
// requests. Results are still examined parent first, so the first failure in
// the hierarchy is the one reported, and membership is checked exactly as
// before: the environment read is scoped to the project, and the application
// must be listed in the environment.
func ResolveAlongside(ctx context.Context, catalog Catalog, target project.Target, alongside Alongside) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	// Reads still running when resolution returns, which only happens on
	// failure, are cancelled rather than left to finish.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	selectors := target.Binding

	var projectRead *future[models.Project]
	var projectList *future[[]models.Project]
	if selectors.ProjectUUID != "" {
		projectRead = start(ctx, func(ctx context.Context) (models.Project, error) {
			return catalog.GetProject(ctx, selectors.ProjectUUID)
		})
	} else {
		projectList = start(ctx, catalog.ListProjects)
	}
	var environmentRead *future[models.Environment]
	if selectors.ProjectUUID != "" && selectors.EnvironmentUUID != "" {
		environmentRead = start(ctx, func(ctx context.Context) (models.Environment, error) {
			return catalog.GetEnvironment(ctx, selectors.ProjectUUID, selectors.EnvironmentUUID)
		})
	}
	var applicationRead *future[models.Application]
	startApplication := func(uuid string) {
		applicationRead = start(ctx, func(ctx context.Context) (models.Application, error) { return catalog.GetApplication(ctx, uuid) })
		if alongside != nil {
			alongside(uuid)
		}
	}
	if selectors.ApplicationUUID != "" {
		startApplication(selectors.ApplicationUUID)
	}

	selectedProject, err := selectProject(projectRead, projectList, selectors.Project, selectors.ProjectUUID)
	if err != nil {
		return Binding{}, err
	}
	projectScope := fmt.Sprintf("project %q", selectedProject.Name)
	var selectedEnvironment models.Environment
	pinnedEnvironment := selectors.EnvironmentUUID != ""
	switch {
	case pinnedEnvironment:
		if environmentRead == nil {
			environmentRead = start(ctx, func(ctx context.Context) (models.Environment, error) {
				return catalog.GetEnvironment(ctx, selectedProject.UUID, selectors.EnvironmentUUID)
			})
		}
		selectedEnvironment = models.Environment{UUID: selectors.EnvironmentUUID, Name: selectors.Environment}
	default:
		environments := selectedProject.Environments
		if environments == nil {
			environments, err = catalog.ListEnvironments(ctx, selectedProject.UUID)
			if err != nil {
				return Binding{}, fmt.Errorf("list environments for project %q: %w", selectedProject.Name, err)
			}
		}
		selectedEnvironment, err = choose(environments, environmentChoice, "environment", projectScope, selectors.Environment, "")
		if err != nil {
			return Binding{}, err
		}
		environmentRead = start(ctx, func(ctx context.Context) (models.Environment, error) {
			return catalog.GetEnvironment(ctx, selectedProject.UUID, selectedEnvironment.UUID)
		})
	}
	environment, err := environmentRead.wait()
	if err != nil {
		if pinnedEnvironment && notFound(err) {
			return Binding{}, &MissingError{Resource: "environment", Scope: projectScope, Name: selectors.Environment, UUID: selectors.EnvironmentUUID}
		}
		return Binding{}, fmt.Errorf("read selected environment: %w", err)
	}
	if err := verifyIdentity("environment", environmentChoice(selectedEnvironment), environmentChoice(environment), pinnedEnvironment); err != nil {
		return Binding{}, err
	}
	selectedApplication, err := choose(environment.Applications, applicationChoice, "application", fmt.Sprintf("environment %q of project %q", environment.Name, selectedProject.Name), selectors.Application, selectors.ApplicationUUID)
	if err != nil {
		return Binding{}, err
	}
	if applicationRead == nil {
		startApplication(selectedApplication.UUID)
	}
	application, err := applicationRead.wait()
	if err != nil {
		return Binding{}, fmt.Errorf("read selected application: %w", err)
	}
	if err := verifyIdentity("application", applicationChoice(selectedApplication), applicationChoice(application), selectors.ApplicationUUID != ""); err != nil {
		return Binding{}, err
	}
	selectedProject.Environments = nil
	result := Binding{Project: selectedProject, Environment: environment, Application: application}
	for _, label := range []struct{ resource, configured, observed, uuid string }{
		{"project", selectors.Project, selectedProject.Name, selectors.ProjectUUID},
		{"environment", selectors.Environment, environment.Name, selectors.EnvironmentUUID},
		{"application", selectors.Application, application.Name, selectors.ApplicationUUID},
	} {
		if label.uuid != "" && label.configured != "" && label.configured != label.observed {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s %q is now named %q; UUID %q remains pinned", label.resource, label.configured, label.observed, label.uuid))
		}
	}
	return result, nil
}

// selectProject finishes whichever project read Resolve started: the direct
// read of a pinned UUID, where a 404 is the pin going missing, or the list.
func selectProject(read *future[models.Project], list *future[[]models.Project], name, uuid string) (models.Project, error) {
	const scope = "the selected instance"
	if read == nil {
		projects, err := list.wait()
		if err != nil {
			return models.Project{}, fmt.Errorf("list projects for binding: %w", err)
		}
		return choose(projects, projectChoice, "project", scope, name, uuid)
	}
	value, err := read.wait()
	if err != nil {
		if notFound(err) {
			return models.Project{}, &MissingError{Resource: "project", Scope: scope, Name: name, UUID: uuid}
		}
		return models.Project{}, fmt.Errorf("read project for binding: %w", err)
	}
	if err := verifyIdentity("project", Choice{UUID: uuid, Name: name}, projectChoice(value), true); err != nil {
		return models.Project{}, err
	}
	return value, nil
}

// notFound reports an HTTP 404 without importing the client: Coolify answers
// 404 for a UUID that does not exist or belongs to another team, the same
// cases in which a list would not contain it.
func notFound(err error) bool {
	var status interface{ HTTPStatusCode() int }
	return errors.As(err, &status) && status.HTTPStatusCode() == http.StatusNotFound
}

// future is one read running in its own goroutine.
type future[T any] struct {
	done  chan struct{}
	value T
	err   error
}

func start[T any](ctx context.Context, read func(context.Context) (T, error)) *future[T] {
	f := &future[T]{done: make(chan struct{})}
	go func() {
		defer close(f.done)
		f.value, f.err = read(ctx)
	}()
	return f
}

func (f *future[T]) wait() (T, error) {
	<-f.done
	return f.value, f.err
}

// SelectProject applies the same identity rules to link candidates as Resolve.
func SelectProject(values []models.Project, name, uuid string) (models.Project, error) {
	return choose(values, projectChoice, "project", "the selected instance", name, uuid)
}

// SelectEnvironment expects candidates already scoped to one project.
func SelectEnvironment(values []models.Environment, name, uuid string) (models.Environment, error) {
	return choose(values, environmentChoice, "environment", "the selected project", name, uuid)
}

// SelectApplication expects candidates already scoped to one environment.
func SelectApplication(values []models.Application, name, uuid string) (models.Application, error) {
	return choose(values, applicationChoice, "application", "the selected environment", name, uuid)
}

func projectChoice(value models.Project) Choice { return Choice{UUID: value.UUID, Name: value.Name} }
func environmentChoice(value models.Environment) Choice {
	return Choice{UUID: value.UUID, Name: value.Name}
}
func applicationChoice(value models.Application) Choice {
	return Choice{UUID: value.UUID, Name: value.Name}
}

func choose[T any](values []T, identity func(T) Choice, resource, scope, name, uuid string) (T, error) {
	var zero T
	var selected T
	var matches []Choice
	for _, value := range values {
		choice := identity(value)
		if choice.UUID == "" {
			return zero, &IdentityError{Resource: resource, Reason: "candidate has no UUID"}
		}
		if (uuid != "" && choice.UUID == uuid) || (uuid == "" && name != "" && choice.Name == name) {
			selected = value
			matches = append(matches, choice)
		}
	}
	switch len(matches) {
	case 0:
		return zero, &MissingError{Resource: resource, Scope: scope, Name: name, UUID: uuid}
	case 1:
		return selected, nil
	default:
		return zero, &AmbiguousError{Resource: resource, Scope: scope, Name: name, UUID: uuid, Choices: matches}
	}
}

func verifyIdentity(resource string, selected, observed Choice, pinned bool) error {
	if observed.UUID != selected.UUID {
		return &IdentityError{Resource: resource, ExpectedUUID: selected.UUID, ActualUUID: observed.UUID, Reason: "response does not identify the selected resource"}
	}
	if !pinned && observed.Name != selected.Name {
		return &IdentityError{Resource: resource, ExpectedUUID: selected.UUID, ActualUUID: observed.UUID, Reason: "resource name changed during resolution; retry or pin its UUID"}
	}
	return nil
}
