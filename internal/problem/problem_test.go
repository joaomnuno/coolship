package problem_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/models"
	"github.com/joaomnuno/coolship/internal/problem"
	"github.com/joaomnuno/coolship/internal/project"
	"github.com/joaomnuno/coolship/internal/resolver"
	"github.com/joaomnuno/coolship/internal/service"
)

// refusal answers every request with one status and body, as Coolify does.
func refusal(t *testing.T, status int, body string) *coolify.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	// A path prefix proves the Coolify page keeps it and drops /api/v1.
	client, err := coolify.NewClient(server.URL+"/coolify", "token-1234", coolify.WithRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func readErr(t *testing.T, client *coolify.Client) error {
	t.Helper()
	_, err := client.ListProjects(context.Background())
	if err == nil {
		t.Fatal("request succeeded")
	}
	return err
}

func TestCoolifyAccessRefusalsAreNamedWithTheirPage(t *testing.T) {
	for _, test := range []struct {
		name, body, code, message, page string
		fix                             problem.FixKind
		abilities                       []string
		docs                            string
	}{
		{"api disabled", `{"success":true,"message":"API is disabled."}`, "api_disabled",
			"Coolify GET /projects: HTTP 403 Forbidden: API is disabled.", "/settings/advanced", problem.FixOpenURL, nil, "https://coolify.io/docs/api/ip-allowlist"},
		{"ip not allowed", `{"success":true,"message":"You are not allowed to access the API."}`, "ip_not_allowed",
			"Coolify GET /projects: HTTP 403 Forbidden: You are not allowed to access the API.", "/settings/advanced", problem.FixOpenURL, nil, "https://coolify.io/docs/api/ip-allowlist"},
		{"missing permissions", `{"message":"Missing required permissions: read, deploy"}`, "missing_permissions",
			"Coolify GET /projects: HTTP 403 Forbidden: Missing required permissions: read, deploy", "/security/api-tokens", problem.FixOpenURL, []string{"read", "deploy"}, "https://coolify.io/docs/api/permissions"},
		{"token exceeds role", `{"message":"This API token has permissions (write, deploy) that exceed your current role as a team member. Members are restricted to read-only API access. Please revoke this token and create a new one with only read permissions."}`, "token_exceeds_role",
			"", "/security/api-tokens", problem.FixOpenURL, nil, "https://coolify.io/docs/api/permissions"},
		{"other forbidden body", `{"message":"private-detail"}`, "forbidden",
			"Coolify GET /projects: HTTP 403 Forbidden", "", problem.FixNone, nil, "https://coolify.io/docs/api/permissions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := readErr(t, refusal(t, http.StatusForbidden, test.body))
			var httpErr *coolify.HTTPError
			if !errors.As(err, &httpErr) || fmt.Sprint(httpErr.Abilities) != fmt.Sprint(test.abilities) || string(httpErr.Denial) != strings.TrimPrefix(test.code, "forbidden") {
				t.Fatalf("typed error: %#v", err)
			}
			found, ok := problem.Classify(fmt.Errorf("prepare: %w", err))
			if !ok || string(found.Code) != test.code || found.Owner != problem.OwnerCoolify || found.Fix != test.fix || found.DocsURL != test.docs || found.Hint == "" {
				t.Fatalf("problem: %+v ok=%v", found, ok)
			}
			if test.message != "" && found.Message != "prepare: "+test.message {
				t.Fatalf("message = %q", found.Message)
			}
			if strings.Contains(found.Message, "private-detail") {
				t.Fatalf("unknown 403 body repeated: %q", found.Message)
			}
			if test.page == "" {
				if found.FixURL != "" {
					t.Fatalf("fix URL without a page: %q", found.FixURL)
				}
				return
			}
			if !strings.HasPrefix(found.FixURL, "http://127.0.0.1:") || !strings.HasSuffix(found.FixURL, "/coolify"+test.page) {
				t.Fatalf("fix URL = %q", found.FixURL)
			}
		})
	}
}

func TestStatusesWithoutAnExplanationKeepTheirTextAndGainAHint(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		fix    problem.FixKind
	}{
		{http.StatusUnauthorized, "unauthorized", problem.FixReLogin},
		{http.StatusMovedPermanently, "redirect", problem.FixNone},
		{http.StatusTooManyRequests, "rate_limited", problem.FixNone},
		{http.StatusInternalServerError, "server_error", problem.FixNone},
	} {
		t.Run(test.code, func(t *testing.T) {
			err := readErr(t, refusal(t, test.status, `{"message":"Unauthenticated."}`))
			found, ok := problem.Classify(err)
			if !ok || string(found.Code) != test.code || found.Fix != test.fix || found.Message != err.Error() || found.Hint == "" {
				t.Fatalf("problem: %+v ok=%v", found, ok)
			}
		})
	}
	if _, ok := problem.Classify(readErr(t, refusal(t, http.StatusNotFound, `{}`))); ok {
		t.Fatal("a 404 is left to the workflow that read it")
	}
}

