package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
		result.URL = strings.TrimRight(s.project.InstanceURL, "/") +
			"/project/" + url.PathEscape(s.project.RemoteProject.UUID) +
			"/environment/" + url.PathEscape(s.project.Environment.UUID) +
			"/application/" + url.PathEscape(s.project.Application.UUID)
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
