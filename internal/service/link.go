package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/models"
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
	authOptions, credentials, err := a.selectCredentials(ctx, options.Options, p.Config.Project.Context, selectChoice)
	if err != nil {
		return LinkResult{}, err
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
		return LinkResult{}, linkSelection(err, "--project", "--project-uuid")
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
		return LinkResult{}, linkSelection(err, "--environment", "--environment-uuid")
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
		return LinkResult{}, linkSelection(err, "--application", "--application-uuid")
	}
	outcome, err := a.writeBinding(ctx, p, backend, bindingRequest{
		credentials: credentials, authOptions: authOptions, target: options.Target, root: options.Root, replace: options.Replace,
		projects: projects, project: remoteProject, environments: environments, environment: environment,
		applications: details.Applications, application: application,
		pinProject: options.ProjectUUID != "", pinEnvironment: options.EnvironmentUUID != "", pinApplication: options.ApplicationUUID != "",
	}, confirm)
	if err != nil {
		return LinkResult{}, err
	}
	return outcome.result, nil
}

// linkSelection rewords a resolver failure for link itself, where the usual
// advice — check the binding, run link, link a UUID — does not apply because
// the selector came from a flag. The resolver's error stays the cause.
func linkSelection(err error, nameFlag, uuidFlag string) error {
	var missing *resolver.MissingError
	var ambiguous *resolver.AmbiguousError
	switch {
	case errors.As(err, &missing):
		selector := fmt.Sprintf("named %q", missing.Name)
		if missing.UUID != "" {
			selector = fmt.Sprintf("with UUID %q", missing.UUID)
		}
		return input(restate(err, "link: no %s %s in %s; pass an exact name with %s, or a UUID with %s", missing.Resource, selector, missing.Scope, nameFlag, uuidFlag))
	case errors.As(err, &ambiguous):
		choices := make([]string, len(ambiguous.Choices))
		for i, choice := range ambiguous.Choices {
			choices[i] = fmt.Sprintf("%q (%s)", choice.Name, choice.UUID)
		}
		return input(restate(err, "link: %d %ss match %q in %s: %s; pick one with %s", len(ambiguous.Choices), ambiguous.Resource, ambiguous.Name, ambiguous.Scope, strings.Join(choices, ", "), uuidFlag))
	}
	return resolutionError(err)
}

// selectCredentials resolves the instance for a workflow that has no complete
// binding yet: an explicit context, the committed one, the CI pair, the saved
// default, or — only when no instance is the default — a choice among the
// configured instances.
func (a *App) selectCredentials(ctx context.Context, options Options, configuredContext string, selectChoice Selector) (auth.Options, auth.Credentials, error) {
	authOptions := a.authOptions(options, configuredContext)
	if authOptions.Context == "" && authOptions.URL == "" && authOptions.Token == "" {
		instances, err := a.deps.ListInstances(authOptions)
		if err != nil {
			return auth.Options{}, auth.Credentials{}, input(err)
		}
		// The default is what every later command uses without --context, so
		// it is taken here without asking; --context overrides it.
		name := defaultInstance(instances)
		if name == "" {
			choices := make([]Choice, len(instances))
			for i, instance := range instances {
				choices[i] = Choice{ID: instance.Name, Name: instance.Name, Detail: instance.URL}
			}
			name, err = choose(ctx, "context", choices, selectChoice)
			if err != nil {
				return auth.Options{}, auth.Credentials{}, err
			}
		}
		authOptions.Context = name
	}
	credentials, err := a.deps.ResolveCredentials(authOptions)
	if err != nil {
		return auth.Options{}, auth.Credentials{}, input(err)
	}
	return authOptions, credentials, nil
}

// defaultInstance names the one instance marked default, or nothing when
// none or several are.
func defaultInstance(instances []auth.Instance) string {
	name := ""
	for _, instance := range instances {
		if !instance.Default {
			continue
		}
		if name != "" {
			return ""
		}
		name = instance.Name
	}
	return name
}

// bindingRequest is a chosen application together with the candidate lists it
// was chosen from, which decide whether names describe it uniquely.
type bindingRequest struct {
	credentials  auth.Credentials
	authOptions  auth.Options
	target       string // named target to write; empty writes [project]
	root         string // explicit root; empty applies the default rule
	replace      bool
	projects     []models.Project
	project      models.Project
	environments []models.Environment
	environment  models.Environment
	applications []models.Application
	application  models.Application
	// Explicit UUID selectors are always written as pins.
	pinProject, pinEnvironment, pinApplication bool
}

