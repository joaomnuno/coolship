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
			if body["instant_deploy"] != false || body["ports_exposes"] != "80" || body["build_pack"] != "dockerfile" || body["is_static"] != nil || body["base_directory"] != nil {
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
	spec := models.ApplicationSpec{ProjectUUID: "p1", EnvironmentName: "production", ServerUUID: "s1", Name: "web",
		GitRepository: "https://github.com/owner/repo", GitBranch: "main", BuildPack: "dockerfile", PortsExposes: "80"}
	created, err := client.CreateApplication(ctx, spec)
	if err != nil || created.UUID != "a9" || created.Domains != "https://a9.example.test" {
		t.Fatalf("created = %#v, %v", created, err)
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
