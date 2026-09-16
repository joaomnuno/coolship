package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/envfile"
)

// ProjectState is what the hints after link and init are chosen from: the
// local variables file, what the application already has on the server, and
// whether it was ever deployed. It holds counts and names, never values.
type ProjectState struct {
	Target TargetInfo `json:"target"`
	// EnvFile is the local dotenv file, relative to the application root,
	// when it exists and sets at least one key; LocalVariables counts them.
	EnvFile        string `json:"env_file,omitempty"`
	LocalVariables int    `json:"local_variables"`
	// RemoteVariables counts the regular-scope variables on the server.
	RemoteVariables int      `json:"remote_variables"`
	Domains         []string `json:"domains,omitempty"`
	// Generated reports that the only domain is the one Coolify generated.
	Generated bool `json:"generated,omitempty"`
	// Compose reports a Compose application, whose domains are per service.
	Compose  bool `json:"compose,omitempty"`
	Deployed bool `json:"deployed"`
}

// ProjectState reads the linked application's state for the next-step hints.
// The variables and the deployment history are read at the same time; a
// history that cannot be read counts as not deployed.
func (a *App) ProjectState(ctx context.Context, options Options) (ProjectState, error) {
	s, err := a.prepare(ctx, options)
	if err != nil {
		return ProjectState{}, err
	}
	application := s.project.Application
	domains := applicationURLs(application)
	state := ProjectState{Target: targetInfo(s.project), Domains: domains, Generated: isGenerated(domains, application.UUID), Compose: application.IsCompose()}
	type history struct {
		last *DeploymentSummary
		err  error
	}
	read := make(chan history, 1)
	go func() {
		last, _, err := lastDeployment(ctx, s.backend, application.UUID)
		read <- history{last, err}
	}()
	variables, err := s.backend.ListEnvironmentVariables(ctx, application.UUID)
	found := <-read
	if err != nil {
		return ProjectState{}, err
	}
	if found.err != nil {
		return ProjectState{}, found.err
	}
	state.Deployed = found.last != nil
	for _, variable := range variables {
		if !variable.IsPreview {
			state.RemoteVariables++
		}
	}
	const name = ".env"
	file, err := envfile.Read(filepath.Join(s.project.Target.AppRoot, name))
	if err == nil && file.Exists {
		if count := len(file.Entries()); count > 0 {
			state.EnvFile, state.LocalVariables = name, count
		}
	}
	return state, nil
}

// Contexts lists the contexts saved in the Coolify CLI configuration, for a
// choice among them. The COOLSHIP_URL and COOLSHIP_TOKEN pair is not a saved
// context and is left out; a file that does not exist lists none.
func (a *App) Contexts(options Options) ([]auth.Instance, error) {
	instances, err := a.deps.ListInstances(auth.Options{ConfigPath: options.CoolifyConfig})
	var missing *auth.MissingCredentialsError
	if errors.As(err, &missing) || errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return instances, err
}