type bindingOutcome struct {
	result  LinkResult
	context project.Context // the verified binding, usable as a session
}

// proposedRoot is the root a binding gets without an explicit one: the
// existing [project] root when that form is kept, else the default rule.
func proposedRoot(p project.Project, target, explicit string) string {
	root := explicit
	if root == "" && target == "" && !p.Config.Named() {
		root = p.Config.Project.Root
	}
	if root == "" {
		root = project.DefaultRoot(p, target)
	}
	return root
}

// writeBinding composes the binding, verifies it through the resolver exactly
// as every later command will, reviews a replacement, and writes the file.
func (a *App) writeBinding(ctx context.Context, p project.Project, backend Backend, request bindingRequest, confirm Confirm) (bindingOutcome, error) {
	binding := config.Binding{Context: request.credentials.Name, Project: request.project.Name,
		Environment: request.environment.Name, Application: request.application.Name, Root: proposedRoot(p, request.target, request.root)}
	if request.authOptions.URL != "" {
		binding.Context = ""
	}
	projectMatches, environmentMatches, applicationMatches := 0, 0, 0
	for _, item := range request.projects {
		if item.Name == binding.Project {
			projectMatches++
		}
	}
	for _, item := range request.environments {
		if item.Name == binding.Environment {
			environmentMatches++
		}
	}
	for _, item := range request.applications {
		if item.Name == binding.Application {
			applicationMatches++
		}
	}
	if request.pinProject || projectMatches > 1 {
		binding.ProjectUUID = request.project.UUID
	}
	if request.pinEnvironment || environmentMatches > 1 {
		binding.EnvironmentUUID = request.environment.UUID
	}
	if request.pinApplication || applicationMatches > 1 {
		binding.ApplicationUUID = request.application.UUID
	}
	proposal, err := project.Propose(p, request.target, binding)
	if err != nil {
		return bindingOutcome{}, input(err)
	}
	candidate := p
	candidate.Config = proposal.Config
	candidate.Exists = true
	target, err := project.Select(candidate, proposal.Key, "")
	if err != nil {
		return bindingOutcome{}, input(err)
	}
	verified, err := resolver.Resolve(ctx, backend, target)
	if err != nil {
		return bindingOutcome{}, resolutionError(err)
	}
	// Selection must remain the same even if a resource changes during the prompts.
	if verified.Project.UUID != request.project.UUID || verified.Environment.UUID != request.environment.UUID || verified.Application.UUID != request.application.UUID {
		return bindingOutcome{}, errors.New("remote selection changed while linking; run link again")
	}
	resolved := project.Context{Project: candidate, Target: target, InstanceName: request.credentials.Name, InstanceURL: request.credentials.URL,
		RemoteProject: verified.Project, Environment: verified.Environment, Application: verified.Application}
	plan := LinkPlan{Path: p.ConfigPath, Target: targetInfo(resolved), Replacing: proposal.Review,
		Converting: p.Exists && p.Config.Named() != proposal.Config.Named()}
	replace := request.replace
	if plan.Replacing && !replace {
		if confirm == nil {
			return bindingOutcome{}, input(project.ErrReplacementRequired)
		}
		accepted, err := confirm(ctx, plan)
		if err != nil {
			return bindingOutcome{}, err
		}
		if !accepted {
			return bindingOutcome{}, ErrCancelled
		}
		replace = true
	}
	if err := ctx.Err(); err != nil {
		return bindingOutcome{}, err
	}
	if err := project.WriteBinding(p, request.target, binding, replace); err != nil {
		return bindingOutcome{}, input(err)
	}
	warnings := verified.Warnings
	if request.authOptions.URL != "" {
		warnings = append(warnings, "Credentials come from COOLSHIP_URL and COOLSHIP_TOKEN; no named context was written.")
	}
	return bindingOutcome{result: LinkResult{Path: p.ConfigPath, Target: plan.Target, Warnings: warnings}, context: resolved}, nil
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
	return ask(ctx, kind, choices, selectChoice)
}

// ask puts the choices to the user, a single one included; it is for choices
// that taking implicitly would decide something the user should see, such as
// the source of a private repository or the key that unlocks it. Without
// anybody to ask, the choice must come from a flag.
func ask(ctx context.Context, kind string, choices []Choice, selectChoice Selector) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(choices) == 0 {
		return "", input(fmt.Errorf("no %s choices are available", kind))
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
