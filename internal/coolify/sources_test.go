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

func TestPrivateSourceEndpoints(t *testing.T) {
	var bodies []map[string]any
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			bodies = append(bodies, body)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/github-apps":
			fmt.Fprint(w, `[{"id":0,"uuid":"pub","name":"Public GitHub","is_public":true,"team_id":0,"organization":null,"client_secret":"x"},
				{"id":3,"uuid":"gh-3","name":"docs-app","organization":"Org","html_url":"https://github.com","is_public":false,"is_system_wide":false,"team_id":0}]`)
		case "GET /api/v1/github-apps/3/repositories/owner/repo/branches":
			fmt.Fprint(w, `{"branches":[{"name":"main","protected":false},{"name":"dev"}]}`)
		case "GET /api/v1/github-apps/3/repositories/owner/gone/branches":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Error loading branches from GitHub.","error":"Not Found"}`)
		case "GET /api/v1/security/keys":
			fmt.Fprint(w, `[{"id":0,"uuid":"k0","name":"localhost's key","private_key":"-----BEGIN SECRET","public_key":"ssh-ed25519 AAAA host","fingerprint":"f0","is_git_related":false},
				{"id":4,"uuid":"k4","name":"deploy","description":null,"public_key":"ssh-ed25519 BBBB deploy","fingerprint":"f4","is_git_related":false}]`)
		case "POST /api/v1/security/keys":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"uuid":"k9"}`)
		case "POST /api/v1/applications/private-github-app":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"uuid":"a-gh","domains":"https://a-gh.example.test"}`)
		case "POST /api/v1/applications/private-deploy-key":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"uuid":"a-key","domains":"https://a-key.example.test"}`)
		default:
			t.Errorf("unexpected endpoint %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	apps, err := client.ListGitHubApps(ctx)
	if err != nil || len(apps) != 2 || !apps[0].IsPublic || apps[1] != (models.GitHubApp{ID: 3, UUID: "gh-3", Name: "docs-app", Organization: "Org", HTMLURL: "https://github.com"}) {
		t.Fatalf("apps = %#v, %v", apps, err)
	}
	branches, err := client.ListGitHubBranches(ctx, 3, "owner", "repo")
	if err != nil || len(branches) != 2 || branches[0].Name != "main" || branches[1].Name != "dev" {
		t.Fatalf("branches = %#v, %v", branches, err)
	}
	var responseError *HTTPError
	if _, err := client.ListGitHubBranches(ctx, 3, "owner", "gone"); !errors.As(err, &responseError) || responseError.StatusCode != 404 {
		t.Fatalf("missing repository = %v", err)
	}
	if _, err := client.ListGitHubBranches(ctx, -1, "owner", "repo"); err == nil {
		t.Fatal("negative id accepted")
	}
	keys, err := client.ListPrivateKeys(ctx)
	if err != nil || len(keys) != 2 || keys[0].Name != "localhost's key" || keys[1] != (models.PrivateKey{ID: 4, UUID: "k4", Name: "deploy", PublicKey: "ssh-ed25519 BBBB deploy", Fingerprint: "f4"}) {
		t.Fatalf("keys = %#v, %v", keys, err)
	}
	created, err := client.CreatePrivateKey(ctx, "new-key", "for owner/repo", "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n")
	if err != nil || created.UUID != "k9" || created.Name != "new-key" {
		t.Fatalf("created key = %#v, %v", created, err)
	}
	if _, err := client.CreatePrivateKey(ctx, "", "", "x"); err == nil {
		t.Fatal("nameless key accepted")
	}

	spec := models.ApplicationSpec{ProjectUUID: "p1", EnvironmentName: "production", ServerUUID: "s1", Name: "web",
		GitRepository: "https://github.com/owner/repo", GitBranch: "main", BuildPack: "dockerfile", PortsExposes: "80", Source: "github-app", GitHubAppUUID: "gh-3"}
	application, err := client.CreateApplication(ctx, spec)
	if err != nil || application.UUID != "a-gh" {
		t.Fatalf("github-app application = %#v, %v", application, err)
	}
	spec.Source, spec.GitHubAppUUID, spec.PrivateKeyUUID, spec.GitRepository = "deploy-key", "", "k4", "git@github.com:owner/repo.git"
	if application, err = client.CreateApplication(ctx, spec); err != nil || application.UUID != "a-key" {
		t.Fatalf("deploy-key application = %#v, %v", application, err)
	}
	for _, bad := range []models.ApplicationSpec{
		{Source: "github-app"}, {Source: "github-app", PrivateKeyUUID: "k4", GitHubAppUUID: "gh-3"},
		{Source: "deploy-key"}, {Source: "public", GitHubAppUUID: "gh-3"}, {Source: "other"},
	} {
		bad.ProjectUUID, bad.EnvironmentName, bad.ServerUUID, bad.Name, bad.GitRepository, bad.GitBranch, bad.BuildPack, bad.PortsExposes = "p1", "production", "s1", "web", "https://github.com/owner/repo", "main", "dockerfile", "80"
		if _, err := client.CreateApplication(ctx, bad); err == nil {
			t.Errorf("spec %+v accepted", bad)
		}
	}
	if len(bodies) != 3 {
		t.Fatalf("%d requests were sent, want 3", len(bodies))
	}
	if key := bodies[0]; key["name"] != "new-key" || key["description"] != "for owner/repo" || !strings.HasPrefix(key["private_key"].(string), "-----BEGIN OPENSSH") {
		t.Errorf("key body = %v", key)
	}
	if gh := bodies[1]; gh["github_app_uuid"] != "gh-3" || gh["private_key_uuid"] != nil || gh["git_repository"] != "https://github.com/owner/repo" || gh["instant_deploy"] != false {
		t.Errorf("github-app body = %v", gh)
	}
	if key := bodies[2]; key["private_key_uuid"] != "k4" || key["github_app_uuid"] != nil || key["git_repository"] != "git@github.com:owner/repo.git" {
		t.Errorf("deploy-key body = %v", key)
	}
}
