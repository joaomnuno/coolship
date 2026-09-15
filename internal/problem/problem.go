// Package problem is the catalog of failures Coolship knows how to explain.
// Each entry has a stable snake_case code, who can fix it (Coolify's setup or
// Coolship's), a fixed hint, a documentation link, and the fix Coolship could
// offer. Classify recognizes the typed errors of the lower layers; the
// executable boundary prints the result once.
package problem

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
)

// Code is a stable identifier printed as "Error [code]:" and in the JSON
// error object. Codes are never renamed; a retired one is left unused.
type Code string

// Catalogued codes, each with a heading on the errors documentation page.
const (
	CodeNoCredentials       Code = "no_credentials"
	CodeUnknownContext      Code = "unknown_context"
	CodeNoDefaultContext    Code = "no_default_context"
	CodeInvalidCredentials  Code = "invalid_credentials"
	CodeNotLinked           Code = "not_linked"
	CodeTargetNotSelected   Code = "target_not_selected"
	CodeInvalidConfig       Code = "invalid_config"
	CodeConfigChanged       Code = "config_changed"
	CodeBindingExists       Code = "binding_exists"
	CodeResourceNotFound    Code = "resource_not_found"
	CodeResourceAmbiguous   Code = "resource_ambiguous"
	CodeIdentityMismatch    Code = "identity_mismatch"
	CodeUnauthorized        Code = "unauthorized"
	CodeAPIDisabled         Code = "api_disabled"
	CodeIPNotAllowed        Code = "ip_not_allowed"
	CodeMissingPermissions  Code = "missing_permissions"
	CodeTokenExceedsRole    Code = "token_exceeds_role"
	CodeForbidden           Code = "forbidden"
	CodeRedirect            Code = "redirect"
	CodeRateLimited         Code = "rate_limited"
	CodeDeploymentQueueFull Code = "deployment_queue_full"
	CodeServerError         Code = "server_error"
	CodeAppNotRunning       Code = "app_not_running"
	CodeHostNotFound        Code = "host_not_found"
	CodeConnectionRefused   Code = "connection_refused"
	CodeTLSError            Code = "tls_error"
	CodeRequestTimeout      Code = "request_timeout"
	CodeNetworkError        Code = "network_error"
	CodeUnexpectedResponse  Code = "unexpected_response"
	CodeUncertainSubmission Code = "uncertain_submission"
	CodeDeploymentTimeout   Code = "deployment_timeout"
)

// Generic codes carry failures without a catalog entry. They appear only in
// the JSON error object; the text keeps its plain "Error:" line.
const (
	CodeInvalidInput Code = "invalid_input"
	CodeUnclassified Code = "unclassified"
	CodeInterrupted  Code = "interrupted"
	CodeCancelled    Code = "cancelled"
	CodeChecksFailed Code = "checks_failed"
)

// Owner says whose setup the fix belongs to.
type Owner string

const (
	OwnerCoolify  Owner = "coolify"
	OwnerCoolship Owner = "coolship"
)

// FixKind is the remedy Coolship could offer for a problem in a terminal.
type FixKind int

const (
	FixNone FixKind = iota
	FixLogin
	FixReLogin
	FixLink
	FixPickContext
	FixOpenURL
)

// Problem is one classified failure. Message is the error's own text, less
// any advice the Hint now gives. FixURL is set for FixOpenURL: the Coolify
// page on the instance that answered. Context names the context involved,
// when the error says which. Instance is the URL of the instance that
// answered, when a response says which, so a fix can log in to it again.
type Problem struct {
	Code     Code
	Owner    Owner
	Message  string
	Hint     string
	DocsURL  string
	Fix      FixKind
	FixURL   string
	Context  string
	Instance string
}

// DocsBase is the Coolship errors page; each code is a heading on it.
const DocsBase = "https://coolship.itrocas.com/docs/platform/errors"

type entry struct {
	owner Owner
	hint  string
	docs  string // a Coolify page; empty means the Coolship errors page
	fix   FixKind
	page  string // path on the instance for FixOpenURL
}

const (
	coolifyAPIAccess   = "https://coolify.io/docs/api/ip-allowlist"
	coolifyPermissions = "https://coolify.io/docs/api/permissions"
	coolifyRateLimits  = "https://coolify.io/docs/api/rate-limits"
	settingsAdvanced   = "/settings/advanced"
	apiTokens          = "/security/api-tokens"
)

