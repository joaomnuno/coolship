// Package gitinfo reads the identity of the repository a directory belongs
// to: its public remote and the checked-out branch. It runs git and nothing
// else, so a workflow can be handed the answer through an injected function.
package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
)

// Repository is what init needs to create an application: where Coolify will
// clone from and which branch it will build.
type Repository struct {
	Remote string // normalized https URL without a .git suffix
	Branch string
}

// Inspect reads the origin remote and the current branch of the repository
// containing dir. A detached HEAD or a missing remote is an error the caller
// can turn into a request for explicit flags.
func Inspect(ctx context.Context, dir string) (Repository, error) {
	remote, err := run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return Repository{}, fmt.Errorf("read the origin remote: %w", err)
	}
	normalized, err := NormalizeRemote(remote)
	if err != nil {
		return Repository{}, err
	}
	// symbolic-ref answers on a branch with no commits yet, and fails on a
	// detached HEAD, where rev-parse would answer "HEAD".
	branch, err := run(ctx, dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		if strings.Contains(err.Error(), "not a symbolic ref") {
			return Repository{}, errors.New("HEAD is detached; check out a branch or pass --branch")
		}
		return Repository{}, fmt.Errorf("read the current branch: %w", err)
	}
	return Repository{Remote: normalized, Branch: branch}, nil
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), firstLine(detail))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func firstLine(text string) string {
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		return text[:index]
	}
	return text
}

// scpStyle matches git@host:owner/repo(.git) and user@host:path forms.
var scpStyle = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?([A-Za-z0-9.-]+):([^/].*)$`)

// NormalizeRemote turns any common remote form into the https URL Coolify
// clones public repositories from: git@github.com:owner/repo.git,
// ssh://git@github.com/owner/repo, and https://github.com/owner/repo.git all
// become https://github.com/owner/repo. Anything that is not a web URL to a
// host — a local path, an unknown scheme, a URL with credentials — is
// rejected, since Coolify could not clone it without further setup.
func NormalizeRemote(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("repository URL is empty")
	}
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", errors.New("repository URL must not contain whitespace")
	}
	var host, path string
	switch {
	case strings.Contains(value, "://"):
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" {
			return "", fmt.Errorf("repository URL %q is not a valid URL", value)
		}
		switch parsed.Scheme {
		case "https", "http", "ssh", "git", "git+ssh", "ssh+git":
		default:
			return "", fmt.Errorf("repository URL %q uses scheme %q; only https and ssh remotes can be converted", value, parsed.Scheme)
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("repository URL %q must not contain a query or fragment", value)
		}
		host, path = strings.ToLower(parsed.Hostname()), parsed.Path
	default:
		match := scpStyle.FindStringSubmatch(value)
		if match == nil {
			return "", fmt.Errorf("repository %q is not a remote URL; pass --repo with an https URL", value)
		}
		host, path = strings.ToLower(match[1]), "/"+match[2]
	}
	path = strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
	path = strings.TrimRight(path, "/")
	if strings.Count(strings.Trim(path, "/"), "/") < 1 || strings.Contains(path, "..") {
		return "", fmt.Errorf("repository URL %q must name an owner and a repository", value)
	}
	for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
		if segment == "" {
			return "", fmt.Errorf("repository URL %q must name an owner and a repository", value)
		}
	}
	return "https://" + host + path, nil
}

// Name is the repository's own name, the last path segment of its remote.
func Name(remote string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}
