package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/joaomnuno/coolship/internal/models"
)

// Domain reports the linked application's domains.
func (a *App) Domain(ctx context.Context, options Options) (DomainResult, error) {
	s, err := a.prepare(ctx, options)
	if err != nil {
		return DomainResult{}, err
	}
	domains := applicationURLs(s.project.Application.FQDN)
	return DomainResult{Target: targetInfo(s.project), Domains: domains, Generated: isGenerated(domains, s.project.Application.UUID), Warnings: s.warnings}, nil
}

// DomainSet replaces the application's domains after confirmation, then reads
// the application back so the result reflects what the server kept.
func (a *App) DomainSet(ctx context.Context, options DomainSetOptions, confirm ConfirmDomain) (DomainSetResult, error) {
	domains, err := normalizeDomains(options.Domains)
	if err != nil {
		return DomainSetResult{}, input(err)
	}
	if options.Redirect != "" && !slices.Contains([]string{"www", "non-www", "both"}, options.Redirect) {
		return DomainSetResult{}, input(fmt.Errorf("--redirect must be www, non-www, or both, not %q", options.Redirect))
	}
	s, err := a.prepare(ctx, options.Options)
	if err != nil {
		return DomainSetResult{}, err
	}
	plan := DomainPlan{Target: targetInfo(s.project), Current: applicationURLs(s.project.Application.FQDN), Domains: domains, Redirect: options.Redirect}
	result := DomainSetResult{Plan: plan, Warnings: s.warnings}
	if slices.Equal(plan.Current, domains) && options.Redirect == "" {
		result.Warnings = append(result.Warnings, "Domains are already set as requested; nothing changed.")
		return result, nil
	}
	if !options.Yes {
		if confirm == nil {
			return result, input(errors.New("changing domains requires --yes when input is noninteractive"))
		}
		accepted, err := confirm(ctx, plan)
		if err != nil {
			return result, err
		}
		if !accepted {
			return result, ErrCancelled
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	update := models.DomainUpdate{Domains: domains, Redirect: options.Redirect, Force: options.Force}
	if err := s.backend.UpdateApplicationDomains(ctx, s.project.Application.UUID, update); err != nil {
		return result, fmt.Errorf("update domains: %w (Coolify rejects a domain in use elsewhere unless --force, and Docker Compose applications take per-service domains, which this command does not set)", err)
	}
	application, err := s.backend.GetApplication(ctx, s.project.Application.UUID)
	if err != nil {
		return result, fmt.Errorf("domains were sent but could not be read back: %w", err)
	}
	if kept := applicationURLs(application.FQDN); !slices.Equal(kept, domains) {
		return result, fmt.Errorf("server kept %s instead of the requested domains; inspect Coolify", strings.Join(kept, ", "))
	}
	result.Warnings = append(result.Warnings, "The proxy learns the new domain on the next deployment; run coolship deploy to apply it.")
	return result, nil
}

// normalizeDomains accepts full web URLs, or bare hosts as https, and rejects
// anything a proxy could not route.
func normalizeDomains(inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, errors.New("at least one domain is required")
	}
	var result []string
	for _, raw := range inputs {
		for _, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if !strings.Contains(item, "://") {
				item = "https://" + item
			}
			parsed, err := url.Parse(item)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
				parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(item, " \t\r\n") {
				return nil, fmt.Errorf("domain %q must be a plain http or https URL such as https://app.example.com", item)
			}
			normalized := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + strings.TrimRight(parsed.Path, "/")
			if slices.Contains(result, normalized) {
				continue
			}
			result = append(result, normalized)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("at least one domain is required")
	}
	return result, nil
}

// isGenerated recognizes Coolify's automatic <uuid>.<wildcard> domain.
func isGenerated(domains []string, uuid string) bool {
	if len(domains) != 1 || uuid == "" {
		return false
	}
	parsed, err := url.Parse(domains[0])
	return err == nil && strings.HasPrefix(strings.ToLower(parsed.Hostname()), strings.ToLower(uuid)+".")
}