func TestAFullDeploymentQueueIsNotARateLimit(t *testing.T) {
	_, cause := refusal(t, http.StatusTooManyRequests, `{"message":"queue"}`).Deploy(context.Background(), models.DeployRequest{ApplicationUUID: "a1"})
	// The words service.deploy puts around the refusal.
	err := fmt.Errorf("server deployment queue is full; wait for a running deployment to finish, then retry: %w", cause)
	found, ok := problem.Classify(err)
	if !ok || found.Code != problem.CodeDeploymentQueueFull || strings.Contains(found.Message, "wait for") ||
		!strings.HasPrefix(found.Message, "server deployment queue is full: Coolify POST /deploy: HTTP 429") {
		t.Fatalf("problem: %+v ok=%v", found, ok)
	}
}

type timeout struct{}

func (timeout) Error() string   { return "i/o timeout" }
func (timeout) Timeout() bool   { return true }
func (timeout) Temporary() bool { return true }

func TestTransportFailuresAreNamedWithoutTheirAdviceTwice(t *testing.T) {
	for _, test := range []struct {
		cause   error
		code    problem.Code
		message string
	}{
		{&net.DNSError{Err: "no such host", Name: "coolify.invalid"}, problem.CodeHostNotFound, "Coolify GET /projects: the host name could not be resolved"},
		{&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, problem.CodeConnectionRefused, "Coolify GET /projects: the connection was refused"},
		{tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, problem.CodeTLSError, "Coolify GET /projects: the server did not answer with TLS"},
		{&net.OpError{Op: "remote error", Err: errors.New("tls: handshake failure")}, problem.CodeTLSError, "Coolify GET /projects: the server refused the TLS handshake"},
		{timeout{}, problem.CodeRequestTimeout, "Coolify GET /projects: the request timed out"},
		{errors.New("proxy said https://user:secret@host"), problem.CodeNetworkError, "Coolify GET /projects: request failed"},
	} {
		t.Run(string(test.code), func(t *testing.T) {
			err := &coolify.RequestError{Method: http.MethodGet, Endpoint: "/projects", Err: test.cause}
			found, ok := problem.Classify(err)
			if !ok || found.Code != test.code || found.Message != test.message || found.Owner != problem.OwnerCoolship {
				t.Fatalf("problem: %+v ok=%v", found, ok)
			}
		})
	}
	if _, ok := problem.Classify(&coolify.RequestError{Err: context.Canceled}); ok {
		t.Fatal("an interrupt is not a catalogued problem")
	}
}

