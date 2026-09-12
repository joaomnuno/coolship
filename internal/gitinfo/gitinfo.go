// Package gitinfo reads the identity of the repository a directory belongs
// to: its remote, in the forms Coolify clones from, and the checked-out
// branch. It runs git and nothing else, so a workflow can be handed the
// answer through an injected function.
package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Repository is what init needs to create an application: where Coolify will
// clone from and which branch it will build. Remote is the https form an
// anonymous clone or a GitHub App uses; SSH is the form a deploy key clones
// over, kept as the remote was written when it already was an SSH remote.
type Repository struct {
	Remote string // normalized https URL without a .git suffix
	SSH    string // user@host:path.git, or user@host:port/path.git with a port
	Branch string
}

// Inspect reads the origin remote and the current branch of the repository
// containing dir. A detached HEAD or a missing remote is an error the caller
// can turn into a request for explicit flags.
func Inspect(ctx context.Context, dir string) (Repository, error) {
	remote, err := run(ctx, dir, nil, "remote", "get-url", "origin")
	if err != nil {
		return Repository{}, fmt.Errorf("read the origin remote: %w", err)
	}
	repository, err := Parse(remote)
	if err != nil {
		return Repository{}, err
	}
	// symbolic-ref answers on a branch with no commits yet, and fails on a
	// detached HEAD, where rev-parse would answer "HEAD".
	branch, err := run(ctx, dir, nil, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		if strings.Contains(err.Error(), "not a symbolic ref") {
			return Repository{}, errors.New("HEAD is detached; check out a branch or pass --branch")
		}
		return Repository{}, fmt.Errorf("read the current branch: %w", err)
	}
	repository.Branch = branch
	return repository, nil
}

// Parse derives both clone forms from one remote as written.
func Parse(remote string) (Repository, error) {
	https, err := NormalizeRemote(remote)
	if err != nil {
		return Repository{}, err
	}
	ssh, err := SSHRemote(remote)
	if err != nil {
		return Repository{}, err
	}
	return Repository{Remote: https, SSH: ssh}, nil
}

