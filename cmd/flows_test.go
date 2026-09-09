package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/coolify"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

// These tests exercise the wiring the executable performs: the real command
// tree, the real workflows, and the real HTTP client against a controlled
// Coolify server. Only the server is a substitute.

const testToken = "integration-token"

// server records the requests a flow makes so duplicated resolution or repeated
// deployment submission fails the test rather than passing silently.
type server struct {
	mu         sync.Mutex
	requests   []string
	deployment []string
	logs       []string
	variables  []map[string]any
}

func (s *server) record(entry string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, entry)
	count := 0
	for _, seen := range s.requests {
		if seen == entry {
			count++
		}
	}
	return count
}

func (s *server) counts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := map[string]int{}
	for _, entry := range s.requests {
		result[entry]++
	}
	return result
}

func newServer(t *testing.T, s *server) *httptest.Server {
	t.Helper()
	application := map[string]any{
		"uuid": "app-1", "name": "fenix-bot", "status": "running:healthy", "fqdn": "https://fenix.example.com",
	}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
	handle := func(pattern string, fn func(http.ResponseWriter, *http.Request, int)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+testToken {
				t.Errorf("%s %s: missing bearer credentials", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fn(w, r, s.record(r.Method+" "+r.URL.Path))
		})
	}
	handle("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request, _ int) {
		// Coolify 4.3.18 answers in plain text with an HTML content type.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("4.3.18"))
	})
	handle("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, []map[string]any{{"uuid": "project-1", "name": "Personal"}})
	})
	handle("GET /api/v1/projects/project-1/environments", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, []map[string]any{{"uuid": "env-1", "name": "production"}})
	})
	handle("GET /api/v1/projects/project-1/env-1", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, map[string]any{"uuid": "env-1", "name": "production", "applications": []map[string]any{application}})
	})
	handle("GET /api/v1/applications/app-1", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, application)
	})
	handle("POST /api/v1/deploy", func(w http.ResponseWriter, r *http.Request, calls int) {
		var body struct {
			UUID        string `json:"uuid"`
			Force       bool   `json:"force"`
			PullRequest int    `json:"pr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UUID != "app-1" {
			t.Errorf("unexpected deployment body %+v (%v)", body, err)
		}
		// Coolify 4.3.18 answers an unknown pull request with 200 and no UUID.
		if body.PullRequest == 9 {
			write(w, map[string]any{"deployments": []map[string]any{
				{"resource_uuid": "app-1", "message": "Pull request 9 not found for this resource."},
			}})
			return
		}
		write(w, map[string]any{"deployments": []map[string]any{
			{"resource_uuid": "app-1", "deployment_uuid": "deploy-1", "message": "queued"},
		}})
	})
	handle("GET /api/v1/deployments/deploy-1", func(w http.ResponseWriter, _ *http.Request, calls int) {
		status := s.deployment[min(calls-1, len(s.deployment)-1)]
		write(w, map[string]any{"deployment_uuid": "deploy-1", "status": status})
	})
	handle("GET /api/v1/applications/app-1/logs", func(w http.ResponseWriter, r *http.Request, calls int) {
		if r.URL.Query().Get("show_timestamps") != "true" {
			t.Errorf("logs requested without timestamps: %s", r.URL.RawQuery)
		}
		write(w, map[string]any{"logs": s.logs[min(calls-1, len(s.logs)-1)]})
	})
	handle("GET /api/v1/applications/app-1/envs", func(w http.ResponseWriter, _ *http.Request, _ int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		write(w, s.variables)
	})
	handle("PATCH /api/v1/applications/app-1/envs/bulk", func(w http.ResponseWriter, r *http.Request, _ int) {
		var body struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("bulk body: %v", err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, item := range body.Data {
			preview, _ := item["is_preview"].(bool)
			updated := false
			for i, existing := range s.variables {
				if existing["key"] == item["key"] && existing["is_preview"] == preview {
					s.variables[i]["value"] = item["value"]
					updated = true
				}
			}
			if !updated {
				s.variables = append(s.variables, map[string]any{"uuid": "env-" + item["key"].(string), "key": item["key"], "value": item["value"], "real_value": item["value"], "is_preview": preview, "is_buildtime": true, "is_runtime": true})
			}
		}
		write(w, []map[string]any{})
	})
	handle("DELETE /api/v1/applications/app-1/envs/env-OLD", func(w http.ResponseWriter, _ *http.Request, _ int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		kept := s.variables[:0]
		for _, existing := range s.variables {
			if existing["uuid"] != "env-OLD" {
				kept = append(kept, existing)
			}
		}
		s.variables = kept
		write(w, map[string]any{"message": "Environment variable deleted."})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	instance := httptest.NewServer(mux)
	t.Cleanup(instance.Close)
	return instance
}

// run executes one command exactly as the executable would, with credentials
// supplied the way CI supplies them.
func run(t *testing.T, url, dir string, in string, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	app := service.New(service.Dependencies{
		NewBackend: func(credentials auth.Credentials) (service.Backend, error) {
			return coolify.NewClient(credentials.URL, credentials.Token)
		},
		CredentialURL:   url,
		CredentialToken: testToken,
		PollInterval:    time.Millisecond,
	})
	streams := ui.Streams{In: strings.NewReader(in), Out: &out, Err: &diagnostic, Interactive: in != ""}
	root := cmd.NewRootCommand(app, streams, "test-version")
	root.SetArgs(append([]string{"--cwd", dir}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}

// projectDirectory returns a repository root that bounds discovery, so a test
// never walks up into the checkout it runs from.
func projectDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLinkedProjectDrivesEveryWorkflow(t *testing.T) {
	s := &server{
		deployment: []string{"in_progress", "finished"},
		logs:       []string{"2026-09-09T10:00:00Z booted\n"},
	}
	instance := newServer(t, s)
	dir := projectDirectory(t)

	// link discovers the project, selects the only available resources, and writes the binding.
	out, _, err := run(t, instance.URL, dir, "", "link", "--format", "json")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	var link service.LinkResult
	if err := json.Unmarshal([]byte(out), &link); err != nil {
		t.Fatalf("link output %q: %v", out, err)
	}
	if link.Target.ApplicationUUID != "app-1" || link.Path != filepath.Join(dir, "coolship.toml") {
		t.Fatalf("unexpected link result %+v", link)
	}
	written, err := os.ReadFile(link.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), testToken) || strings.Contains(string(written), instance.URL) {
		t.Fatalf("configuration retains credentials or instance address: %s", written)
	}
	binding, err := config.Parse(written)
	if err != nil {
		t.Fatalf("written configuration is unreadable: %v", err)
	}
	if binding.Project.Application != "fenix-bot" || binding.Project.Environment != "production" || binding.Project.Project != "Personal" {
		t.Fatalf("unexpected binding %+v", binding.Project)
	}

	// Every later command resolves from that binding alone, with no extra flags.
	out, _, err = run(t, instance.URL, dir, "", "status", "--format", "json")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var status service.StatusResult
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatalf("status output %q: %v", out, err)
	}
	if status.Status != "running:healthy" || status.URL != "https://fenix.example.com" || status.Target.ApplicationUUID != "app-1" {
		t.Fatalf("unexpected status %+v", status)
	}

	out, diagnostic, err := run(t, instance.URL, dir, "", "deploy", "--format", "json")
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	var deploy service.DeployResult
	if err := json.Unmarshal([]byte(out), &deploy); err != nil {
		t.Fatalf("deploy output %q: %v", out, err)
	}
	if deploy.DeploymentUUID != "deploy-1" || deploy.Status != "finished" {
		t.Fatalf("unexpected deploy result %+v", deploy)
	}
	if !strings.Contains(diagnostic, "in_progress") || !strings.Contains(diagnostic, "finished") {
		t.Errorf("progress was not reported on stderr: %q", diagnostic)
	}
	// The credential-pair warning is emitted once as an event; the final result
	// must not repeat it.
	if count := strings.Count(diagnostic, "Warning:"); count != 1 {
		t.Errorf("deploy reported %d warnings, want 1: %q", count, diagnostic)
	}

	out, _, err = run(t, instance.URL, dir, "", "logs")
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if !strings.Contains(out, "booted") {
		t.Errorf("logs output %q", out)
	}

	// One preparation per invocation. link resolves once while selecting and
	// once while verifying its own write; status, deploy, and logs add one each.
	want := map[string]int{
		"GET /api/v1/projects":                        5,
		"GET /api/v1/projects/project-1/environments": 5,
		"GET /api/v1/projects/project-1/env-1":        5,
		"GET /api/v1/applications/app-1":              4,
		"POST /api/v1/deploy":                         1,
	}
	counts := s.counts()
	for endpoint, expected := range want {
		if counts[endpoint] != expected {
			t.Errorf("%s requested %d times, want %d", endpoint, counts[endpoint], expected)
		}
	}
}

func TestNoninteractiveLinkRequiresExplicitSelection(t *testing.T) {
	// Two projects make the choice ambiguous, so a prompt would be required.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"uuid": "project-1", "name": "Personal"}, {"uuid": "project-2", "name": "Work"},
		})
	})
	ambiguous := httptest.NewServer(mux)
	defer ambiguous.Close()

	dir := projectDirectory(t)
	_, _, err := run(t, ambiguous.URL, dir, "", "link")
	if err == nil || !strings.Contains(err.Error(), "--project") {
		t.Fatalf("expected a noninteractive selection error, got %v", err)
	}
	if ui.ExitCode(err) != 2 {
		t.Errorf("input failure exit code = %d, want 2", ui.ExitCode(err))
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !os.IsNotExist(err) {
		t.Errorf("failed link wrote configuration: %v", err)
	}
}

func TestServerFailureIsReportedWithoutWriting(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer failing.Close()
	dir := projectDirectory(t)
	_, _, err := run(t, failing.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot")
	if err == nil {
		t.Fatal("expected the server failure to surface")
	}
	if ui.ExitCode(err) != 1 {
		t.Errorf("operational failure exit code = %d, want 1", ui.ExitCode(err))
	}
	if strings.Contains(err.Error(), testToken) {
		t.Error("diagnostic leaked the token")
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !os.IsNotExist(err) {
		t.Errorf("failed link wrote configuration: %v", err)
	}
}

func TestDiagnosticCommandsAgainstTheServer(t *testing.T) {
	s := &server{logs: []string{"2026-01-01T00:00:00Z ok\n"}}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if _, _, err := run(t, instance.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link: %v", err)
	}

	out, _, err := run(t, instance.URL, dir, "", "doctor", "--format", "json")
	if err != nil {
		t.Fatalf("doctor: %v (%s)", err, out)
	}
	var doctor service.DoctorResult
	if err := json.Unmarshal([]byte(out), &doctor); err != nil || doctor.Failed {
		t.Fatalf("doctor result %s: %v", out, err)
	}
	found := map[string]string{}
	for _, check := range doctor.Checks {
		found[check.Name] = check.Detail
	}
	if !strings.Contains(found["Server"], "Coolify 4.3.18") || !strings.Contains(found["Application"], "fenix-bot") {
		t.Fatalf("doctor checks %v", found)
	}

	out, _, err = run(t, instance.URL, dir, "", "open", "--print")
	if err != nil || strings.TrimSpace(out) != "https://fenix.example.com" {
		t.Fatalf("open: out=%q err=%v", out, err)
	}
	out, _, err = run(t, instance.URL, dir, "", "open", "--dashboard")
	if err != nil || !strings.HasPrefix(out, instance.URL+"/project/project-1/environment/env-1/application/app-1") {
		t.Fatalf("open --dashboard: out=%q err=%v", out, err)
	}

	out, _, err = run(t, instance.URL, dir, "", "config", "--format", "json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var config service.ConfigResult
	if err := json.Unmarshal([]byte(out), &config); err != nil || config.CredentialSource != "environment" || config.Binding.Application != "fenix-bot" {
		t.Fatalf("config result %s: %v", out, err)
	}
	// config never contacts the server.
	if s.counts()["GET /api/v1/version"] != 1 {
		t.Fatalf("unexpected server traffic from config: %v", s.counts())
	}

	if _, _, err := run(t, instance.URL, dir, "", "unlink"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("noninteractive unlink without --yes: %v", err)
	}
	if _, _, err := run(t, instance.URL, dir, "", "unlink", "--yes"); err != nil {
		t.Fatalf("unlink --yes: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !os.IsNotExist(err) {
		t.Fatal("unlink left the configuration in place")
	}
	if _, _, err := run(t, instance.URL, dir, "", "status"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("status after unlink should report not linked: %v", err)
	}
}

func TestEnvRoundTripAgainstTheServer(t *testing.T) {
	s := &server{variables: []map[string]any{
		{"uuid": "env-KEEP", "key": "KEEP", "value": "k", "real_value": "k", "is_preview": false},
		{"uuid": "env-OLD", "key": "OLD", "value": "o", "real_value": "o", "is_preview": false},
		{"uuid": "env-KEEP-preview", "key": "KEEP", "value": "preview-k", "real_value": "preview-k", "is_preview": true},
	}}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if _, _, err := run(t, instance.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, _, err := run(t, instance.URL, dir, "", "env", "pull"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	path := filepath.Join(dir, ".env")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "KEEP=k\nOLD=o\n" {
		t.Fatalf("pulled file %q err=%v", data, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("pulled file mode %o", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte("KEEP=changed\nNEW=n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, instance.URL, dir, "", "env", "diff", "--format", "json")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var diff service.EnvDiffResult
	if err := json.Unmarshal([]byte(out), &diff); err != nil || len(diff.Added) != 1 || len(diff.Changed) != 1 || len(diff.Removed) != 1 || diff.Changed[0].Local != "" {
		t.Fatalf("diff %s: %v", out, err)
	}
	if _, _, err := run(t, instance.URL, dir, "", "env", "push", "--yes", "--prune"); err != nil {
		t.Fatalf("push: %v", err)
	}
	out, _, err = run(t, instance.URL, dir, "", "env", "diff", "--exit-code")
	if err != nil || !strings.Contains(out, "No differences") {
		t.Fatalf("after push: out=%q err=%v", out, err)
	}
	// The preview scope was never touched.
	out, _, err = run(t, instance.URL, dir, "", "env", "diff", "--preview", "--show-values")
	if err != nil || !strings.Contains(out, "~ KEEP: local changed, remote preview-k") {
		t.Fatalf("preview scope: out=%q err=%v", out, err)
	}
}

func TestPreviewDeploymentAgainstTheServer(t *testing.T) {
	s := &server{deployment: []string{"finished"}}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if _, _, err := run(t, instance.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link: %v", err)
	}
	out, _, err := run(t, instance.URL, dir, "", "preview", "--pr", "7", "--format", "json")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var result service.DeployResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.PullRequest != 7 || result.Status != "finished" {
		t.Fatalf("preview result %s: %v", out, err)
	}
	_, _, err = run(t, instance.URL, dir, "", "preview", "--pr", "9")
	if err == nil || !strings.Contains(err.Error(), "Pull request 9 not found") || ui.ExitCode(err) != 1 {
		t.Fatalf("unknown pull request: %v", err)
	}
}
