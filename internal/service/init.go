package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/gitinfo"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
)

// buildPacks are the ones init can create. Docker Compose is refused: its
// domains and variables are per service, which no other Coolship command
// models yet.
var buildPacks = []string{"nixpacks", "dockerfile", "static"}

// composeFiles are the names Coolify itself looks for.
var composeFiles = []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

// Init creates an application for this repository from its public remote,
// then binds the directory to it exactly as link would. Nothing is sent
// before the plan is confirmed; an existing binding is never replaced.
func (a *App) Init(ctx context.Context, options InitOptions, selectChoice Selector, confirm ConfirmInit, emit Emitter) (InitResult, error) {
	if err := ctx.Err(); err != nil {
		return InitResult{}, err
	}
	timeout, err := deployTimeout(options.Timeout)
	if err != nil {
		return InitResult{}, err
	}
	if options.BuildPack != "" && !slices.Contains(buildPacks, options.BuildPack) {
		if options.BuildPack == "dockercompose" {
			return InitResult{}, input(errors.New(composeRefusal))
		}
		return InitResult{}, input(fmt.Errorf("--build-pack must be one of %s, not %q", strings.Join(buildPacks, ", "), options.BuildPack))
	}
	if options.Port < 0 || options.Port > 65535 {
		return InitResult{}, input(fmt.Errorf("--port must be between 1 and 65535, not %d", options.Port))
	}
	if options.CreateProject && options.Project == "" {
		return InitResult{}, input(errors.New("--create-project needs --project NAME"))
	}
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, true)
	if err != nil {
		return InitResult{}, input(err)
	}
	root := proposedRoot(p, options.Target, "")
	appRoot := filepath.Join(p.ConfigRoot, root)

	repository, err := a.repository(ctx, options, appRoot)
	if err != nil {
		return InitResult{}, err
	}
	buildPack := options.BuildPack
	if buildPack == "" {
		if buildPack, err = detectBuildPack(appRoot); err != nil {
			return InitResult{}, err
		}
	}
	port := options.Port
	if port == 0 {
		port = defaultPort(buildPack)
	}
	name := strings.TrimSpace(options.Name)
	if name == "" {
		name = gitinfo.Name(repository.Remote)
	}
	if err := validateName("application name", name); err != nil {
		return InitResult{}, input(err)
	}
	environmentName := options.Environment
	if environmentName == "" {
		environmentName = "production"
	}

	authOptions, credentials, err := a.selectCredentials(ctx, options.Options, p.Config.Project.Context, selectChoice)
	if err != nil {
		return InitResult{}, err
	}
	backend, err := a.backend(credentials)
	if err != nil {
		return InitResult{}, err
	}
	projects, err := backend.ListProjects(ctx)
	if err != nil {
		return InitResult{}, err
	}
	remoteProject, newProject, err := chooseProject(ctx, projects, options, selectChoice)
	if err != nil {
		return InitResult{}, err
	}
	var environments []models.Environment
	var environment models.Environment
	var applications []models.Application
	if !newProject {
		environments, environment, applications, err = inspectEnvironment(ctx, backend, remoteProject, environmentName)
		if err != nil {
			return InitResult{}, err
		}
		for _, existing := range applications {
			if existing.Name == name {
				return InitResult{}, input(fmt.Errorf("application %q already exists in environment %q of project %q; pass --name for a new one, or run coolship link to bind the existing one", name, environmentName, remoteProject.Name))
			}
		}
	}
	server, warnings, err := chooseServer(ctx, backend, options.Server, selectChoice)
	if err != nil {
		return InitResult{}, err
	}

	// The binding is proposed before anything is created, so a directory that
	// is already linked is refused rather than left pointing at the old
	// application after a second one appeared.
	binding := config.Binding{Context: credentials.Name, Project: remoteProject.Name, Environment: environmentName, Application: name, Root: root}
	if authOptions.URL != "" {
		binding.Context = ""
	}
	proposal, err := project.Propose(p, options.Target, binding)
	if err != nil {
		return InitResult{}, input(err)
	}
	if proposal.Review {
		return InitResult{}, input(fmt.Errorf("%s already binds this target; run coolship unlink first, or coolship link to change the binding without creating an application", p.ConfigPath))
	}
	plan := InitPlan{Path: p.ConfigPath, Target: proposal.Key, Root: root, Repository: repository.Remote, Branch: repository.Branch,
		BuildPack: buildPack, Port: port, Static: options.Static, Name: name, Instance: credentials.Name,
		Project: remoteProject.Name, NewProject: newProject, Environment: environmentName, Server: server.Name, Deploy: options.Deploy}
	if !options.Yes {
		if confirm == nil {
			return InitResult{}, input(errors.New("creating an application requires --yes when input is noninteractive"))
		}
		accepted, err := confirm(ctx, plan)
		if err != nil {
			return InitResult{}, err
		}
		if !accepted {
			return InitResult{}, ErrCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return InitResult{}, err
	}

	if newProject {
		remoteProject, err = backend.CreateProject(ctx, options.Project, "")
		if err != nil {
			return InitResult{}, fmt.Errorf("create project %q: %w", options.Project, err)
		}
		projects = append(projects, remoteProject)
		environments, environment, applications, err = inspectEnvironment(ctx, backend, remoteProject, environmentName)
		if err != nil {
			return InitResult{}, fmt.Errorf("project %q (%s) was created, but its environment could not be used: %w", remoteProject.Name, remoteProject.UUID, err)
		}
	}
	spec := models.ApplicationSpec{ProjectUUID: remoteProject.UUID, EnvironmentName: environment.Name, ServerUUID: server.UUID,
		Name: name, GitRepository: repository.Remote, GitBranch: repository.Branch, BuildPack: buildPack,
		PortsExposes: fmt.Sprint(port), IsStatic: options.Static}
	if root != "." {
		spec.BaseDirectory = "/" + filepath.ToSlash(root)
	}
	created, err := backend.CreateApplication(ctx, spec)
	if err != nil {
		var status interface{ HTTPStatusCode() int }
		if errors.As(err, &status) {
			return InitResult{}, fmt.Errorf("create application %q: %w", name, err)
		}
		return InitResult{}, fmt.Errorf("create application %q: %w (if the request reached the server, the application exists; check Coolify before retrying)", name, err)
	}
	application := models.Application{UUID: created.UUID, Name: name, FQDN: created.Domains}
	outcome, err := a.writeBinding(ctx, p, backend, bindingRequest{
		credentials: credentials, authOptions: authOptions, target: options.Target,
		projects: projects, project: remoteProject, environments: environments, environment: environment,
		applications: append(applications, application), application: application,
	}, nil)
	if err != nil {
		return InitResult{}, fmt.Errorf("application %q (%s) was created, but the binding was not written: %w; run coolship link --application %q to bind it", name, created.UUID, err, name)
	}
	result := InitResult{Plan: plan, Target: outcome.result.Target, URL: created.Domains, Warnings: append(warnings, outcome.result.Warnings...)}
	if !options.Deploy {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deployment, err := a.deploy(ctx, session{project: outcome.context, backend: backend}, DeployOptions{Options: options.Options, Timeout: timeout}, emit)
	if err != nil {
		return result, fmt.Errorf("application %q was created and linked; its first deployment failed: %w", name, err)
	}
	result.Deployment = &deployment
	return result, nil
}

const composeRefusal = "this is a Docker Compose project; init does not create compose applications, whose domains and variables are per service. Create it in Coolify, then run coolship link"

// repository takes what the flags supplied and reads the rest from Git.
func (a *App) repository(ctx context.Context, options InitOptions, dir string) (gitinfo.Repository, error) {
	repository := gitinfo.Repository{Remote: strings.TrimSpace(options.Repository), Branch: strings.TrimSpace(options.Branch)}
	if repository.Remote == "" || repository.Branch == "" {
		if a.deps.InspectRepository == nil {
			return gitinfo.Repository{}, input(errors.New("Git inspection is not available; pass --repo and --branch"))
		}
		detected, err := a.deps.InspectRepository(ctx, dir)
		if err != nil {
			return gitinfo.Repository{}, input(fmt.Errorf("%w; pass --repo and --branch to say where Coolify should clone from", err))
		}
		if repository.Remote == "" {
			repository.Remote = detected.Remote
		}
		if repository.Branch == "" {
			repository.Branch = detected.Branch
		}
	}
	remote, err := gitinfo.NormalizeRemote(repository.Remote)
	if err != nil {
		return gitinfo.Repository{}, input(err)
	}
	repository.Remote = remote
	if err := validateName("branch", repository.Branch); err != nil {
		return gitinfo.Repository{}, input(err)
	}
	return repository, nil
}

func validateName(what, value string) error {
	if value == "" {
		return fmt.Errorf("%s is empty", what)
	}
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s %q must not contain whitespace or start with a dash", what, value)
	}
	return nil
}

