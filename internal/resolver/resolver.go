package resolver

import (
	"context"
	"fmt"

	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
)

// Catalog is owned by the resolver, and contains only the reads needed to
// establish an application binding in its project and environment.
type Catalog interface {
	ListProjects(context.Context) ([]models.Project, error)
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

// Resolve validates each child against its selected parent before inspecting the
// application. A missing pinned resource never falls back to a matching name.
func Resolve(ctx context.Context, catalog Catalog, target project.Target) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	selectors := target.Binding
	projects, err := catalog.ListProjects(ctx)
	if err != nil {
		return Binding{}, fmt.Errorf("list projects for binding: %w", err)
	}
	selectedProject, err := choose(projects, projectChoice, "project", "the selected instance", selectors.Project, selectors.ProjectUUID)
	if err != nil {
		return Binding{}, err
	}
	environments, err := catalog.ListEnvironments(ctx, selectedProject.UUID)
	if err != nil {
		return Binding{}, fmt.Errorf("list environments for project %q: %w", selectedProject.Name, err)
	}
	selectedEnvironment, err := choose(environments, environmentChoice, "environment", fmt.Sprintf("project %q", selectedProject.Name), selectors.Environment, selectors.EnvironmentUUID)
	if err != nil {
		return Binding{}, err
	}
	environment, err := catalog.GetEnvironment(ctx, selectedProject.UUID, selectedEnvironment.UUID)
	if err != nil {
		return Binding{}, fmt.Errorf("read selected environment: %w", err)
	}
	if err := verifyIdentity("environment", environmentChoice(selectedEnvironment), environmentChoice(environment), selectors.EnvironmentUUID != ""); err != nil {
		return Binding{}, err
	}
	selectedApplication, err := choose(environment.Applications, applicationChoice, "application", fmt.Sprintf("environment %q of project %q", environment.Name, selectedProject.Name), selectors.Application, selectors.ApplicationUUID)
	if err != nil {
		return Binding{}, err
	}
	application, err := catalog.GetApplication(ctx, selectedApplication.UUID)
	if err != nil {
		return Binding{}, fmt.Errorf("read selected application: %w", err)
	}
	if err := verifyIdentity("application", applicationChoice(selectedApplication), applicationChoice(application), selectors.ApplicationUUID != ""); err != nil {
		return Binding{}, err
	}
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
