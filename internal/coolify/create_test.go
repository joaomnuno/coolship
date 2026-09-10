package coolify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/models"
)

func TestCreationEndpointsAndRefusalMessages(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/servers":
			fmt.Fprint(w, `[{"uuid":"s1","name":"Master","ip":"10.0.0.1","is_reachable":true,"is_usable":true,"settings":{"secret":"x"}}]`)
		case "POST /api/v1/projects":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["name"] != "New" || body["description"] != "d" {
				t.Errorf("project body = %#v (%v)", body, err)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"uuid":"p9"}`)
		case "POST /api/v1/applications/public":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			switch body["build_pack"] {
			case "dockerfile":
				if body["instant_deploy"] != false || body["ports_exposes"] != "80" || body["is_static"] != nil || body["base_directory"] != nil ||
					body["dockerfile_location"] != nil || body["docker_compose_location"] != nil || body["docker_compose_domains"] != nil || body["health_check_enabled"] != false {
					t.Errorf("application body = %#v", body)
				}
			case "dockercompose":
				domains, _ := json.Marshal(body["docker_compose_domains"])
				if body["ports_exposes"] != "80" || body["docker_compose_location"] != "/compose.yml" || string(domains) != `[{"domain":"https://web.example.test","name":"web"}]` ||
					body["health_check_enabled"] != nil || body["is_static"] != nil || body["publish_directory"] != nil {
					t.Errorf("compose body = %#v", body)
				}
			case "nixpacks":
				if body["ports_exposes"] != "80" || body["is_static"] != true || body["publish_directory"] != "/build" || body["install_command"] != "npm ci" || body["build_command"] != "npm run build" || body["start_command"] != nil {
					t.Errorf("static build body = %#v", body)
				}
			default:
				t.Errorf("application body = %#v", body)
			}
			if body["git_repository"] == "https://github.com/owner/private" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				fmt.Fprint(w, `{"message":"Validation failed.","errors":{"git_repository":["The git repository is not\naccessible."],"name":"too long"}}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"uuid":"a9","domains":"https://a9.example.test"}`)
		default:
			t.Errorf("unexpected endpoint %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	servers, err := client.ListServers(ctx)
	if err != nil || len(servers) != 1 || servers[0].UUID != "s1" || !servers[0].IsUsable {
		t.Fatalf("servers = %#v, %v", servers, err)
	}
	project, err := client.CreateProject(ctx, "New", "d")
	if err != nil || project.UUID != "p9" || project.Name != "New" {
		t.Fatalf("project = %#v, %v", project, err)
	}
	off := false
	spec := models.ApplicationSpec{ProjectUUID: "p1", EnvironmentName: "production", ServerUUID: "s1", Name: "web",
		GitRepository: "https://github.com/owner/repo", GitBranch: "main", BuildPack: "dockerfile", PortsExposes: "80", HealthCheckEnabled: &off}
	created, err := client.CreateApplication(ctx, spec)
	if err != nil || created.UUID != "a9" || created.Domains != "https://a9.example.test" {
		t.Fatalf("created = %#v, %v", created, err)
	}
	// The build pack refinements are sent only when set, under the server's names.
	compose := spec
	compose.BuildPack, compose.HealthCheckEnabled, compose.DockerComposeLocation = "dockercompose", nil, "/compose.yml"
	compose.DockerComposeDomains = []models.ComposeDomain{{Name: "web", Domain: "https://web.example.test"}}
	if _, err := client.CreateApplication(ctx, compose); err != nil {
		t.Fatalf("compose: %v", err)
	}
	static := spec
	static.BuildPack, static.HealthCheckEnabled, static.IsStatic, static.PublishDirectory = "nixpacks", nil, true, "/build"
	static.InstallCommand, static.BuildCommand = "npm ci", "npm run build"
	if _, err := client.CreateApplication(ctx, static); err != nil {
		t.Fatalf("static build: %v", err)
	}
	spec.GitRepository = "https://github.com/owner/private"
	_, err = client.CreateApplication(ctx, spec)
	var responseError *HTTPError
	if !errors.As(err, &responseError) || responseError.StatusCode != 422 {
		t.Fatalf("refusal = %v", err)
	}
	if want := "Validation failed.; git_repository: The git repository is not accessible.; name: too long"; responseError.Message != want || !strings.Contains(err.Error(), want) {
		t.Fatalf("message = %q, want %q", responseError.Message, want)
	}
	spec.Name = ""
	if _, err := client.CreateApplication(ctx, spec); err == nil {
		t.Fatal("incomplete spec must be rejected before any request")
	}
	if _, err := client.CreateProject(ctx, " ", ""); err == nil {
		t.Fatal("empty project name must be rejected before any request")
	}
}
