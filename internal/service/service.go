package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
	"github.com/joaomnuno/coolship/internal/sshkey"
)

// App coordinates workflows with one authenticated backend per invocation.
type App struct{ deps Dependencies }

func New(deps Dependencies) *App {
	if deps.ResolveCredentials == nil {
		deps.ResolveCredentials = auth.Resolve
	}
	if deps.ListInstances == nil {
		deps.ListInstances = auth.List
	}
	if deps.InspectCredentials == nil {
		deps.InspectCredentials = auth.Inspect
	}
	if deps.SaveCredentials == nil {
		deps.SaveCredentials = auth.Save
	}
	if deps.FindLogin == nil {
		deps.FindLogin = auth.FindLogin
	}
	if deps.RemoveCredentials == nil {
		deps.RemoveCredentials = auth.Remove
	}
	if deps.PollInterval <= 0 {
		deps.PollInterval = 2 * time.Second
	}
	if deps.GenerateKey == nil {
		deps.GenerateKey = sshkey.Generate
	}
	return &App{deps: deps}
}

type session struct {
	project  project.Context
	backend  Backend
	warnings []string
}

func input(err error) error { return &InputError{Err: err} }

func (a *App) authOptions(options Options, configuredContext string) auth.Options {
	name := options.Context
	if name == "" && a.deps.CredentialURL == "" && a.deps.CredentialToken == "" {
		name = configuredContext
	}
	return auth.Options{Context: name, ConfigPath: options.CoolifyConfig, URL: a.deps.CredentialURL, Token: a.deps.CredentialToken}
}

func (a *App) backend(credentials auth.Credentials) (Backend, error) {
	if a.deps.NewBackend == nil {
		return nil, errors.New("Coolify backend is not configured")
	}
	return a.deps.NewBackend(credentials)
}

func (a *App) prepare(ctx context.Context, options Options) (session, error) {
	return a.prepareAlongside(ctx, options, nil)
}

// prepareAlongside is prepare with a read started beside the application
// read, as soon as the application's UUID is known; see resolver.Alongside.
func (a *App) prepareAlongside(ctx context.Context, options Options, alongside func(Backend, string)) (session, error) {
	if err := ctx.Err(); err != nil {
		return session{}, err
	}
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, false)
	if err != nil {
		return session{}, input(err)
	}
	target, err := project.Select(p, options.Target, options.Environment)
	if err != nil {
		return session{}, input(err)
	}
	credentials, err := a.deps.ResolveCredentials(a.authOptions(options, target.Binding.Context))
	if err != nil {
		return session{}, input(err)
	}
	backend, err := a.backend(credentials)
	if err != nil {
		return session{}, err
	}
	var started resolver.Alongside
	if alongside != nil {
		started = func(uuid string) { alongside(backend, uuid) }
	}
	binding, err := resolver.ResolveAlongside(ctx, backend, target, started)
	if err != nil {
		return session{}, resolutionError(err)
	}
	warnings := binding.Warnings
	if a.deps.CredentialURL != "" {
		warnings = append(warnings, "COOLSHIP_URL and COOLSHIP_TOKEN select this invocation's instance; committed context is not used.")
	}
	return session{
		project: project.Context{Project: p, Target: target, InstanceName: credentials.Name, InstanceURL: credentials.URL,
			RemoteProject: binding.Project, Environment: binding.Environment, Application: binding.Application},
		backend: backend, warnings: warnings,
	}, nil
}

func resolutionError(err error) error {
	var missing *resolver.MissingError
	var ambiguous *resolver.AmbiguousError
	var identity *resolver.IdentityError
	if errors.As(err, &missing) || errors.As(err, &ambiguous) || errors.As(err, &identity) {
		return input(err)
	}
	return err
}

func targetInfo(p project.Context) TargetInfo {
	return TargetInfo{Target: p.Target.Key, Instance: p.InstanceName, InstanceURL: p.InstanceURL,
		Project: p.RemoteProject.Name, ProjectUUID: p.RemoteProject.UUID,
		Environment: p.Environment.Name, EnvironmentUUID: p.Environment.UUID,
		Application: p.Application.Name, ApplicationUUID: p.Application.UUID, Root: p.Target.AppRoot}
}

func (a *App) Status(ctx context.Context, options Options) (StatusResult, error) {
	// The history needs only the application's UUID, so it is read beside
	// the application rather than after it. A failed resolution cancels it
	// and discards whatever it found. A pinned application that turns out to
	// be missing is found by name and reported again with its real UUID; that
	// read replaces the first, which is cancelled.
	historyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type history struct {
		last    *DeploymentSummary
		warning string
		err     error
	}
	var read chan history
	cancelRead := func() {}
	defer func() { cancelRead() }()
	s, err := a.prepareAlongside(ctx, options, func(backend Backend, uuid string) {
		cancelRead()
		readCtx, cancelThis := context.WithCancel(historyCtx)
		cancelRead = cancelThis
		result := make(chan history, 1)
		read = result
		go func() {
			last, warning, err := lastDeployment(readCtx, backend, uuid)
			result <- history{last, warning, err}
		}()
	})
	if err != nil {
		return StatusResult{}, err
	}
	result := StatusResult{Target: targetInfo(s.project), Status: s.project.Application.Status,
		URL: statusURL(s.project.Application), Warnings: s.warnings}
	var found history
	if read != nil {
		found = <-read
	} else {
		found.last, found.warning, found.err = lastDeployment(ctx, s.backend, s.project.Application.UUID)
	}
	if found.err != nil {
		return StatusResult{}, found.err
	}
	result.LastDeployment = found.last
	if found.warning != "" {
		result.Warnings = append(result.Warnings, found.warning)
	}
	return result, nil
}

func emitEvent(emit Emitter, event Event) error {
	if emit == nil {
		return nil
	}
	if err := emit(event); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