var catalog = map[Code]entry{
	CodeNoCredentials: {owner: OwnerCoolship, fix: FixLogin,
		hint: "Run coolship login to save a Coolify URL and API token, or set COOLSHIP_URL and COOLSHIP_TOKEN."},
	CodeUnknownContext: {owner: OwnerCoolship, fix: FixPickContext,
		hint: "Pass --context with one of the saved contexts, or run coolship login to save this one."},
	CodeNoDefaultContext: {owner: OwnerCoolship, fix: FixPickContext,
		hint: "Pass --context NAME, or run coolship login --default to make a saved context the default."},
	CodeInvalidCredentials: {owner: OwnerCoolship,
		hint: "Check COOLSHIP_URL and COOLSHIP_TOKEN, or fix the Coolify CLI configuration file; coolship login rewrites a context."},
	CodeNotLinked: {owner: OwnerCoolship, fix: FixLink,
		hint: "Run coolship link to use an existing Coolify application, or coolship init to create one."},
	CodeTargetNotSelected: {owner: OwnerCoolship,
		hint: "Targets are the [apps.<name>] tables in coolship.toml; a file with a single [project] table takes no target name."},
	CodeInvalidConfig: {owner: OwnerCoolship,
		hint: "Fix coolship.toml by hand, or run coolship link --replace to write the binding again."},
	CodeConfigChanged: {owner: OwnerCoolship,
		hint: "coolship.toml changed while the command ran; review the file and run the command again."},
	CodeBindingExists: {owner: OwnerCoolship,
		hint: "Pass --replace to overwrite the binding (--yes for unlink), or run coolship unlink first."},
	CodeResourceNotFound: {owner: OwnerCoolship,
		hint: "Check the names and UUIDs in coolship.toml, or run coolship link to bind the application again."},
	CodeResourceAmbiguous: {owner: OwnerCoolship,
		hint: "Several resources share that name; run coolship link to pin the UUID of the one you mean."},
	CodeIdentityMismatch: {owner: OwnerCoolship,
		hint: "Coolify answered for a different resource than the one asked for; check the application in Coolify, then run coolship link again."},
	CodeUnauthorized: {owner: OwnerCoolship, fix: FixReLogin,
		hint: "The server rejected the token: it may be revoked, expired, or from another instance. Run coolship login to save a new one, or check COOLSHIP_TOKEN."},
	CodeAPIDisabled: {owner: OwnerCoolify, docs: coolifyAPIAccess, fix: FixOpenURL, page: settingsAdvanced,
		hint: "API access is turned off on this Coolify instance. An instance admin can turn it on in Coolify under Settings, Advanced, API access."},
	CodeIPNotAllowed: {owner: OwnerCoolify, docs: coolifyAPIAccess, fix: FixOpenURL, page: settingsAdvanced,
		hint: "This machine's IP address is not on the instance's API allowlist. An instance admin can add it in Coolify under Settings, Advanced, Allowed API IPs."},
	CodeMissingPermissions: {owner: OwnerCoolify, docs: coolifyPermissions, fix: FixOpenURL, page: apiTokens,
		hint: "The API token lacks a permission this command needs. Create a token with read, write, and deploy (build logs and secret values also need read:sensitive) under Security, API Tokens, then run coolship login."},
	CodeTokenExceedsRole: {owner: OwnerCoolify, docs: coolifyPermissions, fix: FixOpenURL, page: apiTokens,
		hint: "Team members may only hold read-only tokens. Ask a team admin for a higher role, or create a token with read permissions only under Security, API Tokens, then run coolship login."},
	CodeForbidden: {owner: OwnerCoolify, docs: coolifyPermissions,
		hint: "The token lacks a required ability, or a proxy in front of Coolify refused the request. Coolship needs read, write, and deploy (build logs and secret values also need read:sensitive)."},
	CodeRedirect: {owner: OwnerCoolship,
		hint: "The instance URL redirects, and Coolship does not follow redirects, so the token is never sent elsewhere. Run coolship login with the final https URL."},
	CodeRateLimited: {owner: OwnerCoolify, docs: coolifyRateLimits,
		hint: "Coolify is limiting how often this token may call the API; wait a minute, then retry."},
	CodeDeploymentQueueFull: {owner: OwnerCoolify,
		hint: "Wait for a running deployment to finish, then retry; each server in Coolify sets how many builds run at once."},
	CodeServerError: {owner: OwnerCoolify,
		hint: "Coolify failed while handling the request. Retry, and check the Coolify logs if it keeps failing."},
	CodeAppNotRunning: {owner: OwnerCoolship,
		hint: "Deploy or start the application (coolship deploy, coolship start) before reading its logs; right after a deployment, retry in a moment."},
	CodeHostNotFound: {owner: OwnerCoolship,
		hint: "Check the instance URL (coolship doctor shows the one in use) and your DNS."},
	CodeConnectionRefused: {owner: OwnerCoolship,
		hint: "Check the instance URL and port, and that Coolify is running."},
	CodeTLSError: {owner: OwnerCoolship,
		hint: "Check the instance URL: http or https, and a host name its certificate covers."},
	CodeRequestTimeout: {owner: OwnerCoolship,
		hint: "Check the instance URL and the network, then retry."},
	CodeNetworkError: {owner: OwnerCoolship,
		hint: "The request got no response. Check the network and any proxy between this machine and Coolify, then retry."},
	CodeUnexpectedResponse: {owner: OwnerCoolship,
		hint: "Coolify answered in a shape Coolship does not expect. Check that the instance runs a supported Coolify version (coolship doctor)."},
	CodeUncertainSubmission: {owner: OwnerCoolship,
		hint: "Check the application's deployments (coolship deployments) before deploying again, so the same commit is not queued twice."},
	CodeDeploymentTimeout: {owner: OwnerCoolship,
		hint: "The deployment continues on the server. Follow it with coolship deployments or in Coolify, or pass a longer --timeout."},
}

