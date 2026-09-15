package resolver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
// deployment history status shows. Resolve calls it as soon as the UUID is
// known, so the caller can run it concurrently with the application read. A
// pinned application UUID is known before its membership is checked; when the
// pin turns out to be missing and the application is found by name instead,
// Alongside is called again with that UUID, and the caller discards the
// earlier read. It must not block; the caller owns its goroutine and discards
// its result when resolution fails.
type Alongside func(applicationUUID string)

// Resolve validates each child against its selected parent before inspecting
// the application. A pinned resource that is missing falls back to its name,
// with a warning; a pin with no name, or a name that matches nothing or more
// than one resource, is an error.
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
//
// A pin that is missing in its scope (a 404 on the direct read, or an
// application absent from its environment) is resolved by the binding's name
// instead, exactly as an unpinned binding would be, and the binding carries
// one warning naming every pin that went missing.
func ResolveAlongside(ctx context.Context, catalog Catalog, target project.Target, alongside Alongside) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	// Reads still running when resolution returns, which only happens on
	// failure or after a missing pin, are cancelled rather than left to finish.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	selectors := target.Binding
	var stale []stalePin

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

	selectedProject, pinnedProject, err := selectProject(ctx, catalog, projectRead, projectList, selectors.Project, selectors.ProjectUUID)
	if err != nil {
		return Binding{}, err
	}
	if selectors.ProjectUUID != "" && !pinnedProject {
		stale = append(stale, stalePin{"project", selectors.ProjectUUID, selectedProject.Name})
	}
	projectScope := fmt.Sprintf("project %q", selectedProject.Name)
	var selectedEnvironment models.Environment
	// environmentByName selects the environment by its name in the project's
	// list and starts reading its details; stalePin is the missing pin it
	// stands in for, if any, so a failure can name both.
	environmentByName := func(stalePin string) error {
		environments := selectedProject.Environments
		if environments == nil {
			environments, err = catalog.ListEnvironments(ctx, selectedProject.UUID)
			if err != nil {
				return fmt.Errorf("list environments for project %q: %w", selectedProject.Name, err)
			}
		}
		selectedEnvironment, err = choose(environments, environmentChoice, "environment", projectScope, selectors.Environment, "")
		if err != nil {
			return fellBack(err, stalePin)
		}
		uuid := selectedEnvironment.UUID
		environmentRead = start(ctx, func(ctx context.Context) (models.Environment, error) {
			return catalog.GetEnvironment(ctx, selectedProject.UUID, uuid)
		})
		return nil
	}
	pinnedEnvironment := selectors.EnvironmentUUID != ""
	if pinnedEnvironment {
		// The early read was scoped by the pinned project; a project found by
		// name instead is another scope.
		if environmentRead == nil || selectedProject.UUID != selectors.ProjectUUID {
			environmentRead = start(ctx, func(ctx context.Context) (models.Environment, error) {
				return catalog.GetEnvironment(ctx, selectedProject.UUID, selectors.EnvironmentUUID)
			})
		}
		selectedEnvironment = models.Environment{UUID: selectors.EnvironmentUUID, Name: selectors.Environment}
	} else if err := environmentByName(""); err != nil {
		return Binding{}, err
	}
	environment, err := environmentRead.wait()
	if err != nil && pinnedEnvironment && notFound(err) {
		if selectors.Environment == "" {
			return Binding{}, &MissingError{Resource: "environment", Scope: projectScope, UUID: selectors.EnvironmentUUID}
		}
		pinnedEnvironment = false
		if err := environmentByName(selectors.EnvironmentUUID); err != nil {
			return Binding{}, err
		}
		stale = append(stale, stalePin{"environment", selectors.EnvironmentUUID, selectedEnvironment.Name})
		environment, err = environmentRead.wait()
	}
	if err != nil {
		return Binding{}, fmt.Errorf("read selected environment: %w", err)
	}
	if err := verifyIdentity("environment", environmentChoice(selectedEnvironment), environmentChoice(environment), pinnedEnvironment); err != nil {
		return Binding{}, err
	}
	applicationScope := fmt.Sprintf("environment %q of project %q", environment.Name, selectedProject.Name)
	pinnedApplication := selectors.ApplicationUUID != ""
	selectedApplication, err := choose(environment.Applications, applicationChoice, "application", applicationScope, selectors.Application, selectors.ApplicationUUID)
	var missing *MissingError
	if pinnedApplication && selectors.Application != "" && errors.As(err, &missing) {
		pinnedApplication = false
		selectedApplication, err = choose(environment.Applications, applicationChoice, "application", applicationScope, selectors.Application, "")
		if err != nil {
			return Binding{}, fellBack(err, selectors.ApplicationUUID)
		}
		stale = append(stale, stalePin{"application", selectors.ApplicationUUID, selectedApplication.Name})
		// The read of the missing pin is superseded; its answer is never used.
		startApplication(selectedApplication.UUID)
	}
	if err != nil {
		return Binding{}, err
	}
	if applicationRead == nil {
		startApplication(selectedApplication.UUID)
	}
	application, err := applicationRead.wait()
	if err != nil && pinnedApplication && notFound(err) {
		// The environment still listed the pin, but the application itself is
		// gone: a deletion race or a stale listing. It is missing all the same.
		if selectors.Application == "" {
			return Binding{}, &MissingError{Resource: "application", Scope: applicationScope, UUID: selectors.ApplicationUUID}
		}
		pinnedApplication = false
		others := make([]models.Application, 0, len(environment.Applications))
		for _, candidate := range environment.Applications {
			if candidate.UUID != selectors.ApplicationUUID {
				others = append(others, candidate)
			}
		}
		selectedApplication, err = choose(others, applicationChoice, "application", applicationScope, selectors.Application, "")
		if err != nil {
			return Binding{}, fellBack(err, selectors.ApplicationUUID)
		}
		stale = append(stale, stalePin{"application", selectors.ApplicationUUID, selectedApplication.Name})
		startApplication(selectedApplication.UUID)
		application, err = applicationRead.wait()
	}
	if err != nil {
		return Binding{}, fmt.Errorf("read selected application: %w", err)
	}
	if err := verifyIdentity("application", applicationChoice(selectedApplication), applicationChoice(application), pinnedApplication); err != nil {
		return Binding{}, err
	}
	selectedProject.Environments = nil
	result := Binding{Project: selectedProject, Environment: environment, Application: application}
	if warning := staleWarning(stale); warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}
	for _, label := range []struct {
		resource, configured, observed, uuid string
		pinned                               bool
	}{
		{"project", selectors.Project, selectedProject.Name, selectors.ProjectUUID, pinnedProject},
		{"environment", selectors.Environment, environment.Name, selectors.EnvironmentUUID, pinnedEnvironment},
		{"application", selectors.Application, application.Name, selectors.ApplicationUUID, pinnedApplication},
	} {
		if label.pinned && label.configured != "" && label.configured != label.observed {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s %q is now named %q; UUID %q remains pinned", label.resource, label.configured, label.observed, label.uuid))
		}
	}
	return result, nil
}

