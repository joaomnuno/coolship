package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
)

// Link selects an existing remote application and writes only its local binding.
func (a *App) Link(ctx context.Context, options LinkOptions, selectChoice Selector, confirm Confirm) (LinkResult, error) {
	if err := ctx.Err(); err != nil {
		return LinkResult{}, err
	}
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, true)
	if err != nil {
		return LinkResult{}, input(err)
	}
	authOptions := a.authOptions(options.Options, p.Config.Project.Context)
	if authOptions.Context == "" && authOptions.URL == "" && authOptions.Token == "" {
		instances, err := a.deps.ListInstances(authOptions)
		if err != nil {
			return LinkResult{}, input(err)
		}
		choices := make([]Choice, len(instances))
		for i, instance := range instances {
			choices[i] = Choice{ID: instance.Name, Name: instance.Name, Detail: instance.URL}
		}
		name, err := choose(ctx, "context", choices, selectChoice)
		if err != nil {
			return LinkResult{}, err
		}
		authOptions.Context = name
	}
	credentials, err := a.deps.ResolveCredentials(authOptions)
	if err != nil {
		return LinkResult{}, input(err)
	}
	backend, err := a.backend(credentials)
	if err != nil {
		return LinkResult{}, err
	}
	projects, err := backend.ListProjects(ctx)
	if err != nil {
		return LinkResult{}, err
	}
	projectID := options.ProjectUUID
	if options.Project == "" && projectID == "" {
		choices := make([]Choice, len(projects))
		for i, resource := range projects {
			choices[i] = Choice{ID: resource.UUID, Name: resource.Name, Detail: resource.UUID}
		}
		projectID, err = choose(ctx, "project", choices, selectChoice)
		if err != nil {
			return LinkResult{}, err
		}
	}
	remoteProject, err := resolver.SelectProject(projects, options.Project, projectID)
	if err != nil {
		return LinkResult{}, resolutionError(err)
	}
	environments, err := backend.ListEnvironments(ctx, remoteProject.UUID)
	if err != nil {
		return LinkResult{}, err
	}
	environmentID := options.EnvironmentUUID
	if options.Environment == "" && environmentID == "" {
		choices := make([]Choice, len(environments))
		for i, resource := range environments {
			choices[i] = Choice{ID: resource.UUID, Name: resource.Name, Detail: resource.UUID}
		}
		environmentID, err = choose(ctx, "environment", choices, selectChoice)
		if err != nil {
			return LinkResult{}, err
		}
	}
	environment, err := resolver.SelectEnvironment(environments, options.Environment, environmentID)
	if err != nil {
		return LinkResult{}, resolutionError(err)
	}
	details, err := backend.GetEnvironment(ctx, remoteProject.UUID, environment.UUID)
	if err != nil {
		return LinkResult{}, err
	}
	if details.UUID != environment.UUID {
		return LinkResult{}, errors.New("server returned a different environment identity")
	}
	applicationID := options.ApplicationUUID
	if options.Application == "" && applicationID == "" {
		choices := make([]Choice, len(details.Applications))
		for i, resource := range details.Applications {
			choices[i] = Choice{ID: resource.UUID, Name: resource.Name, Detail: resource.UUID}
		}
		applicationID, err = choose(ctx, "application", choices, selectChoice)
		if err != nil {
			return LinkResult{}, err
		}
	}
	application, err := resolver.SelectApplication(details.Applications, options.Application, applicationID)
	if err != nil {
		return LinkResult{}, resolutionError(err)
	}
	binding := config.Binding{Context: credentials.Name, Project: remoteProject.Name,
		Environment: environment.Name, Application: application.Name, Root: options.Root}
	if authOptions.URL != "" {
		binding.Context = ""
	}
	if binding.Root == "" && options.Target == "" && !p.Config.Named() {
		binding.Root = p.Config.Project.Root
	}
	if binding.Root == "" {
		binding.Root = project.DefaultRoot(p, options.Target)
	}
	projectMatches, environmentMatches, applicationMatches := 0, 0, 0
	for _, item := range projects {
		if item.Name == binding.Project {
			projectMatches++
		}
	}
	for _, item := range environments {
		if item.Name == binding.Environment {
			environmentMatches++
		}
	}
	for _, item := range details.Applications {
		if item.Name == binding.Application {
			applicationMatches++
		}
	}
	if options.ProjectUUID != "" || projectMatches > 1 {
		binding.ProjectUUID = remoteProject.UUID
	}
	if options.EnvironmentUUID != "" || environmentMatches > 1 {
		binding.EnvironmentUUID = environment.UUID
	}
	if options.ApplicationUUID != "" || applicationMatches > 1 {
		binding.ApplicationUUID = application.UUID
	}
	proposal, err := project.Propose(p, options.Target, binding)
	if err != nil {
		return LinkResult{}, input(err)
	}
	candidate := p
	candidate.Config = proposal.Config
	candidate.Exists = true
	target, err := project.Select(candidate, proposal.Key, "")
	if err != nil {
		return LinkResult{}, input(err)
	}
	verified, err := resolver.Resolve(ctx, backend, target)
	if err != nil {
		return LinkResult{}, resolutionError(err)
	}
	// Selection must remain the same even if a resource changes during the prompts.
	if verified.Project.UUID != remoteProject.UUID || verified.Environment.UUID != environment.UUID || verified.Application.UUID != application.UUID {
		return LinkResult{}, errors.New("remote selection changed while linking; run link again")
	}
	resolved := project.Context{Project: candidate, Target: target, InstanceName: credentials.Name, InstanceURL: credentials.URL,
		RemoteProject: verified.Project, Environment: verified.Environment, Application: verified.Application}
	plan := LinkPlan{Path: p.ConfigPath, Target: targetInfo(resolved), Replacing: proposal.Review,
		Converting: p.Exists && p.Config.Named() != proposal.Config.Named()}
	replace := options.Replace
	if plan.Replacing && !replace {
		if confirm == nil {
			return LinkResult{}, input(project.ErrReplacementRequired)
		}
		accepted, err := confirm(ctx, plan)
		if err != nil {
			return LinkResult{}, err
		}
		if !accepted {
			return LinkResult{}, ErrCancelled
		}
		replace = true
	}
	if err := ctx.Err(); err != nil {
		return LinkResult{}, err
	}
	if err := project.WriteBinding(p, options.Target, binding, replace); err != nil {
		return LinkResult{}, input(err)
	}
	warnings := verified.Warnings
	if authOptions.URL != "" {
		warnings = append(warnings, "Credentials come from COOLSHIP_URL and COOLSHIP_TOKEN; no named context was written.")
	}
	return LinkResult{Path: p.ConfigPath, Target: plan.Target, Warnings: warnings}, nil
}

func choose(ctx context.Context, kind string, choices []Choice, selectChoice Selector) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(choices) == 0 {
		return "", input(fmt.Errorf("no %s choices are available", kind))
	}
	if len(choices) == 1 {
		if choices[0].ID == "" {
			return "", errors.New("server returned a resource without an identity")
		}
		return choices[0].ID, nil
	}
	if selectChoice == nil {
		return "", input(fmt.Errorf("select a %s with an explicit flag", kind))
	}
	id, err := selectChoice(ctx, kind, choices)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", ErrCancelled
	}
	for _, choice := range choices {
		if choice.ID == id {
			return id, nil
		}
	}
	return "", input(fmt.Errorf("selected %s is not one of the available choices", kind))
}
