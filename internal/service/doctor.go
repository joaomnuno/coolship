package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
)

// VerifiedServerVersion is the Coolify release Coolship was validated against.
const VerifiedServerVersion = "4.3.18"

// Doctor runs the same steps as every workflow, one at a time, and keeps going
// past local failures so one run can show every problem. Remote checks stop at
// the first failure because each depends on the previous.
func (a *App) Doctor(ctx context.Context, options Options) (DoctorResult, error) {
	if err := ctx.Err(); err != nil {
		return DoctorResult{}, err
	}
	var result DoctorResult
	add := func(name, status, detail string) {
		result.Checks = append(result.Checks, Check{Name: name, Status: status, Detail: detail})
		if status == "failed" {
			result.Failed = true
		}
	}

	var target project.Target
	linked := false
	p, err := project.Discover(project.Paths{CWD: options.CWD, ConfigPath: options.ConfigPath}, false)
	switch {
	case err != nil:
		add("Project configuration", "failed", err.Error())
	default:
		add("Project configuration", "ok", p.ConfigPath)
		if p.GitRoot == "" {
			add("Git repository", "warning", "no Git worktree above the configuration; discovery stops at the filesystem root")
		} else {
			add("Git repository", "ok", p.GitRoot)
		}
		target, err = project.Select(p, options.Target, options.Environment)
		if err != nil {
			add("Binding", "failed", err.Error())
		} else {
			add("Binding", "ok", describeBinding(target))
			linked = true
		}
	}

	authOptions := a.authOptions(options, target.Binding.Context)
	report := a.deps.InspectCredentials(authOptions)
	switch {
	case report.Err != nil && report.Source == "environment":
		add("Credentials", "failed", report.Err.Error())
	case report.Source == "environment":
		add("Credentials", "ok", "COOLSHIP_URL and COOLSHIP_TOKEN")
	case !report.Exists:
		add("Credentials", "failed", fmt.Sprintf("Coolify CLI configuration not found at %s; run coolify login, use --coolify-config, or set COOLSHIP_URL and COOLSHIP_TOKEN", report.Path))
	case report.Err != nil:
		add("Credentials", "failed", report.Err.Error())
	default:
		detail := fmt.Sprintf("%s (%d instance%s", report.Path, len(report.Instances), plural(len(report.Instances)))
		if report.Default != "" {
			detail += ", default " + report.Default
		}
		detail += ")"
		status := "ok"
		if report.WorldReadable {
			status = "warning"
			detail += "; file is readable by other users"
		}
		add("Credentials", status, detail)
	}
	credentials, err := a.deps.ResolveCredentials(authOptions)
	if err != nil {
		add("Context", "failed", err.Error())
		return result, nil
	}
	add("Context", "ok", credentials.Name+" at "+credentials.URL)

	backend, err := a.backend(credentials)
	if err != nil {
		add("Server", "failed", err.Error())
		return result, nil
	}
	version, err := backend.Version(ctx)
	if err != nil {
		add("Server", "failed", describeServerError(err))
		return result, nil
	}
	detail := "Coolify " + version
	if version != VerifiedServerVersion {
		detail += " (verified against " + VerifiedServerVersion + ")"
	}
	add("Server", "ok", detail)

	if !linked {
		add("Application", "skipped", "no binding to resolve")
		return result, nil
	}
	binding, err := resolver.Resolve(ctx, backend, target)
	if err != nil {
		add("Application", "failed", err.Error())
		return result, nil
	}
	status := "ok"
	if !strings.HasPrefix(binding.Application.Status, "running") {
		status = "warning"
	}
	add("Application", status, fmt.Sprintf("%s (%s) is %s", binding.Application.Name, binding.Application.UUID, binding.Application.Status))
	for _, warning := range binding.Warnings {
		add("Application", "warning", warning)
	}
	return result, nil
}

func describeBinding(target project.Target) string {
	b := target.Binding
	parts := []string{firstNonEmpty(b.Project, b.ProjectUUID), firstNonEmpty(b.Environment, b.EnvironmentUUID), firstNonEmpty(b.Application, b.ApplicationUUID)}
	return strings.Join(parts, " / ") + " in " + target.AppRoot
}

// describeServerError classifies the failures a wrong token produces. The
// backend is reached through its interface, so the status code is read through
// an interface as well.
func describeServerError(err error) string {
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		switch status.HTTPStatusCode() {
		case 401:
			return "server rejected the token (401); create a new API token in Coolify"
		case 403:
			return "token lacks the required ability (403); it needs read, write, and deploy"
		}
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
