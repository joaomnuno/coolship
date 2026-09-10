package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	"github.com/joaomnuno/coolship/internal/gitinfo"
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
	creations  []map[string]any
	// status replaces the application's running:healthy when set, and
	// notRunning makes the logs endpoint refuse as Coolify 4.3.18 does for
	// an application without a container.
	status     string
	notRunning bool
	// stopAfter is how many logs snapshots are served before the container
	// goes away: later logs calls refuse as notRunning does, and the
	// application reads exited:unhealthy from then on.
	stopAfter int
	history   []map[string]any // deployment rows, newest first, as the list endpoint returns them
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
	if s.status != "" {
		application["status"] = s.status
	}
	second := map[string]any{"uuid": "app-2", "name": "fenix-api", "status": "running:healthy", "fqdn": "https://api.example.com"}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}
	handle := func(pattern string, fn func(http.ResponseWriter, *http.Request, int)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if authorization := r.Header.Get("Authorization"); authorization != "Bearer "+testToken {
				// A wrong token is a legitimate 401 (login must handle it); a
				// missing header is a wiring bug.
				if authorization == "" {
					t.Errorf("%s %s: missing bearer credentials", r.Method, r.URL.Path)
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fn(w, r, s.record(r.Method+" "+r.URL.Path))
		})
	}
	handle("GET /api/v1/teams/current", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, map[string]any{"id": 0, "name": "PelicanOS"})
	})
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
	environment := map[string]any{"uuid": "env-1", "name": "production", "applications": []map[string]any{application, second}}
	handle("GET /api/v1/projects/project-1/env-1", func(w http.ResponseWriter, _ *http.Request, _ int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		write(w, environment)
	})
	handle("GET /api/v1/applications/app-1", func(w http.ResponseWriter, _ *http.Request, _ int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		write(w, application)
	})
	handle("PATCH /api/v1/applications/app-1", func(w http.ResponseWriter, r *http.Request, _ int) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("patch body: %v", err)
		}
		if domains, ok := body["domains"].(string); ok {
			application["fqdn"] = domains
		}
		write(w, map[string]any{"uuid": "app-1"})
	})
	handle("GET /api/v1/applications/app-2", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, second)
	})
	handle("GET /api/v1/servers", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, []map[string]any{
			{"uuid": "server-1", "name": "Master Ubuntu", "ip": "10.0.0.1", "is_reachable": true, "is_usable": true, "settings": map[string]any{"is_usable": true}},
			{"uuid": "server-2", "name": "Build box", "ip": "10.0.0.2", "is_reachable": false, "is_usable": false},
		})
	})
	// Creation answers as Coolify 4.3.18 does: 201 with the uuid and the
	// generated domain, or 422 with a message and field errors. The new
	// application then appears in the environment like any other.
	handle("POST /api/v1/applications/public", func(w http.ResponseWriter, r *http.Request, _ int) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("creation body: %v", err)
		}
		s.mu.Lock()
		s.creations = append(s.creations, body)
		s.mu.Unlock()
		if body["git_repository"] == "https://github.com/joaomnuno/private" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			write(w, map[string]any{"message": "Validation failed.", "errors": map[string]any{"git_repository": []string{"Repository is not accessible."}}})
			return
		}
		if body["project_uuid"] != "project-1" || body["environment_name"] != "production" || body["server_uuid"] != "server-1" || body["instant_deploy"] != false {
			t.Errorf("unexpected creation body %v", body)
		}
		name, _ := body["name"].(string)
		created := map[string]any{"uuid": "app-" + name, "name": name, "status": "exited:unhealthy", "fqdn": "https://app-" + name + ".coolify.example.com"}
		environment["applications"] = append(environment["applications"].([]map[string]any), created)
		mux.HandleFunc("GET /api/v1/applications/app-"+name, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+testToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			s.record(r.Method + " " + r.URL.Path)
			write(w, created)
		})
		w.WriteHeader(http.StatusCreated)
		write(w, map[string]any{"uuid": created["uuid"], "domains": created["fqdn"]})
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
		write(w, map[string]any{"deployment_uuid": "deploy-1", "status": status, "application": map[string]any{"uuid": "app-1"}})
	})
	// History answers as Coolify 4.3.18 does: a count and the newest rows,
	// build logs included for a token that may read them. Only app-1 has
	// rows; every other application has an empty history.
	handle("GET /api/v1/deployments/applications/{uuid}", func(w http.ResponseWriter, r *http.Request, _ int) {
		take := 10
		if value := r.URL.Query().Get("take"); value != "" {
			fmt.Sscanf(value, "%d", &take)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		rows, total := []map[string]any{}, 0
		if r.PathValue("uuid") == "app-1" {
			rows = append(rows, s.history[:min(take, len(s.history))]...)
			total = len(s.history)
		}
		write(w, map[string]any{"count": total, "deployments": rows})
	})
	handle("GET /api/v1/deployments/d-old", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, map[string]any{"deployment_uuid": "d-old", "status": "finished", "commit": "0cd7c4a692347804dbd076a4d7e11c847e085473", "application": map[string]any{"uuid": "app-1"}})
	})
	handle("POST /api/v1/applications/app-1/stop", func(w http.ResponseWriter, _ *http.Request, _ int) {
		// The real job runs later; the controlled server flips the status at once.
		application["status"] = "exited:unhealthy"
		write(w, map[string]any{"message": "Application stopping request queued."})
	})
	handle("POST /api/v1/applications/app-1/start", func(w http.ResponseWriter, r *http.Request, _ int) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["force"] != false {
			t.Errorf("start body %v (%v)", body, err)
		}
		application["status"] = "running:healthy"
		write(w, map[string]any{"message": "Deployment request queued.", "deployment_uuid": "deploy-1"})
	})
	handle("POST /api/v1/applications/app-1/restart", func(w http.ResponseWriter, _ *http.Request, _ int) {
		write(w, map[string]any{"message": "Restart request queued.", "deployment_uuid": "deploy-1"})
	})
	handle("POST /api/v1/deployments/d-run/cancel", func(w http.ResponseWriter, _ *http.Request, _ int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, row := range s.history {
			if row["deployment_uuid"] != "d-run" {
				continue
			}
			if row["status"] != "queued" && row["status"] != "in_progress" {
				w.WriteHeader(http.StatusBadRequest)
				write(w, map[string]any{"message": fmt.Sprintf("Deployment cannot be cancelled. Current status: %s", row["status"])})
				return
			}
			row["status"] = "cancelled-by-user"
			write(w, map[string]any{"message": "Deployment cancelled successfully.", "deployment_uuid": "d-run", "status": "cancelled-by-user"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		write(w, map[string]any{"message": "Deployment not found."})
	})
	handle("GET /api/v1/applications/app-1/logs", func(w http.ResponseWriter, r *http.Request, calls int) {
		if r.URL.Query().Get("show_timestamps") != "true" {
			t.Errorf("logs requested without timestamps: %s", r.URL.RawQuery)
		}
		stopped := s.stopAfter > 0 && calls > s.stopAfter
		if stopped {
			s.mu.Lock()
			application["status"] = "exited:unhealthy"
			s.mu.Unlock()
		}
		if s.notRunning || stopped {
			w.WriteHeader(http.StatusBadRequest)
			write(w, map[string]any{"message": "Application is not running."})
			return
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
		RunProcess: func(_ context.Context, spec service.ProcessSpec) (int, error) {
			// Record what a real runner would receive; flows do not spawn processes.
			fmt.Fprintf(&out, "spec dir=%s args=%v shell=%q env=%v\n", filepath.Base(spec.Dir), spec.Args, spec.Shell, spec.Env)
			return 0, nil
		},
		// Test directories carry a .git marker, not a repository; the answer
		// git would give is supplied here.
		InspectRepository: func(_ context.Context, dir string) (gitinfo.Repository, error) {
			return gitinfo.Repository{Remote: "https://github.com/joaomnuno/" + filepath.Base(dir), Branch: "main"}, nil
		},
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

	// link discovers the project, selects the only project and environment, and writes the binding.
	out, _, err := run(t, instance.URL, dir, "", "link", "--application", "fenix-bot", "--format", "json")
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

func TestInitCreatesThenEveryCommandResolvesIt(t *testing.T) {
	s := &server{deployment: []string{"finished"}}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(dir)

	// Noninteractive without --yes stops at the plan: no request creates anything.
	_, _, err := run(t, instance.URL, dir, "", "init", "--project", "Personal")
	if !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--yes") || len(s.creations) != 0 {
		t.Fatalf("without --yes: err=%v creations=%d", err, len(s.creations))
	}
	// Interactively, the plan is shown on stderr and declined.
	_, diagnostic, err := run(t, instance.URL, dir, "n\n", "init", "--project", "Personal")
	if !errors.Is(err, service.ErrCancelled) || !strings.Contains(diagnostic, "Create application "+name+" on") || !strings.Contains(diagnostic, "Master Ubuntu") || len(s.creations) != 0 {
		t.Fatalf("declined: err=%v stderr=%q", err, diagnostic)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !os.IsNotExist(err) {
		t.Fatalf("declined init wrote configuration: %v", err)
	}
	// The server's refusal reaches the user with its explanation, and nothing is written.
	_, _, err = run(t, instance.URL, dir, "", "init", "--project", "Personal", "--repo", "https://github.com/joaomnuno/private", "--yes")
	if err == nil || !strings.Contains(err.Error(), "Repository is not accessible") || ui.ExitCode(err) != 1 {
		t.Fatalf("refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "coolship.toml")); !os.IsNotExist(err) {
		t.Fatalf("refused init wrote configuration: %v", err)
	}

	// Accepted: the only usable server is chosen, the Dockerfile sets the
	// build pack, the repository comes from Git, and the binding is written.
	out, _, err := run(t, instance.URL, dir, "", "init", "--project", "Personal", "--yes", "--format", "json")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	var result service.InitResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("init output %q: %v", out, err)
	}
	if result.Target.ApplicationUUID != "app-"+name || result.Plan.Server != "Master Ubuntu" || result.Plan.BuildPack != "dockerfile" || result.Plan.Port != 80 ||
		result.Plan.Repository != "https://github.com/joaomnuno/"+name || result.URL != "https://app-"+name+".coolify.example.com" {
		t.Fatalf("unexpected init result %+v", result)
	}
	if len(s.creations) != 2 || s.creations[1]["ports_exposes"] != "80" || s.creations[1]["build_pack"] != "dockerfile" || s.creations[1]["git_branch"] != "main" {
		t.Fatalf("creations %v", s.creations)
	}
	written, err := os.ReadFile(filepath.Join(dir, "coolship.toml"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := config.Parse(written)
	if err != nil || binding.Project.Application != name || binding.Project.Project != "Personal" || binding.Project.Environment != "production" || binding.Project.ApplicationUUID != "" {
		t.Fatalf("written binding %+v: %v", binding.Project, err)
	}
	// status resolves the new application from the same directory with no flags.
	out, _, err = run(t, instance.URL, dir, "", "status", "--format", "json")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var status service.StatusResult
	if err := json.Unmarshal([]byte(out), &status); err != nil || status.Target.ApplicationUUID != "app-"+name || status.URL != result.URL {
		t.Fatalf("status %s: %v", out, err)
	}
	// A second init in the linked directory is refused before any request.
	if _, _, err := run(t, instance.URL, dir, "", "init", "--project", "Personal", "--name", "again", "--yes"); !errors.Is(err, service.ErrInput) || len(s.creations) != 2 {
		t.Fatalf("already linked: err=%v creations=%d", err, len(s.creations))
	}
	// Nothing was deployed, and no request went to the unusable server.
	if counts := s.counts(); counts["POST /api/v1/deploy"] != 0 || counts["POST /api/v1/applications/public"] != 2 {
		t.Fatalf("requests %v", counts)
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

func TestMonorepoTargetsAgainstTheServer(t *testing.T) {
	s := &server{deployment: []string{"finished"}}
	instance := newServer(t, s)
	root := projectDirectory(t)
	for _, dir := range []string{"apps/web", "apps/api"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := run(t, instance.URL, filepath.Join(root, "apps/web"), "", "link", "--target", "web", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link web: %v", err)
	}
	if _, _, err := run(t, instance.URL, filepath.Join(root, "apps/api"), "", "link", "--target", "api", "--project", "Personal", "--application", "fenix-api"); err != nil {
		t.Fatalf("link api: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "coolship.toml"))
	if !strings.Contains(string(data), "[apps.web]") || !strings.Contains(string(data), "[apps.api]") || !strings.Contains(string(data), `root = 'apps/api'`) {
		t.Fatalf("configuration:\n%s", data)
	}
	// From apps/api the api target is implied; from the root a target is required.
	out, _, err := run(t, instance.URL, filepath.Join(root, "apps/api"), "", "status", "--format", "json")
	if err != nil {
		t.Fatalf("status from apps/api: %v", err)
	}
	var status service.StatusResult
	if err := json.Unmarshal([]byte(out), &status); err != nil || status.Target.Target != "api" || status.Target.ApplicationUUID != "app-2" {
		t.Fatalf("status %s: %v", out, err)
	}
	if _, _, err := run(t, instance.URL, root, "", "status"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("root without target: %v", err)
	}
	out, _, err = run(t, instance.URL, root, "", "deploy", "web", "--format", "json")
	if err != nil {
		t.Fatalf("deploy web: %v", err)
	}
	var deploy service.DeployResult
	if err := json.Unmarshal([]byte(out), &deploy); err != nil || deploy.Target.Target != "web" || deploy.Target.ApplicationUUID != "app-1" {
		t.Fatalf("deploy %s: %v", out, err)
	}
	if s.counts()["POST /api/v1/deploy"] != 1 {
		t.Fatalf("deploy calls: %v", s.counts())
	}
}

func TestDevReceivesServerVariables(t *testing.T) {
	s := &server{variables: []map[string]any{
		{"uuid": "e1", "key": "SHARED", "value": "{{team.X}}", "real_value": "resolved", "is_preview": false, "is_runtime": true, "is_shared": true},
		{"uuid": "e2", "key": "PLAIN", "value": "p", "real_value": "p", "is_preview": false, "is_runtime": true},
		{"uuid": "e3", "key": "PLAIN", "value": "pp", "real_value": "pp", "is_preview": true, "is_runtime": true},
	}}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if _, _, err := run(t, instance.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link: %v", err)
	}
	out, _, err := run(t, instance.URL, dir, "", "dev", "--", "printenv", "PLAIN")
	if err != nil || !strings.Contains(out, "args=[printenv PLAIN]") || !strings.Contains(out, "env=[PLAIN=p SHARED=resolved]") {
		t.Fatalf("dev: out=%q err=%v", out, err)
	}
}

func TestDomainRoundTripAgainstTheServer(t *testing.T) {
	s := &server{}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if _, _, err := run(t, instance.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link: %v", err)
	}
	out, _, err := run(t, instance.URL, dir, "", "domain")
	if err != nil || strings.TrimSpace(out) != "https://fenix.example.com" {
		t.Fatalf("domain: out=%q err=%v", out, err)
	}
	if _, _, err := run(t, instance.URL, dir, "", "domain", "set", "new.example.com", "--yes"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, _, err = run(t, instance.URL, dir, "", "status", "--format", "json")
	if err != nil || !strings.Contains(out, `"url":"https://new.example.com"`) {
		t.Fatalf("status after set: out=%q err=%v", out, err)
	}
}

func TestLifecycleAgainstTheServer(t *testing.T) {
	row := func(uuid, status string) map[string]any {
		return map[string]any{"id": 1, "deployment_uuid": uuid, "pull_request_id": 0, "force_rebuild": false, "commit": "0cd7c4a692347804dbd076a4d7e11c847e085473",
			"status": status, "is_webhook": false, "is_api": true, "restart_only": false, "rollback": false, "server_name": "Master Ubuntu",
			"commit_message": "cool container", "logs": `[{"output":"secret build output","hidden":false}]`,
			"created_at": "2026-09-10T11:37:12.000000Z", "updated_at": "2026-09-10T11:37:37.000000Z", "finished_at": "2026-09-10T11:37:36.000000Z"}
	}
	running := row("d-run", "in_progress")
	running["finished_at"] = nil
	s := &server{deployment: []string{"in_progress", "finished"}, history: []map[string]any{running, row("d-old", "finished")}}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	if _, _, err := run(t, instance.URL, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link: %v", err)
	}

	// History is typed and never carries the build log the server sent.
	out, _, err := run(t, instance.URL, dir, "", "deployments", "--format", "json")
	if err != nil {
		t.Fatalf("deployments: %v", err)
	}
	var history service.DeploymentsResult
	if err := json.Unmarshal([]byte(out), &history); err != nil || history.Total != 2 || len(history.Deployments) != 2 || history.Deployments[0].Status != "in_progress" || history.Deployments[1].FinishedAt == "" {
		t.Fatalf("deployments %s: %v", out, err)
	}
	if strings.Contains(out, "secret build output") || strings.Contains(out, "logs") {
		t.Fatalf("history output carries build logs: %s", out)
	}
	out, _, err = run(t, instance.URL, dir, "", "deployments", "-n", "1")
	if err != nil || !strings.Contains(out, "(1 of 2)") || !strings.Contains(out, "d-run") || strings.Contains(out, "d-old") {
		t.Fatalf("deployments -n 1: out=%q err=%v", out, err)
	}
	// status gains the last deployment from the same history.
	out, _, err = run(t, instance.URL, dir, "", "status")
	if err != nil || !strings.Contains(out, "Status: running:healthy\nURL: https://fenix.example.com\nLast deployment: d-run in_progress (0cd7c4a) ") {
		t.Fatalf("status: out=%q err=%v", out, err)
	}

	// stop asks, or needs --yes; then the status is polled until it leaves running.
	if _, _, err := run(t, instance.URL, dir, "", "stop"); !errors.Is(err, service.ErrInput) || s.counts()["POST /api/v1/applications/app-1/stop"] != 0 {
		t.Fatalf("noninteractive stop without --yes: %v", err)
	}
	out, diagnostic, err := run(t, instance.URL, dir, "", "stop", "--yes")
	if err != nil || !strings.Contains(out, "Status: exited:unhealthy") || !strings.Contains(diagnostic, "Application stopping request queued.") || !strings.Contains(diagnostic, "Application status: exited:unhealthy") {
		t.Fatalf("stop: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	out, _, err = run(t, instance.URL, dir, "", "status", "--format", "json")
	if err != nil || !strings.Contains(out, `"status":"exited:unhealthy"`) {
		t.Fatalf("status after stop: out=%q err=%v", out, err)
	}
	// A second stop finds the application already exited and sends nothing.
	if _, diagnostic, err := run(t, instance.URL, dir, "", "stop", "--yes"); err != nil || !strings.Contains(diagnostic, "already stopped (exited:unhealthy)") || s.counts()["POST /api/v1/applications/app-1/stop"] != 1 {
		t.Fatalf("stop when stopped: stderr=%q err=%v counts=%v", diagnostic, err, s.counts())
	}

	// start queues a deployment through the action and observes it like deploy.
	out, diagnostic, err = run(t, instance.URL, dir, "", "start", "--format", "json")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	var started service.DeployResult
	if err := json.Unmarshal([]byte(out), &started); err != nil || started.Action != "start" || started.DeploymentUUID != "deploy-1" || started.Status != "finished" {
		t.Fatalf("start result %s: %v", out, err)
	}
	if !strings.Contains(diagnostic, "Deployment deploy-1: in_progress") || !strings.Contains(diagnostic, "Deployment deploy-1: finished") {
		t.Fatalf("start progress: %q", diagnostic)
	}
	out, _, err = run(t, instance.URL, dir, "", "status")
	if err != nil || !strings.Contains(out, "Status: running:healthy") {
		t.Fatalf("status after start: out=%q err=%v", out, err)
	}
	// restart needs --yes noninteractively and returns the queued identity with --no-wait.
	if _, _, err := run(t, instance.URL, dir, "", "restart", "--no-wait"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("noninteractive restart without --yes: %v", err)
	}
	out, _, err = run(t, instance.URL, dir, "", "restart", "--yes", "--no-wait", "--format", "json")
	if err != nil || !strings.Contains(out, `"action":"restart"`) || !strings.Contains(out, `"status":"queued"`) {
		t.Fatalf("restart: out=%q err=%v", out, err)
	}

	// cancel finds the one running deployment, then refuses when none is left.
	if _, _, err := run(t, instance.URL, dir, "", "cancel"); !errors.Is(err, service.ErrInput) || s.counts()["POST /api/v1/deployments/d-run/cancel"] != 0 {
		t.Fatalf("noninteractive cancel without --yes: %v", err)
	}
	out, _, err = run(t, instance.URL, dir, "", "cancel", "--yes")
	if err != nil || !strings.Contains(out, "Deployment: d-run\n") || !strings.Contains(out, "Status: cancelled-by-user") {
		t.Fatalf("cancel: out=%q err=%v", out, err)
	}
	if _, _, err := run(t, instance.URL, dir, "", "cancel", "--yes"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "no deployment") {
		t.Fatalf("cancel with none running: %v", err)
	}
	// A named deployment that already ended is refused before any request.
	if _, _, err := run(t, instance.URL, dir, "", "cancel", "d-old", "--yes"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "is finished") {
		t.Fatalf("cancel finished: %v", err)
	}
	counts := s.counts()
	for endpoint, expected := range map[string]int{
		"POST /api/v1/applications/app-1/stop":    1,
		"POST /api/v1/applications/app-1/start":   1,
		"POST /api/v1/applications/app-1/restart": 1,
		"POST /api/v1/deployments/d-run/cancel":   1,
		"POST /api/v1/deploy":                     0,
	} {
		if counts[endpoint] != expected {
			t.Errorf("%s requested %d times, want %d", endpoint, counts[endpoint], expected)
		}
	}
}

// runWithFile executes a command with credentials taken from a Coolify CLI
// configuration file rather than the environment pair.
func runWithFile(t *testing.T, configPath, dir, in string, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	app := service.New(service.Dependencies{
		NewBackend: func(credentials auth.Credentials) (service.Backend, error) {
			return coolify.NewClient(credentials.URL, credentials.Token)
		},
		PollInterval: time.Millisecond,
	})
	root := cmd.NewRootCommand(app, ui.Streams{In: strings.NewReader(in), Out: &out, Err: &diagnostic}, "test-version")
	root.SetArgs(append([]string{"--cwd", dir, "--coolify-config", configPath}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}

func TestLoginWritesAFileEveryCommandCanUse(t *testing.T) {
	s := &server{}
	instance := newServer(t, s)
	dir := projectDirectory(t)
	configPath := filepath.Join(t.TempDir(), "coolify", "config.json")
	out, _, err := runWithFile(t, configPath, dir, testToken+"\n", "login", "--url", instance.URL, "--name", "ci", "--token-stdin")
	if err != nil || !strings.Contains(out, "Logged in to ci") || !strings.Contains(out, "PelicanOS") || strings.Contains(out, testToken) {
		t.Fatalf("login: out=%q err=%v", out, err)
	}
	data, _ := os.ReadFile(configPath)
	if info, _ := os.Stat(configPath); info.Mode().Perm() != 0o600 || !strings.Contains(string(data), `"default": true`) {
		t.Fatalf("written file: mode=%o content=%s", info.Mode().Perm(), data)
	}
	// A wrong token is refused by the server and nothing is written for it.
	if _, _, err := runWithFile(t, configPath, dir, "wrong\n", "login", "--url", instance.URL, "--name", "bad", "--token-stdin"); err == nil {
		t.Fatal("wrong token accepted")
	}
	if data, _ := os.ReadFile(configPath); strings.Contains(string(data), `"bad"`) {
		t.Fatal("rejected context was written")
	}
	if _, _, err := runWithFile(t, configPath, dir, "", "link", "--project", "Personal", "--application", "fenix-bot"); err != nil {
		t.Fatalf("link with the saved context: %v", err)
	}
	out, _, err = runWithFile(t, configPath, dir, "", "doctor")
	if err != nil || !strings.Contains(out, "[ok]   Context: ci at "+instance.URL) {
		t.Fatalf("doctor: out=%q err=%v", out, err)
	}
	if _, _, err := runWithFile(t, configPath, dir, "", "logout", "ci"); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, _, err := runWithFile(t, configPath, dir, "", "status"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("status after logout should fail on credentials: %v", err)
	}
}

// linkedDirectory writes a binding directly, so a flow can start from a
// linked project without a server that answers link.
func linkedDirectory(t *testing.T) string {
	t.Helper()
	dir := projectDirectory(t)
	data, err := config.Marshal(config.Config{Version: 1, Project: config.Binding{Project: "Personal", Environment: "production", Application: "fenix-bot", Root: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coolship.toml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRejectedTokensAndRedirectsExplainThemselvesOnEveryCommand(t *testing.T) {
	for _, test := range []struct {
		status int
		hint   string
	}{
		{http.StatusUnauthorized, "run coolship login"},
		{http.StatusForbidden, "lacks a required ability"},
		{http.StatusMovedPermanently, "redirects are not followed"},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.status/100 == 3 {
					w.Header().Set("Location", "https://elsewhere.example.com/")
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, `{"message":"private-detail"}`)
			}))
			defer refusing.Close()
			dir := linkedDirectory(t)
			for _, args := range [][]string{{"status"}, {"deploy"}, {"env", "pull"}, {"link", "--project", "Personal", "--application", "fenix-bot", "--replace"}} {
				out, _, err := run(t, refusing.URL, dir, "", args...)
				if err == nil || ui.ExitCode(err) != 1 || out != "" {
					t.Fatalf("%v: out=%q err=%v code=%d", args, out, err, ui.ExitCode(err))
				}
				var diagnostic bytes.Buffer
				if err := ui.PrintError(ui.Streams{Err: &diagnostic}, err); err != nil {
					t.Fatal(err)
				}
				text := diagnostic.String()
				if !strings.Contains(text, fmt.Sprintf("HTTP %d", test.status)) || !strings.Contains(text, test.hint) || strings.Count(text, test.hint) != 1 {
					t.Fatalf("%v: %q", args, text)
				}
				if strings.Contains(text, "private-detail") || strings.Contains(text, testToken) || strings.Count(text, "\n") != 1 {
					t.Fatalf("%v leaks or spans lines: %q", args, text)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
				t.Errorf("failed pull wrote a file: %v", err)
			}
		})
	}
}

func TestLogsOfAStoppedApplicationSayWhy(t *testing.T) {
	s := &server{status: "exited:unhealthy", notRunning: true}
	instance := newServer(t, s)
	dir := linkedDirectory(t)
	for _, args := range [][]string{{"logs"}, {"logs", "--follow"}, {"logs", "--format", "json"}} {
		out, _, err := run(t, instance.URL, dir, "", args...)
		if err == nil || ui.ExitCode(err) != 1 || strings.Contains(out, `"type":"logs"`) || strings.Contains(out, "\n\n") {
			t.Fatalf("%v: out=%q err=%v", args, out, err)
		}
		if !strings.Contains(err.Error(), "application is not running (status exited:unhealthy)") || strings.Contains(err.Error(), "400") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	// A container that goes away mid-follow is reported with the status read
	// then, not the running one seen when the follow started.
	s = &server{stopAfter: 1, logs: []string{"2026-09-09T10:00:00Z hello\n"}}
	instance = newServer(t, s)
	out, _, err := run(t, instance.URL, dir, "", "logs", "--follow")
	if err == nil || ui.ExitCode(err) != 1 || !strings.Contains(out, "hello") {
		t.Fatalf("follow past a stop: out=%q err=%v", out, err)
	}
	if !strings.Contains(err.Error(), "application is not running (status exited:unhealthy)") || strings.Contains(err.Error(), "running:healthy") {
		t.Fatalf("follow past a stop: %v", err)
	}
	// Out-of-range --lines fails before any request.
	before := s.counts()["GET /api/v1/applications/app-1/logs"]
	if _, _, err := run(t, instance.URL, dir, "", "logs", "-n", "10001"); !errors.Is(err, service.ErrInput) {
		t.Fatalf("10001 lines: %v", err)
	}
	if after := s.counts()["GET /api/v1/applications/app-1/logs"]; after != before {
		t.Fatalf("out-of-range lines reached the server: %d -> %d", before, after)
	}
}

func TestLinkUsesTheSavedDefaultInstanceWithoutAsking(t *testing.T) {
	s := &server{}
	instance := newServer(t, s)
	// A closed port for the other instance: reaching it is the failure to detect.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + listener.Addr().String()
	listener.Close()
	configPath := filepath.Join(t.TempDir(), "config.json")
	write := func(defaultName string) {
		t.Helper()
		var instances []map[string]any
		for name, url := range map[string]string{"other": closed, "lab": instance.URL} {
			token := "synthetic-other-token"
			if name == "lab" {
				token = testToken
			}
			instances = append(instances, map[string]any{"name": name, "fqdn": url, "token": token, "default": name == defaultName})
		}
		data, err := json.Marshal(map[string]any{"instances": instances})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("lab")
	dir := projectDirectory(t)
	out, _, err := runWithFile(t, configPath, dir, "", "link", "--project", "Personal", "--application", "fenix-bot", "--format", "json")
	if err != nil {
		t.Fatalf("link with a default: %v", err)
	}
	var result service.LinkResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Target.Instance != "lab" {
		t.Fatalf("link result %s: %v", out, err)
	}
	// --context still overrides the default.
	_, _, err = runWithFile(t, configPath, dir, "", "link", "--context", "other", "--project", "Personal", "--application", "fenix-bot", "--replace")
	if err == nil || !strings.Contains(err.Error(), "connection was refused") || strings.Contains(err.Error(), closed) {
		t.Fatalf("--context other: %v", err)
	}
	// Without a default, a noninteractive link must be told which instance.
	write("")
	if _, _, err := runWithFile(t, configPath, projectDirectory(t), "", "link", "--project", "Personal", "--application", "fenix-bot"); !errors.Is(err, service.ErrInput) || !strings.Contains(err.Error(), "--context") {
		t.Fatalf("no default: %v", err)
	}
}
