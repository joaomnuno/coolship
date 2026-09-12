package service

import (
	"context"
	"errors"

	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/project"
)

// Config reports the effective local configuration for this invocation. It
// reads files only: a credential problem is a warning here, not a failure.
// The preferences report comes from Dependencies, as the executable already
// loaded it, once, before the command tree ran; Config assembles it into the
// result without a second read, so every caller gets the complete result.
func (a *App) Config(ctx context.Context, options Options) (ConfigResult, error) {
	if err := ctx.Err(); err != nil {
		return ConfigResult{}, err
	}
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, false)
	if err != nil {
		return ConfigResult{}, input(err)
	}
	target, err := project.Select(p, options.Target, options.Environment)
	if err != nil {
		return ConfigResult{}, input(err)
	}
	result := ConfigResult{
		ConfigPath: p.ConfigPath, ConfigRoot: p.ConfigRoot, GitRoot: p.GitRoot,
		Target: target.Key, AppRoot: target.AppRoot, Binding: target.Binding,
	}
	configured := p.Config.Project.Environment
	if p.Config.Named() {
		configured = p.Config.Apps[target.Key].Environment
	}
	if options.Environment != "" && options.Environment != configured {
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
	result.Preferences = preferencesReport(a.deps.Preferences)
	return result, nil
}

// preferencesReport describes the preferences file without a second read:
// the executable loaded it once, before the tree ran. Without a report there
// is no line to show.
func preferencesReport(report preferences.Report) *PreferencesReport {
	if report.Path == "" && report.Err == nil {
		return nil
	}
	out := &PreferencesReport{Path: report.Path, Present: report.Present, Preferences: report.Preferences}
	if report.Err != nil {
		out.Error = report.Err.Error()
		// The path is already its own field; keep only the cause.
		var typed *preferences.Error
		if errors.As(report.Err, &typed) {
			out.Error = typed.Err.Error()
		}
	}
	return out
}