func writeCredentials(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalSetupFailuresOfferTheirFix(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "config.json")
	_, noCredentials := auth.Resolve(auth.Options{ConfigPath: missingPath})
	saved := writeCredentials(t, `{"instances":[{"name":"home","fqdn":"https://c.example.com","token":"t"}]}`)
	_, unknown := auth.Resolve(auth.Options{ConfigPath: saved, Context: "hme"})
	_, noDefault := auth.Resolve(auth.Options{ConfigPath: saved})
	_, pair := auth.Resolve(auth.Options{URL: "https://c.example.com"})
	for _, test := range []struct {
		name    string
		err     error
		code    problem.Code
		fix     problem.FixKind
		message string
		context string
	}{
		{"no credentials", noCredentials, problem.CodeNoCredentials, problem.FixLogin, "No Coolify credentials at " + missingPath, ""},
		{"unknown context", unknown, problem.CodeUnknownContext, problem.FixPickContext, `Coolify context not found: "hme" (did you mean "home"?); saved contexts: home`, "hme"},
		{"no default", noDefault, problem.CodeNoDefaultContext, problem.FixPickContext, "invalid Coolify credentials: no default instance", ""},
		{"invalid credentials", pair, problem.CodeInvalidCredentials, problem.FixNone, "invalid Coolify credentials: COOLSHIP_URL and COOLSHIP_TOKEN must be supplied together", ""},
		{"not linked", &service.InputError{Err: project.ErrNotLinked}, problem.CodeNotLinked, problem.FixLink, "project is not linked", ""},
		{"target", fmt.Errorf("select: %w", project.ErrTargetSelection), problem.CodeTargetNotSelected, problem.FixNone, "select: target cannot be selected", ""},
		{"conflict", project.ErrConflict, problem.CodeConfigChanged, problem.FixNone, "project configuration changed", ""},
		{"binding exists", project.ErrReplacementRequired, problem.CodeBindingExists, problem.FixNone, "binding already exists", ""},
		{"invalid config", fmt.Errorf("%w: version must be 1", config.ErrInvalid), problem.CodeInvalidConfig, problem.FixNone, "invalid project configuration: version must be 1", ""},
		{"missing resource", &resolver.MissingError{Resource: "application", Name: "web", Scope: "environment production"}, problem.CodeResourceNotFound, problem.FixNone,
			`application named "web" not found in environment production`, ""},
		{"ambiguous resource", &resolver.AmbiguousError{Resource: "application", Scope: "environment production", Choices: []resolver.Choice{{UUID: "a", Name: "web"}}}, problem.CodeResourceAmbiguous, problem.FixNone,
			`application selector is ambiguous in environment production: "web" (a)`, ""},
		{"identity", &resolver.IdentityError{Resource: "application", Reason: "moved"}, problem.CodeIdentityMismatch, problem.FixNone, `application identity validation failed: moved (expected UUID "", received "")`, ""},
		{"deployment timeout", &service.DeploymentError{DeploymentUUID: "d1", Err: &service.TimeoutError{Timeout: time.Minute, Err: context.DeadlineExceeded}},
			problem.CodeDeploymentTimeout, problem.FixNone, "deployment d1: --timeout 1m0s elapsed before the deployment finished", ""},
		{"uncertain", &coolify.UncertainSubmissionError{ResourceUUID: "a1", Err: &coolify.RequestError{Err: timeout{}}}, problem.CodeUncertainSubmission, problem.FixNone, "", ""},
		{"not running", &coolify.NotRunningError{Message: "Application is not running."}, problem.CodeAppNotRunning, problem.FixNone, "application is not running", ""},
		{"protocol", &coolify.ProtocolError{Endpoint: "/version", Reason: "odd"}, problem.CodeUnexpectedResponse, problem.FixNone, "Coolify /version: odd", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			found, ok := problem.Classify(test.err)
			if !ok || found.Code != test.code || found.Fix != test.fix || found.Context != test.context || found.Hint == "" ||
				found.DocsURL != problem.DocsBase+"#"+string(test.code) || found.FixURL != "" {
				t.Fatalf("problem: %+v ok=%v", found, ok)
			}
			if test.message != "" && found.Message != test.message {
				t.Fatalf("message = %q, want %q", found.Message, test.message)
			}
		})
	}
	_, noneSaved := auth.Resolve(auth.Options{ConfigPath: writeCredentials(t, `{"instances":[{"name":"home","fqdn":"https://c.example.com","token":"t","default":true}]}`), Context: "work"})
	if found, _ := problem.Classify(noneSaved); found.Code != problem.CodeUnknownContext || found.Context != "work" {
		t.Fatalf("unknown context: %+v", found)
	}
	for _, uncatalogued := range []error{errors.New("boom"), &service.InputError{Err: errors.New("--port must be between 1 and 65535")}, context.Canceled, nil} {
		if found, ok := problem.Classify(uncatalogued); ok {
			t.Fatalf("%v classified as %+v", uncatalogued, found)
		}
	}
}

func TestLogsRefusalDropsTheAdviceTheHintGives(t *testing.T) {
	// The words service.logsError restates the refusal with.
	err := restated{text: "application is not running (status exited); deploy it, or start it in Coolify, before reading its logs", err: &coolify.NotRunningError{}}
	found, ok := problem.Classify(err)
	if !ok || found.Message != "application is not running (status exited)" {
		t.Fatalf("problem: %+v", found)
	}
}

type restated struct {
	text string
	err  error
}

func (e restated) Error() string { return e.text }
func (e restated) Unwrap() error { return e.err }

// TestEveryCodeHasItsOwnHeading keeps the errors page in step with the
// catalog: one "### code" heading per code, generic ones included.
func TestEveryCodeHasItsOwnHeading(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "content", "docs", "platform", "errors.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	headings := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^### ([a-z_]+)$`).FindAllStringSubmatch(string(data), -1) {
		headings[match[1]] = true
	}
	codes := append(problem.Codes(), problem.GenericCodes()...)
	for _, code := range codes {
		if !headings[string(code)] {
			t.Errorf("errors.mdx has no heading for %s", code)
		}
		delete(headings, string(code))
	}
	for heading := range headings {
		t.Errorf("errors.mdx documents unknown code %s", heading)
	}
	for _, code := range problem.Codes() {
		entry, _ := problem.Lookup(code)
		if entry.Hint == "" || !strings.HasSuffix(entry.Hint, ".") || (entry.Owner != problem.OwnerCoolify && entry.Owner != problem.OwnerCoolship) {
			t.Errorf("%s: incomplete entry %+v", code, entry)
		}
		if !regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`).MatchString(string(code)) {
			t.Errorf("%s is not snake_case", code)
		}
	}
}