// stalePin is a pinned UUID that no longer exists in its scope, and the name
// the resource was found by instead.
type stalePin struct{ resource, uuid, name string }

// staleWarning names every missing pin in one warning, so a replaced project,
// whose environment and application pins go missing with it, warns once.
func staleWarning(stale []stalePin) string {
	switch len(stale) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("coolship.toml pins %s %s that no longer exists; found %q by name. Run coolship link to refresh.", stale[0].resource, stale[0].uuid, stale[0].name)
	}
	pins := make([]string, len(stale))
	names := make([]string, len(stale))
	for i, pin := range stale {
		pins[i] = pin.resource + " " + pin.uuid
		names[i] = fmt.Sprintf("%q", pin.name)
	}
	return fmt.Sprintf("coolship.toml pins %s that no longer exist; found %s by name. Run coolship link to refresh.", strings.Join(pins, ", "), strings.Join(names, ", "))
}

// fellBack marks a name lookup that stood in for a missing pin, so its error
// names the pin as well as the name.
func fellBack(err error, uuid string) error {
	var missing *MissingError
	if uuid != "" && errors.As(err, &missing) {
		missing.UUID = uuid
		missing.Fallback = true
	}
	return err
}

// selectProject finishes whichever project read Resolve started: the list, or
// the direct read of a pinned UUID, where a 404 is the pin going missing and
// the project is then found by name. It reports whether the pin held.
func selectProject(ctx context.Context, catalog Catalog, read *future[models.Project], list *future[[]models.Project], name, uuid string) (models.Project, bool, error) {
	const scope = "the selected instance"
	if read == nil {
		projects, err := list.wait()
		if err != nil {
			return models.Project{}, false, fmt.Errorf("list projects for binding: %w", err)
		}
		value, err := choose(projects, projectChoice, "project", scope, name, uuid)
		return value, false, err
	}
	value, err := read.wait()
	if err != nil {
		if !notFound(err) {
			return models.Project{}, false, fmt.Errorf("read project for binding: %w", err)
		}
		if name == "" {
			return models.Project{}, false, &MissingError{Resource: "project", Scope: scope, UUID: uuid}
		}
		projects, err := catalog.ListProjects(ctx)
		if err != nil {
			return models.Project{}, false, fmt.Errorf("list projects for binding: %w", err)
		}
		value, err := choose(projects, projectChoice, "project", scope, name, "")
		return value, false, fellBack(err, uuid)
	}
	if err := verifyIdentity("project", Choice{UUID: uuid, Name: name}, projectChoice(value), true); err != nil {
		return models.Project{}, false, err
	}
	return value, true, nil
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