// Heads lists the branches of a remote as an anonymous client sees them, or
// fails when the remote cannot be read without credentials. The probe runs
// in an empty directory that is also its home, with the user's git
// configuration, credential helpers, and askpass programs disabled and git's
// own environment variables dropped, so nothing stored on this machine — a
// credential helper, ~/.netrc, ~/.git-credentials, an http.extraHeader in a
// checkout or in GIT_CONFIG_PARAMETERS, the SSH agent — can make a private
// repository look public. The remote must be a URL, never a local path or an
// option.
func Heads(ctx context.Context, remote string) ([]string, error) {
	if !strings.Contains(remote, "://") || strings.HasPrefix(remote, "-") {
		return nil, fmt.Errorf("remote %q is not a URL", redact(remote))
	}
	home, err := os.MkdirTemp("", "coolship-probe-")
	if err != nil {
		return nil, fmt.Errorf("prepare the anonymous probe: %w", err)
	}
	defer os.RemoveAll(home)
	out, err := run(ctx, home, probeEnvironment(home), "-c", "credential.helper=", "-c", "core.askPass=", "-c", "http.extraHeader=", "ls-remote", "--heads", "--", remote)
	if err != nil {
		var failure *commandError
		if errors.As(err, &failure) {
			return nil, fmt.Errorf("git ls-remote: %s", failure.detail)
		}
		return nil, err
	}
	var heads []string
	for _, line := range strings.Split(out, "\n") {
		_, ref, found := strings.Cut(line, "\t")
		if found && strings.HasPrefix(ref, "refs/heads/") {
			heads = append(heads, strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	return heads, nil
}

// probeEnvironment is the process environment with everything git could take
// a login from removed: git's own variables (a repository through GIT_DIR,
// configuration through GIT_CONFIG_PARAMETERS, a client certificate), the
// home directory that holds ~/.netrc and ~/.git-credentials, and the SSH
// agent. Trust settings for a private certificate authority and tracing are
// kept, proxies are inherited, and the given directory becomes the home and
// the ceiling of repository discovery.
func probeEnvironment(home string) []string {
	kept := []string{"GIT_EXEC_PATH", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH", "GIT_SSL_NO_VERIFY", "GIT_TRACE", "GIT_TRACE_CURL", "GIT_CURL_VERBOSE"}
	dropped := []string{"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "XDG_CONFIG_HOME", "SSH_AUTH_SOCK"}
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if (strings.HasPrefix(name, "GIT_") && !slices.Contains(kept, name)) || slices.Contains(dropped, name) {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+home, "GIT_CEILING_DIRECTORIES="+home,
		"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}

// waitDelay bounds how long a finished or killed git may keep Run waiting on
// its pipes. git talks to a remote through a child, git-remote-https, which
// inherits the pipes and can outlive a killed git; without the delay, a
// cancelled probe would wait for it instead of returning at the deadline.
const waitDelay = time.Second

func run(ctx context.Context, dir string, environment []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	command.Env = environment
	command.WaitDelay = waitDelay
	isolate(command)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		detail := firstLine(strings.TrimSpace(stderr.String()))
		if detail == "" {
			detail = err.Error()
		}
		return "", &commandError{args: args, detail: detail}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// commandError is a git command that ran and failed, with what it said.
type commandError struct {
	args   []string
	detail string
}

func (e *commandError) Error() string { return "git " + strings.Join(e.args, " ") + ": " + e.detail }

func firstLine(text string) string {
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		return text[:index]
	}
	return text
}

// scpStyle matches git@host:owner/repo(.git) and user@host:path forms.
var scpStyle = regexp.MustCompile(`^(?:([A-Za-z0-9._-]+)@)?([A-Za-z0-9.-]+):([^/].*)$`)

// urlUserinfo is the userinfo of a URL-style remote, scheme://user:token@,
// and scpUserinfo the user:token@ of anything written in the scp-style form.
// Both reach as far as the last @ before the path, so a secret containing an
// @ leaves nothing behind.
var (
	urlUserinfo = regexp.MustCompile(`^([A-Za-z0-9+.-]+://)[^/]*@`)
	scpUserinfo = regexp.MustCompile(`^[^/@]*:[^/]*@`)
)

// redact is a remote as it can be shown in an error: a URL-style remote loses
// its userinfo entirely, since a login there is a password or a token and is
// dropped from the clone forms anyway, and an scp-style remote loses a
// user:token@ prefix while a plain git@ stays, because an SSH remote's user
// is not a secret.
func redact(value string) string {
	if strings.Contains(value, "://") {
		return urlUserinfo.ReplaceAllString(value, "$1")
	}
	return scpUserinfo.ReplaceAllString(value, "redacted@")
}

// defaultPorts is the port each URL scheme already means.
var defaultPorts = map[string]string{"https": "443", "http": "80", "git": "9418"}

// remote is one parsed remote: the pieces both clone forms are built from.
type remote struct {
	user, host, port, path string
	ssh                    bool // written as an SSH remote (scp-style or ssh://)
}

// parse accepts any common remote form. Anything that is not a URL to a host
// — a local path, an unknown scheme — is rejected, since Coolify could not
// clone it without further setup; credentials in a URL are dropped.
func parse(raw string) (remote, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return remote{}, errors.New("repository URL is empty")
	}
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return remote{}, errors.New("repository URL must not contain whitespace")
	}
	// Errors quote the remote without its credentials: a validation failure
	// is printed, logged, and pasted into issues.
	shown := redact(value)
	var parsed remote
	switch {
	case strings.Contains(value, "://"):
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" {
			return remote{}, fmt.Errorf("repository URL %q is not a valid URL", shown)
		}
		switch u.Scheme {
		case "https", "http", "git":
			// A port the scheme already implies says nothing; any other
			// one is part of the address and has to survive normalization.
			if port := u.Port(); port != defaultPorts[u.Scheme] {
				parsed.port = port
			}
		case "ssh", "git+ssh", "ssh+git":
			parsed.ssh = true
			parsed.user, parsed.port = u.User.Username(), u.Port()
		default:
			return remote{}, fmt.Errorf("repository URL %q uses scheme %q; only https and ssh remotes can be converted", shown, u.Scheme)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return remote{}, fmt.Errorf("repository URL %q must not contain a query or fragment", shown)
		}
		parsed.host, parsed.path = strings.ToLower(u.Hostname()), u.Path
	default:
		match := scpStyle.FindStringSubmatch(value)
		if match == nil {
			return remote{}, fmt.Errorf("repository %q is not a remote URL; pass --repo with an https URL", shown)
		}
		parsed.ssh = true
		parsed.user, parsed.host, parsed.path = match[1], strings.ToLower(match[2]), "/"+match[3]
	}
	path := strings.TrimSuffix(strings.TrimRight(parsed.path, "/"), ".git")
	path = strings.Trim(path, "/")
	if strings.Count(path, "/") < 1 || strings.Contains(path, "..") {
		return remote{}, fmt.Errorf("repository URL %q must name an owner and a repository", shown)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" {
			return remote{}, fmt.Errorf("repository URL %q must name an owner and a repository", shown)
		}
	}
	parsed.path = path
	return parsed, nil
}

// NormalizeRemote turns any common remote form into the https URL Coolify
// clones public repositories from: git@github.com:owner/repo.git,
// ssh://git@github.com/owner/repo, and https://github.com/owner/repo.git all
// become https://github.com/owner/repo. A forge served on a port of its own,
// https://gitea.example.com:8443/owner/repo, keeps it; an SSH port does not
// carry over, because it addresses a different service.
func NormalizeRemote(raw string) (string, error) {
	parsed, err := parse(raw)
	if err != nil {
		return "", err
	}
	host := parsed.host
	if !parsed.ssh && parsed.port != "" {
		host += ":" + parsed.port
	}
	return "https://" + host + "/" + parsed.path, nil
}

// SSHRemote turns any common remote form into the SSH URL a deploy key clones
// over. A remote that already is one keeps its user and port:
// git@github.com:owner/repo.git stays as it is, ssh://git@host:2222/owner/repo
// becomes git@host:2222/owner/repo.git (the form Coolify parses a port from),
// and https://github.com/owner/repo becomes git@github.com:owner/repo.git.
func SSHRemote(raw string) (string, error) {
	parsed, err := parse(raw)
	if err != nil {
		return "", err
	}
	user := parsed.user
	if user == "" {
		user = "git"
	}
	if parsed.ssh && parsed.port != "" {
		return user + "@" + parsed.host + ":" + parsed.port + "/" + parsed.path + ".git", nil
	}
	return user + "@" + parsed.host + ":" + parsed.path + ".git", nil
}

// Name is the repository's own name, the last path segment of its remote.
func Name(remote string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(remote, "/"), ".git")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}

// Path is the owner/repository part of a remote in either form, which is how
// a GitHub App names the repositories it can reach.
func Path(remote string) string {
	parsed, err := parse(remote)
	if err != nil {
		return ""
	}
	return parsed.path
}

// Host is the lowercase host of a remote in either form.
func Host(remote string) string {
	parsed, err := parse(remote)
	if err != nil {
		return ""
	}
	return parsed.host
}
