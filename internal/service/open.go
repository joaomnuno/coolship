package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/joaomnuno/coolship/internal/project"
)

// Open resolves the application's public URL, or its Coolify dashboard page.
func (a *App) Open(ctx context.Context, options OpenOptions) (OpenResult, error) {
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return OpenResult{}, err
	}
	result := OpenResult{Target: targetInfo(s.project), Warnings: s.warnings}
	if options.Dashboard {
		result.Kind = "dashboard"
		result.URL = applicationPage(s.project)
		return result, nil
	}
	urls := applicationURLs(s.project.Application.FQDN)
	if len(urls) == 0 {
		return result, input(errors.New("application has no domain configured; use --dashboard to open it in Coolify"))
	}
	result.Kind = "application"
	result.URL = urls[0]
	if len(urls) > 1 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Application has %d domains; using the first. Others: %s", len(urls), strings.Join(urls[1:], ", ")))
	}
	return result, nil
}

// applicationPage is the application's page in Coolify. Every segment is
// escaped so an identity the server chose cannot rewrite the path.
func applicationPage(p project.Context) string {
	return strings.TrimRight(p.InstanceURL, "/") +
		"/project/" + url.PathEscape(p.RemoteProject.UUID) +
		"/environment/" + url.PathEscape(p.Environment.UUID) +
		"/application/" + url.PathEscape(p.Application.UUID)
}

// deploymentPage is one deployment's page in Coolify, where its log and the
// retry live; it hangs off the application page so the two cannot drift.
func deploymentPage(p project.Context, deploymentUUID string) string {
	return applicationPage(p) + "/deployment/" + url.PathEscape(deploymentUUID)
}

// resultURL names where a deployment can be seen: the application once the
// deployment finished and the application has a domain, otherwise the
// deployment's page in Coolify — a queued or unfinished deployment has
// nothing to show yet, and a failed one has its log there. A preview keeps
// the page too, since the application's FQDN is the production one.
func resultURL(p project.Context, result DeployResult) (string, string) {
	if result.Status == "finished" && result.PullRequest == 0 {
		if urls := applicationURLs(p.Application.FQDN); len(urls) > 0 {
			return urls[0], "application"
		}
	}
	return deploymentPage(p, result.DeploymentUUID), "deployment"
}

// applicationURLs keeps only web URLs from Coolify's comma-separated domain
// list, so a malformed domain can never reach a browser.
func applicationURLs(fqdn string) []string {
	var result []string
	for _, item := range strings.Split(fqdn, ",") {
		item = strings.TrimSpace(item)
		parsed, err := url.Parse(item)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}