// Codes lists every catalogued code, sorted.
func Codes() []Code {
	codes := make([]Code, 0, len(catalog))
	for code := range catalog {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}

// GenericCodes lists the codes the JSON error object uses for failures
// outside the catalog.
func GenericCodes() []Code {
	return []Code{CodeInvalidInput, CodeUnclassified, CodeInterrupted, CodeCancelled, CodeChecksFailed}
}

// Lookup returns the catalog entry for code, without a message.
func Lookup(code Code) (Problem, bool) {
	e, ok := catalog[code]
	if !ok {
		return Problem{}, false
	}
	docs := e.docs
	if docs == "" {
		docs = DocsBase + "#" + string(code)
	}
	return Problem{Code: code, Owner: e.owner, Hint: e.hint, DocsURL: docs, Fix: e.fix}, true
}

// Classify recognizes a catalogued failure anywhere in err's chain. An
// interruption, and any error the catalog does not know, reports false. An
// interruption after a deployment request was sent is still an uncertain
// submission: the server may have accepted it, so a retry must check first.
func Classify(err error) (Problem, bool) {
	if err == nil {
		return Problem{}, false
	}
	var uncertain *coolify.UncertainSubmissionError
	if errors.Is(err, context.Canceled) && !errors.As(err, &uncertain) {
		return Problem{}, false
	}
	found, ok := recognize(err)
	if !ok {
		return Problem{}, false
	}
	p, _ := Lookup(found.code)
	p.Message = err.Error()
	for _, advice := range found.advice {
		if strings.Contains(p.Message, advice) {
			p.Message = strings.Replace(p.Message, advice, "", 1)
			break
		}
	}
	p.Context = found.context
	p.Instance = found.instance
	if p.Fix == FixOpenURL {
		if found.instance == "" {
			p.Fix = FixNone
		} else {
			p.FixURL = strings.TrimRight(found.instance, "/") + catalog[found.code].page
		}
	}
	return p, true
}

// recognized is what an error in the chain says beyond its code: advice its
// text carries that the hint replaces (the first one found is removed), the
// instance that answered, and the context named.
type recognized struct {
	code     Code
	advice   []string
	instance string
	context  string
}

func recognize(err error) (recognized, bool) {
	var timeout interface{ DeploymentTimeout() time.Duration }
	var uncertain *coolify.UncertainSubmissionError
	var notRunning *coolify.NotRunningError
	var httpErr *coolify.HTTPError
	var requestErr *coolify.RequestError
	var protocol *coolify.ProtocolError
	var missing *auth.MissingCredentialsError
	var unknownContext *auth.ContextNotFoundError
	var noDefault *auth.NoDefaultContextError
	var missingResource *resolver.MissingError
	var ambiguous *resolver.AmbiguousError
	var identity *resolver.IdentityError
	switch {
	case errors.As(err, &timeout):
		return recognized{code: CodeDeploymentTimeout, advice: []string{"; it continues on the server"}}, true
	case errors.As(err, &uncertain):
		return recognized{code: CodeUncertainSubmission}, true
	case errors.As(err, &notRunning):
		return recognized{code: CodeAppNotRunning, advice: []string{"; deploy it, or start it in Coolify, before reading its logs"}}, true
	case errors.As(err, &httpErr):
		return recognizeHTTP(httpErr)
	case errors.As(err, &requestErr):
		return recognizeTransport(requestErr)
	case errors.As(err, &protocol):
		return recognized{code: CodeUnexpectedResponse}, true
	case errors.As(err, &missing):
		return recognized{code: CodeNoCredentials, advice: []string{"; run coolship login, or set COOLSHIP_URL and COOLSHIP_TOKEN"}}, true
	case errors.As(err, &unknownContext):
		return recognized{code: CodeUnknownContext, advice: []string{", run coolship login"}, context: unknownContext.Name}, true
	case errors.As(err, &noDefault):
		return recognized{code: CodeNoDefaultContext, advice: []string{"; pass --context NAME or run coolship login --default"}}, true
	case errors.Is(err, auth.ErrInvalid):
		return recognized{code: CodeInvalidCredentials}, true
	case errors.Is(err, project.ErrNotLinked):
		return recognized{code: CodeNotLinked, advice: []string{"; run coolship link to use an existing Coolify application, or coolship init to create one"}}, true
	case errors.Is(err, project.ErrTargetSelection):
		return recognized{code: CodeTargetNotSelected}, true
	case errors.Is(err, project.ErrConflict):
		return recognized{code: CodeConfigChanged, advice: []string{"; review and retry linking"}}, true
	case errors.Is(err, project.ErrReplacementRequired):
		return recognized{code: CodeBindingExists, advice: []string{"; use --replace to authorize replacement"}}, true
	case errors.Is(err, config.ErrInvalid):
		return recognized{code: CodeInvalidConfig}, true
	case errors.As(err, &missingResource):
		return recognized{code: CodeResourceNotFound, advice: []string{"; check the binding or run coolship link"}}, true
	case errors.As(err, &ambiguous):
		return recognized{code: CodeResourceAmbiguous, advice: []string{"; link an explicit UUID"}}, true
	case errors.As(err, &identity):
		return recognized{code: CodeIdentityMismatch}, true
	}
	return recognized{}, false
}

// deployEndpoint is the one request whose 429 means the deployment queue is
// full rather than a rate limit.
const deployEndpoint = "/deploy"

func recognizeHTTP(e *coolify.HTTPError) (recognized, bool) {
	found := recognized{instance: e.Instance}
	switch code := e.StatusCode; {
	case code == http.StatusUnauthorized:
		found.code = CodeUnauthorized
	case code == http.StatusForbidden:
		switch e.Denial {
		case coolify.DenialAPIDisabled:
			found.code = CodeAPIDisabled
		case coolify.DenialIPNotAllowed:
			found.code = CodeIPNotAllowed
		case coolify.DenialMissingPermissions:
			found.code = CodeMissingPermissions
		case coolify.DenialTokenExceedsRole:
			found.code = CodeTokenExceedsRole
		default:
			found.code = CodeForbidden
		}
	case code >= 300 && code < 400:
		found.code = CodeRedirect
	case code == http.StatusTooManyRequests && e.Endpoint == deployEndpoint:
		found.code = CodeDeploymentQueueFull
		found.advice = []string{"; wait for a running deployment to finish, then retry"}
	case code == http.StatusTooManyRequests:
		found.code = CodeRateLimited
	case code >= 500:
		found.code = CodeServerError
	default:
		return recognized{}, false
	}
	return found, true
}

func recognizeTransport(e *coolify.RequestError) (recognized, bool) {
	switch e.Kind() {
	case coolify.TransportHostNotFound:
		return recognized{code: CodeHostNotFound, advice: []string{"; check the instance URL"}}, true
	case coolify.TransportRefused:
		return recognized{code: CodeConnectionRefused, advice: []string{"; check the instance URL and that Coolify is running"}}, true
	case coolify.TransportCertificate, coolify.TransportNotTLS, coolify.TransportHandshake:
		return recognized{code: CodeTLSError, advice: []string{
			"; check the instance URL (http or https)",
			"; check the instance URL (its certificate may not cover this host name)",
			"; check the instance URL"}}, true
	case coolify.TransportTimeout:
		return recognized{code: CodeRequestTimeout, advice: []string{"; check the instance URL and the network"}}, true
	case coolify.TransportFailed:
		return recognized{code: CodeNetworkError}, true
	}
	return recognized{}, false
}
