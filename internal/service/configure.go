package service

import (
	"context"

	"github.com/joaomnuno/coolship/internal/project"
)

// Config reports the effective local configuration for this invocation. It
// reads files only: a credential problem is a warning here, not a failure.
func (a *App) Config(ctx context.Context, options Options) (ConfigResult, error) {
	if err := ctx.Err(); err != nil {
		return ConfigResult{}, err
	}
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, false)
	if err != nil {
		return ConfigResult{}, input(err)
	}
	target, err := project.Select(p, options.Environment)
	if err != nil {
		return ConfigResult{}, input(err)
	}
	result := ConfigResult{
		ConfigPath: p.ConfigPath, ConfigRoot: p.ConfigRoot, GitRoot: p.GitRoot,
		Target: target.Key, AppRoot: target.AppRoot, Binding: target.Binding,
	}
	if options.Environment != "" && options.Environment != p.Config.Project.Environment {
		result.Overrides = map[string]string{"environment": options.Environment}
	}
	if options.Context != "" {
		if result.Overrides == nil {
			result.Overrides = map[string]string{}
		}
		result.Overrides["context"] = options.Context
	}
	authOptions := a.authOptions(options, target.Binding.Context)
	report := a.deps.InspectCredentials(authOptions)
	result.CredentialSource = report.Source
	result.CredentialPath = report.Path
	credentials, err := a.deps.ResolveCredentials(authOptions)
	if err != nil {
		result.Warnings = append(result.Warnings, "Credentials: "+err.Error())
	} else {
		result.Instance = credentials.Name
		result.InstanceURL = credentials.URL
	}
	if a.deps.CredentialURL != "" {
		result.Warnings = append(result.Warnings, "COOLSHIP_URL and COOLSHIP_TOKEN select this invocation's instance; committed context is not used.")
	}
	return result, nil
}