// detectBuildPack reads the application root the way Coolify's own detection
// would: a compose file is refused, a Dockerfile builds itself, anything else
// is handed to Nixpacks.
func detectBuildPack(dir string) (string, error) {
	for _, name := range composeFiles {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return "", input(errors.New(composeRefusal))
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil && !info.IsDir() {
		return "dockerfile", nil
	}
	return "nixpacks", nil
}

// defaultPort is what each build pack's typical result listens on; the plan
// shows it so the user can check before creation.
func defaultPort(buildPack string) int {
	if buildPack == "nixpacks" {
		return 3000
	}
	return 80
}

// chooseProject selects by name, creates on request, or prompts among all.
func chooseProject(ctx context.Context, projects []models.Project, options InitOptions, selectChoice Selector) (models.Project, bool, error) {
	if options.Project != "" {
		selected, err := resolver.SelectProject(projects, options.Project, "")
		var missing *resolver.MissingError
		if errors.As(err, &missing) {
			if options.CreateProject {
				return models.Project{Name: options.Project}, true, nil
			}
			return models.Project{}, false, input(fmt.Errorf("%w; pass --create-project to create it", err))
		}
		if err != nil {
			return models.Project{}, false, resolutionError(err)
		}
		return selected, false, nil
	}
	choices := make([]Choice, len(projects))
	for i, resource := range projects {
		choices[i] = Choice{ID: resource.UUID, Name: resource.Name, Detail: resource.UUID}
	}
	id, err := choose(ctx, "project", choices, selectChoice)
	if err != nil {
		return models.Project{}, false, err
	}
	selected, err := resolver.SelectProject(projects, "", id)
	if err != nil {
		return models.Project{}, false, resolutionError(err)
	}
	return selected, false, nil
}

// inspectEnvironment finds the environment by name and reads its applications.
func inspectEnvironment(ctx context.Context, backend Backend, remoteProject models.Project, name string) ([]models.Environment, models.Environment, []models.Application, error) {
	environments, err := backend.ListEnvironments(ctx, remoteProject.UUID)
	if err != nil {
		return nil, models.Environment{}, nil, err
	}
	environment, err := resolver.SelectEnvironment(environments, name, "")
	if err != nil {
		return nil, models.Environment{}, nil, resolutionError(err)
	}
	details, err := backend.GetEnvironment(ctx, remoteProject.UUID, environment.UUID)
	if err != nil {
		return nil, models.Environment{}, nil, err
	}
	if details.UUID != environment.UUID {
		return nil, models.Environment{}, nil, errors.New("server returned a different environment identity")
	}
	return environments, environment, details.Applications, nil
}

// chooseServer takes the named server, or the only usable one, or prompts
// among the usable ones. A named server that Coolify does not consider usable
// is accepted with a warning: the server decides at creation.
func chooseServer(ctx context.Context, backend Backend, name string, selectChoice Selector) (models.Server, []string, error) {
	servers, err := backend.ListServers(ctx)
	if err != nil {
		return models.Server{}, nil, err
	}
	if name != "" {
		var matches []models.Server
		for _, server := range servers {
			if server.Name == name {
				matches = append(matches, server)
			}
		}
		switch len(matches) {
		case 0:
			names := make([]string, len(servers))
			for i, server := range servers {
				names[i] = server.Name
			}
			return models.Server{}, nil, input(fmt.Errorf("no server named %q; available: %s", name, strings.Join(names, ", ")))
		case 1:
			var warnings []string
			if !matches[0].IsUsable || !matches[0].IsReachable {
				warnings = append(warnings, fmt.Sprintf("Coolify does not report server %q as usable and reachable; creation may fail.", name))
			}
			return matches[0], warnings, nil
		default:
			return models.Server{}, nil, input(fmt.Errorf("%d servers are named %q; rename one in Coolify", len(matches), name))
		}
	}
	var choices []Choice
	for _, server := range servers {
		if server.IsUsable && server.IsReachable {
			choices = append(choices, Choice{ID: server.UUID, Name: server.Name, Detail: server.IP})
		}
	}
	id, err := choose(ctx, "server", choices, selectChoice)
	if err != nil {
		return models.Server{}, nil, err
	}
	for _, server := range servers {
		if server.UUID == id {
			return server, nil, nil
		}
	}
	return models.Server{}, nil, errors.New("selected server is not one of the available choices")
}
