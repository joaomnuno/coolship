package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/joaomnuno/coolship/internal/models"
)

// Domain reports the linked application's domains: a Compose application's
// service by service, any other application's as one list.
func (a *App) Domain(ctx context.Context, options Options) (DomainResult, error) {
	s, err := a.prepare(ctx, options)
	if err != nil {
		return DomainResult{}, err
	}
	application := s.project.Application
	domains := applicationDomains(application)
	urls := urlsOf(domains)
	result := DomainResult{Target: targetInfo(s.project), Domains: urls, Generated: isGenerated(urls, application.UUID), Warnings: s.warnings}
	if application.IsCompose() && len(application.ComposeDomains) > 0 {
		result.Services = domains
	}
	return result, nil
}

// DomainSet replaces the application's domains after confirmation, then reads
// the application back so the result reflects what the server kept. A
// Compose application takes SERVICE=URL pairs and has its whole per-service
// map replaced, each service keeping the redirect it has unless one is
// asked for; any other application takes URLs. The arguments are checked
// before any request, and which form the application takes once it is read.
func (a *App) DomainSet(ctx context.Context, options DomainSetOptions, confirm ConfirmDomain) (DomainSetResult, error) {
	services, domains, err := parseDomainArguments(options.Domains)
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
	application := s.project.Application
	current := applicationDomains(application)
	plan := DomainPlan{Target: targetInfo(s.project), Current: urlsOf(current), Redirect: options.Redirect}
	update := models.DomainUpdate{Force: options.Force}
	compose := application.IsCompose()
	switch {
	case compose && services == nil:
		return DomainSetResult{}, input(fmt.Errorf("%s is a Compose application, whose domains are set per service: give each one as SERVICE=URL, such as domain set %s", application.Name, composeExample(application)))
	case !compose && services != nil:
		return DomainSetResult{}, input(fmt.Errorf("SERVICE=URL pairs set the domains of a Compose application; %s takes domains such as https://app.example.com", application.Name))
	case compose:
		// Coolify stores the map as sent, so a service sent without a
		// redirect loses the one it has. The current one is carried over
		// when none is asked for, as the server itself keeps a plain
		// application's redirect when the request leaves it out.
		for i := range services {
			services[i].Redirect = options.Redirect
			if options.Redirect == "" {
				services[i].Redirect = currentRedirect(application.ComposeDomains, services[i].Name)
			}
		}
		plan.CurrentServices = current
		plan.Services = serviceDomains(services)
		plan.Domains = urlsOf(plan.Services)
		update.Services = services
	default:
		plan.Domains = domains
		update.Domains, update.Redirect = domains, options.Redirect
	}
	result := DomainSetResult{Plan: plan, Warnings: s.warnings}
	// A plain application does not report its redirect, so a request with
	// one is always sent; a Compose plan names every redirect it sends. A
	// URL with a port is always sent too: Coolify keeps the port apart from
	// the domain, where the read does not show it.
	if sameDomains(current, plan) && (options.Redirect == "" || compose) {
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
	if err := s.backend.UpdateApplicationDomains(ctx, application.UUID, update); err != nil {
		if compose {
			return result, fmt.Errorf("update domains: %w (Coolify rejects a domain in use elsewhere unless --force, and needs its copy of the compose file, reloaded from the repository in its UI, to know the services)", err)
		}
		return result, fmt.Errorf("update domains: %w (Coolify rejects a domain in use elsewhere unless --force)", err)
	}
	updated, err := s.backend.GetApplication(ctx, application.UUID)
	if err != nil {
		return result, fmt.Errorf("domains were sent but could not be read back: %w", err)
	}
	if kept := applicationDomains(updated); !keptDomains(kept, plan) {
		if compose {
			return result, fmt.Errorf("server kept %s instead of %s; Coolify drops a service its copy of the compose file does not define, so check the service names against the file, then inspect Coolify", describeDomains(kept), describeDomains(plan.Services))
		}
		return result, fmt.Errorf("server kept %s instead of the requested domains; inspect Coolify", describeDomains(kept))
	}
	result.Warnings = append(result.Warnings, "The proxy learns the new domain on the next deployment; run coolship deploy to apply it.")
	return result, nil
}

// serviceName is what a compose file may call a service, which is also
// what tells a SERVICE=URL pair from a URL with a query.
var serviceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// parseDomainArguments reads domain set's arguments as SERVICE=URL pairs, or
// as URLs, and normalizes the URLs either way. Mixing the two forms is
// refused; which one the application takes is decided once it is read. A
// service named twice gets both URLs.
func parseDomainArguments(args []string) (services []models.ComposeDomain, domains []string, err error) {
	if len(args) == 0 {
		return nil, nil, errors.New("at least one domain is required")
	}
	paired := 0
	for _, arg := range args {
		if service, _, ok := strings.Cut(arg, "="); ok && serviceName.MatchString(strings.TrimSpace(service)) {
			paired++
		}
	}
	switch {
	case paired == 0:
		domains, err = normalizeDomains(args)
		return nil, domains, err
	case paired < len(args):
		return nil, nil, errors.New("SERVICE=URL pairs and plain domains cannot be mixed: a Compose application takes pairs, any other application domains")
	}
	for _, arg := range args {
		service, value, _ := strings.Cut(arg, "=")
		service = strings.TrimSpace(service)
		urls, err := normalizeDomains([]string{value})
		if err != nil {
			return nil, nil, fmt.Errorf("service %s: %w", service, err)
		}
		index := slices.IndexFunc(services, func(entry models.ComposeDomain) bool { return entry.Name == service })
		if index < 0 {
			services = append(services, models.ComposeDomain{Name: service, Domain: strings.Join(urls, ",")})
			continue
		}
		merged := strings.Split(services[index].Domain, ",")
		for _, url := range urls {
			if !slices.Contains(merged, url) {
				merged = append(merged, url)
			}
		}
		services[index].Domain = strings.Join(merged, ",")
	}
	return services, nil, nil
}

// serviceDomains lists the URLs a per-service map names, one entry per URL,
// as the plan and the result show them.
func serviceDomains(services []models.ComposeDomain) []ServiceDomain {
	var result []ServiceDomain
	for _, service := range services {
		for _, url := range strings.Split(service.Domain, ",") {
			result = append(result, ServiceDomain{Service: service.Name, URL: url, Redirect: service.Redirect})
		}
	}
	return result
}

// sameDomains reports whether the domains an application has are the ones a
// plan asks for: the same URLs in the same order, and for a Compose
// application under the same services with the same redirects, since the
// plan names the redirect each service is sent, the current one when none
// was asked for. A plain application's redirect is not compared: the plan
// leaves it out to keep whatever is set, and the application does not
// report it.
func sameDomains(domains []ServiceDomain, plan DomainPlan) bool {
	return matchDomains(domains, plan, func(have, want string) bool { return have == want })
}

// keptDomains reports whether the domains read back after an update are the
// ones the plan sent, as sameDomains does, except that a URL kept without
// the port it was sent with counts: Coolify keeps the port apart from the
// domain, as the container port the domain routes to, and reports the
// domain without it.
func keptDomains(domains []ServiceDomain, plan DomainPlan) bool {
	return matchDomains(domains, plan, func(have, want string) bool { return have == want || have == withoutPort(want) })
}

func matchDomains(domains []ServiceDomain, plan DomainPlan, sameURL func(have, want string) bool) bool {
	if plan.Services == nil {
		return slices.EqualFunc(urlsOf(domains), plan.Domains, sameURL)
	}
	return slices.EqualFunc(domains, plan.Services, func(have, want ServiceDomain) bool {
		return have.Service == want.Service && sameURL(have.URL, want.URL) && have.Redirect == want.Redirect
	})
}

// withoutPort is a normalized URL without its port, or the URL itself when
// it has none.
func withoutPort(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Port() == "" {
		return rawURL
	}
	return parsed.Scheme + "://" + strings.TrimSuffix(parsed.Host, ":"+parsed.Port()) + parsed.Path
}

// currentRedirect is the redirect a Compose application's service has now,
// or empty for a service without one or without a domain.
func currentRedirect(domains models.ComposeDomains, service string) string {
	for _, entry := range domains {
		if entry.Name == service {
			return entry.Redirect
		}
	}
	return ""
}

// describeDomains names domains in an error, as the user would type them,
// with the redirect a Compose service has beside it.
func describeDomains(domains []ServiceDomain) string {
	if len(domains) == 0 {
		return "no domain"
	}
	var parts []string
	for _, domain := range domains {
		part := domain.URL
		if domain.Service != "" {
			part = domain.Service + "=" + domain.URL
		}
		if domain.Redirect != "" {
			part += " (redirect " + domain.Redirect + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// composeExample shows the SERVICE=URL form with the application's own
// services when it has any with a domain, so the message names what to
// type; a service is otherwise named as the compose file would.
func composeExample(application models.Application) string {
	var names []string
	for _, entry := range application.ComposeDomains {
		if entry.Name != "" && !slices.Contains(names, entry.Name) {
			names = append(names, entry.Name)
		}
	}
	if len(names) == 0 {
		return "web=https://app.example.com, naming each service as the compose file does"
	}
	var pairs []string
	for _, name := range names {
		pairs = append(pairs, name+"=https://"+name+".example.com")
	}
	return strings.Join(pairs, " ") + " (its services with a domain now: " + strings.Join(names, ", ") + ")"
}

// domainHost is a hostname a proxy can route: labels of letters, digits,
// hyphens, and underscores joined by dots. It is what tells https://= or a
// stray second = in a SERVICE=URL pair from a domain before anything is
// sent; an IP address is accepted beside it.
var domainHost = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*\.?$`)

// normalizeDomains accepts full web URLs, or bare hosts as https, and rejects
// anything a proxy could not route: another scheme, a host that is not a
// hostname or an IP address, a port outside 1 to 65535, credentials, a
// query, or a fragment.
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
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !routableHost(parsed.Hostname()) ||
				parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(item, " \t\r\n") {
				return nil, fmt.Errorf("domain %q must be a plain http or https URL such as https://app.example.com", item)
			}
			if port := parsed.Port(); port != "" {
				if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
					return nil, fmt.Errorf("domain %q has port %s; a port is a number from 1 to 65535", item, port)
				}
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

// routableHost reports whether a URL's host is a hostname or an IP address.
func routableHost(host string) bool {
	return domainHost.MatchString(host) || net.ParseIP(host) != nil
}

// isGenerated recognizes Coolify's automatic <uuid>.<wildcard> domain.
func isGenerated(domains []string, uuid string) bool {
	if len(domains) != 1 || uuid == "" {
		return false
	}
	parsed, err := url.Parse(domains[0])
	return err == nil && strings.HasPrefix(strings.ToLower(parsed.Hostname()), strings.ToLower(uuid)+".")
}
