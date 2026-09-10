package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/auth"
)

// Login verifies a URL and token against the server, then stores them in the
// Coolify CLI configuration so both tools share one login. The token is only
// written after the server has accepted it.
func (a *App) Login(ctx context.Context, options LoginOptions) (LoginResult, error) {
	if err := ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	address, err := auth.NormalizeURL(options.URL)
	if err != nil {
		return LoginResult{}, input(err)
	}
	if err := auth.ValidateName(options.Name); err != nil {
		return LoginResult{}, input(err)
	}
	token := strings.TrimSpace(options.Token)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return LoginResult{}, input(errors.New("an API token is required; create one in Coolify under Keys & Tokens with read, write, and deploy abilities"))
	}
	backend, err := a.backend(auth.Credentials{Name: options.Name, URL: address, Token: token})
	if err != nil {
		return LoginResult{}, err
	}
	version, err := backend.Version(ctx)
	if err != nil {
		return LoginResult{}, fmt.Errorf("could not verify %s: %s", address, describeServerError(err))
	}
	team, err := backend.Team(ctx)
	if err != nil {
		return LoginResult{}, fmt.Errorf("token was not accepted by %s: %s", address, describeServerError(err))
	}
	existing := a.deps.InspectCredentials(auth.Options{ConfigPath: options.ConfigPath})
	replaced := false
	for _, instance := range existing.Instances {
		if instance.Name == options.Name {
			replaced = true
		}
	}
	path, err := a.deps.SaveCredentials(options.ConfigPath, auth.Stored{Name: options.Name, URL: address, Token: token}, options.Default)
	if err != nil {
		return LoginResult{}, input(err)
	}
	saved := a.deps.InspectCredentials(auth.Options{ConfigPath: path})
	return LoginResult{Name: options.Name, URL: address, Path: path, Default: saved.Default == options.Name, Server: version, Team: team.Name, Replaced: replaced}, nil
}

// Logout removes one stored instance. Nothing on the server changes; the
// token stays valid until it is revoked in Coolify.
func (a *App) Logout(ctx context.Context, options LogoutOptions) (LogoutResult, error) {
	if err := ctx.Err(); err != nil {
		return LogoutResult{}, err
	}
	if err := auth.ValidateName(options.Name); err != nil {
		return LogoutResult{}, input(err)
	}
	path, wasDefault, err := a.deps.RemoveCredentials(options.ConfigPath, options.Name)
	if err != nil {
		return LogoutResult{}, input(err)
	}
	result := LogoutResult{Name: options.Name, Path: path}
	remaining := a.deps.InspectCredentials(auth.Options{ConfigPath: path})
	if wasDefault && len(remaining.Instances) > 0 {
		result.Warnings = append(result.Warnings, "The removed context was the default; run coolship login --default or pass --context until you choose another.")
	}
	result.Warnings = append(result.Warnings, "The token is still valid on the server until you revoke it in Coolify.")
	return result, nil
}
