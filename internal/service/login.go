package service

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/problem"
)

// CloudURL is Coolify Cloud's instance URL, the one login offers by name.
const CloudURL = "https://app.coolify.io"

// NormalizeInstanceURL applies the instance URL rule a login must satisfy, so
// a prompt can check an answer before the token is asked for.
func NormalizeInstanceURL(raw string) (string, error) {
	return auth.NormalizeURL(strings.TrimSpace(raw))
}

// SuggestContextName proposes a name for the instance at address: "cloud"
// for Coolify Cloud, otherwise the first label of the host, so
// coolify.example.com becomes "coolify". A name already saved gains a number.
func SuggestContextName(address string, saved []SavedContext) string {
	name := ""
	if normalized, err := NormalizeInstanceURL(address); err == nil && normalized == CloudURL {
		name = "cloud"
	} else if parsed, err := url.Parse(strings.TrimSpace(address)); err == nil && parsed.Hostname() != "" {
		name, _, _ = strings.Cut(parsed.Hostname(), ".")
		if name == "" {
			name = parsed.Hostname()
		}
	}
	if name == "" {
		return ""
	}
	taken := map[string]bool{}
	for _, existing := range saved {
		taken[existing.Name] = true
	}
	candidate := name
	for n := 2; taken[candidate]; n++ {
		candidate = name + "-" + strconv.Itoa(n)
	}
	return candidate
}

// SavedContexts lists the contexts in the Coolify CLI configuration, names and
// URLs only, for a login to show before it asks anything. A missing or
// unreadable file lists nothing; saving reports its problem, if any.
func (a *App) SavedContexts(configPath string) []SavedContext {
	report := a.deps.InspectCredentials(auth.Options{ConfigPath: configPath})
	contexts := make([]SavedContext, 0, len(report.Instances))
	for _, instance := range report.Instances {
		contexts = append(contexts, SavedContext{Name: instance.Name, URL: instance.URL, Default: instance.Default})
	}
	return contexts
}

// CheckInstance is a login's first check, made before any token is asked
// for: the URL follows the instance URL rule, and Coolify answers its public
// health check there. It returns the normalized URL.
func (a *App) CheckInstance(ctx context.Context, raw string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	address, err := NormalizeInstanceURL(raw)
	if err != nil {
		return "", input(err)
	}
	if a.deps.CheckHealth == nil {
		return "", errors.New("Coolify health check is not configured")
	}
	if err := a.deps.CheckHealth(ctx, address); err != nil {
		return "", restate(err, "no Coolify answered at %s: %s", address, describeLoginFailure(err))
	}
	return address, nil
}

// CheckLogin is a login's second check, made before anything is written: the
// context name and token are well formed, the file does not already hold this
// URL with this token, and the server accepts the token (GET /version, then
// the token's team). options.URL must have passed CheckInstance.
func (a *App) CheckLogin(ctx context.Context, options LoginOptions) (LoginCheck, error) {
	if err := ctx.Err(); err != nil {
		return LoginCheck{}, err
	}
	address, err := NormalizeInstanceURL(options.URL)
	if err != nil {
		return LoginCheck{}, input(err)
	}
	if err := auth.ValidateName(options.Name); err != nil {
		return LoginCheck{}, input(err)
	}
	token := strings.TrimSpace(options.Token)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return LoginCheck{}, input(errors.New("an API token is required; create one in Coolify under Keys & Tokens"))
	}
	match, err := a.deps.FindLogin(options.ConfigPath, address, token)
	if err != nil {
		return LoginCheck{}, input(err)
	}
	if match.Duplicate != "" && !a.makesDefault(options, match.Duplicate) {
		return LoginCheck{}, input(&auth.DuplicateLoginError{Name: match.Duplicate, URL: address})
	}
	backend, err := a.backend(auth.Credentials{Name: options.Name, URL: address, Token: token})
	if err != nil {
		return LoginCheck{}, err
	}
	version, err := backend.Version(ctx)
	if err != nil {
		return LoginCheck{}, restate(err, "could not verify %s: %s", address, describeLoginFailure(err))
	}
	team, err := backend.Team(ctx)
	if err != nil {
		return LoginCheck{}, restate(err, "token was not accepted by %s: %s", address, describeLoginFailure(err))
	}
	return LoginCheck{Server: version, Team: team.Name}, nil
}

// makesDefault reports whether saving a login the file already holds under
// duplicate still changes something: --default for that same context when it
// is not the default yet.
func (a *App) makesDefault(options LoginOptions, duplicate string) bool {
	if !options.Default || options.Name != duplicate {
		return false
	}
	return a.deps.InspectCredentials(auth.Options{ConfigPath: options.ConfigPath}).Default != duplicate
}

// SaveLogin writes a checked login to the Coolify CLI configuration, so both
// tools share it, and reports what was saved. A context with the same name is
// replaced.
func (a *App) SaveLogin(ctx context.Context, options LoginOptions, check LoginCheck) (LoginResult, error) {
	if err := ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	address, err := NormalizeInstanceURL(options.URL)
	if err != nil {
		return LoginResult{}, input(err)
	}
	token := strings.TrimSpace(options.Token)
	replaced := false
	for _, saved := range a.SavedContexts(options.ConfigPath) {
		if saved.Name == options.Name {
			replaced = true
		}
	}
	path, err := a.deps.SaveCredentials(options.ConfigPath, auth.Stored{Name: options.Name, URL: address, Token: token}, options.Default)
	if err != nil {
		return LoginResult{}, input(err)
	}
	saved := a.deps.InspectCredentials(auth.Options{ConfigPath: path})
	return LoginResult{Name: options.Name, URL: address, Path: path, Default: saved.Default == options.Name, Server: check.Server, Team: check.Team, Replaced: replaced}, nil
}

// Login runs a whole login without asking anything: the instance check, the
// token check, then the save. The token is only written after the server has
// accepted it.
func (a *App) Login(ctx context.Context, options LoginOptions) (LoginResult, error) {
	address, err := a.CheckInstance(ctx, options.URL)
	if err != nil {
		return LoginResult{}, err
	}
	options.URL = address
	check, err := a.CheckLogin(ctx, options)
	if err != nil {
		return LoginResult{}, err
	}
	return a.SaveLogin(ctx, options, check)
}

// describeLoginFailure words a failed verification request. A failure the
// error catalog knows keeps its own text, because the executable boundary
// prints the catalog's hint beside it; anything else gains the server hint.
func describeLoginFailure(err error) string {
	if _, ok := problem.Classify(err); ok {
		return err.Error()
	}
	return describeServerError(err)
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
