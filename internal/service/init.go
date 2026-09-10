package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/gitinfo"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
)

// probeTimeout bounds the anonymous ls-remote; an unreachable host must not
// hold the command for git's own connection timeout.
const probeTimeout = 45 * time.Second

// Init creates an application for this repository, then binds the directory
// to it exactly as link would. Nothing is sent before the plan is confirmed;
// an existing binding is never replaced. A private repository is created
// through a GitHub App or a deploy key registered in Coolify; with a new
// deploy key, only the key is created and its public half is returned.
func (a *App) Init(ctx context.Context, options InitOptions, selectChoice Selector, confirm ConfirmInit, emit Emitter) (InitResult, error) {
	if err := ctx.Err(); err != nil {
		return InitResult{}, err
	}
	timeout, err := deployTimeout(options.Timeout)
	if err != nil {
		return InitResult{}, err
	}
	if err := validateBuildOptions(options.BuildOptions); err != nil {
		return InitResult{}, err
	}
	if options.CreateProject && options.Project == "" {
		return InitResult{}, input(errors.New("--create-project needs --project NAME"))
	}
	source, err := initSource(options)
	if err != nil {
		return InitResult{}, err
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
	build, err := settleBuild(options.BuildOptions, appRoot)
	if err != nil {
		return InitResult{}, err
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
	// The source is settled first, so a repository the chosen GitHub App
	// cannot see, or a key that does not exist, fails before any other choice.
	origin, err := a.chooseSource(ctx, backend, source, options, repository, selectChoice)
	if err != nil {
		return InitResult{}, err
	}
	warnings := origin.warnings
	if build.BuildPack == BuildPackCompose && len(build.ComposeDomains) == 0 {
		warnings = append(warnings, "No service has a domain yet; set them per service in Coolify (domain set does not apply to Compose applications).")
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
	server, serverWarnings, err := chooseServer(ctx, backend, options.Server, selectChoice)
	if err != nil {
		return InitResult{}, err
	}
	warnings = append(warnings, serverWarnings...)

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
	plan := InitPlan{Path: p.ConfigPath, Target: proposal.Key, Root: root, Repository: origin.remote, Branch: repository.Branch,
		BuildPack: build.BuildPack, Port: build.Port, Static: build.Static, PublishDirectory: build.PublishDirectory,
		Dockerfile: build.Dockerfile, ComposeFile: build.ComposeFile, ComposeDomains: build.ComposeDomains,
		InstallCommand: build.InstallCommand, BuildCommand: build.BuildCommand, StartCommand: build.StartCommand,
		Name: name, Instance: credentials.Name,
		Project: remoteProject.Name, NewProject: newProject, Environment: environmentName, Server: server.Name, Deploy: options.Deploy,
		GitHubApp: origin.app.Name, DeployKey: origin.key.Name, NewDeployKey: origin.newKey != ""}
	if origin.source != SourcePublic {
		plan.Source = origin.source
	}
	if plan.NewDeployKey {
		plan.DeployKey = origin.newKey
		if options.Deploy {
			warnings = append(warnings, "--deploy has no effect yet: the application is created once the new key is registered on the repository.")
		}
	}
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

	if plan.NewDeployKey {
		key, err := a.createDeployKey(ctx, backend, origin.newKey, origin.remote)
		if err != nil {
			return InitResult{}, err
		}
		return InitResult{Plan: plan, DeployKey: &key, Warnings: warnings}, nil
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
		Name: name, GitRepository: origin.remote, GitBranch: repository.Branch,
		Source: origin.source, GitHubAppUUID: origin.app.UUID, PrivateKeyUUID: origin.key.UUID}
	build.apply(&spec)
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

// initSource settles the source the flags ask for: --github-app implies a
// GitHub App and either key flag implies a deploy key, while a --source that
// contradicts them is an input error.
func initSource(options InitOptions) (string, error) {
	source := options.Source
	if source == "" {
		source = SourceAuto
	}
	switch source {
	case SourceAuto, SourcePublic, SourceGitHubApp, SourceDeployKey:
	default:
		return "", input(fmt.Errorf("--source must be one of auto, public, github-app, deploy-key, not %q", source))
	}
	if options.GitHubApp != "" {
		if source == SourceAuto {
			source = SourceGitHubApp
		}
		if source != SourceGitHubApp {
			return "", input(fmt.Errorf("--github-app applies to --source github-app, not %s", source))
		}
	}
	if options.DeployKey != "" || options.CreateDeployKey != "" {
		if options.DeployKey != "" && options.CreateDeployKey != "" {
			return "", input(errors.New("--deploy-key and --create-deploy-key cannot be combined; the first uses an existing key, the second makes a new one"))
		}
		if source == SourceAuto {
			source = SourceDeployKey
		}
		if source != SourceDeployKey {
			return "", input(fmt.Errorf("--deploy-key and --create-deploy-key apply to --source deploy-key, not %s", source))
		}
	}
	return source, nil
}

// repository takes what the flags supplied and reads the rest from Git.
func (a *App) repository(ctx context.Context, options InitOptions, dir string) (gitinfo.Repository, error) {
	remote, branch := strings.TrimSpace(options.Repository), strings.TrimSpace(options.Branch)
	var repository gitinfo.Repository
	if remote == "" || branch == "" {
		if a.deps.InspectRepository == nil {
			return gitinfo.Repository{}, input(errors.New("Git inspection is not available; pass --repo and --branch"))
		}
		detected, err := a.deps.InspectRepository(ctx, dir)
		if err != nil {
			return gitinfo.Repository{}, input(fmt.Errorf("%w; pass --repo and --branch to say where Coolify should clone from", err))
		}
		repository = detected
		if branch == "" {
			branch = detected.Branch
		}
	}
	if remote == "" {
		remote = repository.Remote
	}
	// Both clone forms come from the remote as written; an inspector that
	// supplied only one form is completed here.
	parsed, err := gitinfo.Parse(remote)
	if err != nil {
		return gitinfo.Repository{}, input(err)
	}
	repository.Remote = parsed.Remote
	if repository.SSH == "" || options.Repository != "" {
		repository.SSH = parsed.SSH
	}
	if err := validateName("branch", branch); err != nil {
		return gitinfo.Repository{}, input(err)
	}
	repository.Branch = branch
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

// origin is the settled source of one application: how Coolify clones, from
// which remote form, and through which registered app or key.
type origin struct {
	source   string
	remote   string // the git_repository value Coolify stores
	app      models.GitHubApp
	key      models.PrivateKey
	newKey   string // name of a key to create instead of an application
	warnings []string
}

// chooseSource decides between an anonymous clone and the private sources.
// With auto, the remote is probed without credentials: reachable means
// public; otherwise the user picks a private source — asked for even when
// only one applies, so a private repository is never handled implicitly —
// or must name one when there is nobody to ask. An explicit public source
// is taken at its word: nothing is probed, so an unreachable host costs no
// time, and Coolify finds out at the first deployment.
func (a *App) chooseSource(ctx context.Context, backend Backend, source string, options InitOptions, repository gitinfo.Repository, selectChoice Selector) (origin, error) {
	result := origin{source: source, remote: repository.Remote}
	if source == SourcePublic {
		return result, nil
	}
	if source == SourceAuto {
		if a.deps.ProbeRemote == nil {
			return origin{}, input(errors.New("Git is not available to check whether the repository is public; pass --source public, github-app, or deploy-key"))
		}
		heads, err := a.probe(ctx, repository.Remote)
		switch {
		case err == nil:
			result.source = SourcePublic
			if !slices.Contains(heads, repository.Branch) {
				result.warnings = append(result.warnings, fmt.Sprintf("Branch %q was not found on %s; Coolify cannot deploy it until it is pushed.", repository.Branch, repository.Remote))
			}
			return result, nil
		case errors.Is(err, context.Canceled):
			return origin{}, err
		}
		private := []Choice{{ID: SourceDeployKey, Name: "Deploy key", Detail: "an SSH key held by Coolify and registered on the repository"}}
		flags := "--source deploy-key"
		if gitinfo.Host(repository.Remote) == "github.com" {
			private = append([]Choice{{ID: SourceGitHubApp, Name: "GitHub App", Detail: "an app installed on the repository and registered in Coolify"}}, private...)
			flags = "--source github-app or --source deploy-key"
		}
		if selectChoice == nil {
			return origin{}, input(fmt.Errorf("%s is not reachable anonymously (%v), so it needs a private source; pass %s", repository.Remote, err, flags))
		}
		result.warnings = append(result.warnings, fmt.Sprintf("%s is not reachable anonymously (%v).", repository.Remote, err))
		chosen, chooseErr := ask(ctx, "source", private, selectChoice)
		if chooseErr != nil {
			if errors.Is(chooseErr, ErrInput) { // the prompter had nobody to ask; say why a source is needed
				return origin{}, input(fmt.Errorf("%s is not reachable anonymously (%v), so it needs a private source (%s): %w", repository.Remote, err, flags, chooseErr))
			}
			return origin{}, chooseErr
		}
		result.source = chosen
	}
	switch result.source {
	case SourceGitHubApp:
		return a.chooseGitHubApp(ctx, backend, options, repository, selectChoice, result)
	case SourceDeployKey:
		return a.chooseDeployKey(ctx, backend, options, repository, selectChoice, result)
	}
	return origin{}, fmt.Errorf("unknown source %q", result.source)
}

// probe runs the anonymous ls-remote under its own deadline.
func (a *App) probe(ctx context.Context, remote string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	heads, err := a.deps.ProbeRemote(ctx, remote)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("no answer within %s", probeTimeout)
	}
	return heads, err
}

// githubBranchPage is how many branches GitHub answers per page by default.
// Coolify relays the first page of the app's branch listing and nothing
// more, so a shorter listing is complete and a full one may go on.
const githubBranchPage = 30

// chooseGitHubApp picks an installed app, then lists the repository's
// branches through Coolify as the app sees them: a repository the app cannot
// reach is the server's 404, so a missing installation fails here rather
// than at creation, and a branch absent from a complete listing is a typo.
// One call answers both; the app's whole repository listing is never asked
// for, since the server pages through every installation repository to
// build it and times out on large installations.
func (a *App) chooseGitHubApp(ctx context.Context, backend Backend, options InitOptions, repository gitinfo.Repository, selectChoice Selector, result origin) (origin, error) {
	host, path := gitinfo.Host(repository.Remote), gitinfo.Path(repository.Remote)
	if host != "github.com" {
		return origin{}, input(fmt.Errorf("a GitHub App clones from github.com only, not %s; use --source deploy-key", host))
	}
	apps, err := backend.ListGitHubApps(ctx)
	if err != nil {
		return origin{}, err
	}
	var installed []models.GitHubApp
	for _, app := range apps {
		if !app.IsPublic { // the built-in public source has no installation to clone privately with
			installed = append(installed, app)
		}
	}
	names := make([]string, len(installed))
	for i, app := range installed {
		names[i] = app.Name
	}
	var app models.GitHubApp
	if options.GitHubApp != "" {
		var matches []models.GitHubApp
		for _, candidate := range installed {
			if candidate.Name == options.GitHubApp {
				matches = append(matches, candidate)
			}
		}
		switch len(matches) {
		case 0:
			if len(installed) == 0 {
				return origin{}, input(fmt.Errorf("no GitHub App named %q: Coolify has no GitHub App installed; register one under Sources, or use --source deploy-key", options.GitHubApp))
			}
			return origin{}, input(fmt.Errorf("no GitHub App named %q; available: %s", options.GitHubApp, strings.Join(names, ", ")))
		case 1:
			app = matches[0]
		default:
			return origin{}, input(fmt.Errorf("%d GitHub Apps are named %q; rename one in Coolify", len(matches), options.GitHubApp))
		}
	} else {
		if len(installed) == 0 {
			return origin{}, input(errors.New("Coolify has no GitHub App installed; register one under Sources, or use --source deploy-key"))
		}
		choices := make([]Choice, len(installed))
		for i, candidate := range installed {
			detail := candidate.Organization
			if detail == "" {
				detail = "personal installation"
			}
			choices[i] = Choice{ID: candidate.UUID, Name: candidate.Name, Detail: detail}
		}
		id, err := choose(ctx, "GitHub App", choices, selectChoice)
		if err != nil {
			return origin{}, err
		}
		for _, candidate := range installed {
			if candidate.UUID == id {
				app = candidate
			}
		}
	}
	result.app = app
	owner, repo, _ := strings.Cut(path, "/")
	branches, err := backend.ListGitHubBranches(ctx, app.ID, owner, repo)
	if err != nil {
		var status interface{ HTTPStatusCode() int }
		if errors.As(err, &status) && status.HTTPStatusCode() == 404 {
			if app.IsSystemWide {
				// The server answers for the owning team's apps only; a
				// system-wide app of another team is checked at creation instead.
				result.warnings = append(result.warnings, fmt.Sprintf("GitHub App %q belongs to another team, so its access to %s could not be checked here; the server checks it at creation.", app.Name, path))
				return result, nil
			}
			return origin{}, input(fmt.Errorf("GitHub App %q cannot access %s; give it access to that repository in its installation settings on GitHub, or pick another app: %s", app.Name, path, strings.Join(names, ", ")))
		}
		return origin{}, fmt.Errorf("list the branches of %s through GitHub App %q: %w", path, app.Name, err)
	}
	known := make([]string, 0, len(branches))
	for _, branch := range branches {
		if branch.Name == repository.Branch {
			return result, nil
		}
		known = append(known, branch.Name)
	}
	if len(branches) >= githubBranchPage {
		// The listing may go on beyond what the server relays; whether the
		// branch exists is left to the first deployment.
		result.warnings = append(result.warnings, fmt.Sprintf("Branch %q is not among the first %d branches of %s that GitHub App %q lists, so it could not be checked; the first deployment fails if it does not exist.", repository.Branch, len(branches), path, app.Name))
		return result, nil
	}
	if len(known) > 8 {
		known = append(known[:8], "…")
	}
	return origin{}, input(fmt.Errorf("branch %q does not exist in %s as GitHub App %q sees it (branches: %s); push it, or pass --branch", repository.Branch, path, app.Name, strings.Join(known, ", ")))
}

// chooseDeployKey picks a key Coolify holds, or records the name of one to
// create. A deploy key clones over SSH, so the SSH form of the remote is
// what Coolify gets.
func (a *App) chooseDeployKey(ctx context.Context, backend Backend, options InitOptions, repository gitinfo.Repository, selectChoice Selector, result origin) (origin, error) {
	result.remote = repository.SSH
	keys, err := backend.ListPrivateKeys(ctx)
	if err != nil {
		return origin{}, err
	}
	if options.CreateDeployKey != "" {
		name := strings.TrimSpace(options.CreateDeployKey)
		if err := validateName("deploy key name", name); err != nil {
			return origin{}, input(err)
		}
		for _, key := range keys {
			if key.Name == name {
				return origin{}, input(fmt.Errorf("Coolify already holds a key named %q; pass --deploy-key %q to use it, or choose another name", name, name))
			}
		}
		result.newKey = name
		return result, nil
	}
	// Keys that belong to a GitHub App and the instance's own localhost key
	// are not deploy keys; they are left out of the prompt, as Coolify's own
	// chooser does, but an explicit name still finds any key.
	var candidates []models.PrivateKey
	for _, key := range keys {
		if key.ID != 0 && !key.IsGitRelated {
			candidates = append(candidates, key)
		}
	}
	if options.DeployKey != "" {
		var matches []models.PrivateKey
		for _, key := range keys {
			if key.Name == options.DeployKey {
				matches = append(matches, key)
			}
		}
		switch len(matches) {
		case 0:
			names := make([]string, len(candidates))
			for i, key := range candidates {
				names[i] = key.Name
			}
			available := "none yet"
			if len(names) > 0 {
				available = strings.Join(names, ", ")
			}
			return origin{}, input(fmt.Errorf("no key named %q in Coolify (available: %s); pass --create-deploy-key %q to create one", options.DeployKey, available, options.DeployKey))
		case 1:
			result.key = matches[0]
			return result, nil
		default:
			return origin{}, input(fmt.Errorf("%d keys are named %q; rename one in Coolify", len(matches), options.DeployKey))
		}
	}
	if len(candidates) == 0 {
		return origin{}, input(errors.New("Coolify holds no key usable as a deploy key; pass --create-deploy-key NAME to create one"))
	}
	// A key is never taken implicitly, even when it is the only one: Coolify
	// also holds the keys it made for its servers, which the list does not
	// tell apart from deploy keys.
	choices := make([]Choice, len(candidates))
	names := make([]string, len(candidates))
	for i, key := range candidates {
		choices[i] = Choice{ID: key.UUID, Name: key.Name, Detail: key.Fingerprint}
		names[i] = key.Name
	}
	if selectChoice == nil {
		return origin{}, input(fmt.Errorf("pass --deploy-key NAME to choose among %s, or --create-deploy-key NAME", strings.Join(names, ", ")))
	}
	id, err := ask(ctx, "deploy key", choices, selectChoice)
	if err != nil {
		return origin{}, err
	}
	for _, key := range candidates {
		if key.UUID == id {
			result.key = key
		}
	}
	return result, nil
}

// createDeployKey generates a pair, registers the private half in Coolify,
// and returns the public half. The private half is discarded with the pair.
func (a *App) createDeployKey(ctx context.Context, backend Backend, name, remote string) (DeployKeyResult, error) {
	pair, err := a.deps.GenerateKey(name)
	if err != nil {
		return DeployKeyResult{}, fmt.Errorf("generate deploy key: %w", err)
	}
	created, err := backend.CreatePrivateKey(ctx, name, "Deploy key for "+remote+", created by coolship", pair.Private)
	if err != nil {
		var status interface{ HTTPStatusCode() int }
		if errors.As(err, &status) {
			return DeployKeyResult{}, fmt.Errorf("create deploy key %q: %w", name, err)
		}
		return DeployKeyResult{}, fmt.Errorf("create deploy key %q: %w (if the request reached the server, the key exists; check Coolify before retrying)", name, err)
	}
	return DeployKeyResult{Name: name, UUID: created.UUID, PublicKey: pair.Public, Repository: remote}, nil
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
