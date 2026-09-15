package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/joaomnuno/coolship/internal/models"
)

func TestProjectStateCountsWithoutValues(t *testing.T) {
	backend := newBackend()
	value := "secret"
	backend.variables = []models.EnvironmentVariable{
		{Key: "A", Value: &value},
		{Key: "A", Value: &value, IsPreview: true},
	}
	app, _, _ := testApp(backend)
	options := linkedOptions(t)
	if err := os.WriteFile(filepath.Join(options.CWD, ".env"), []byte("A=1\nB=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := app.ProjectState(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if state.EnvFile != ".env" || state.LocalVariables != 2 || state.RemoteVariables != 1 {
		t.Fatalf("variables: %+v", state)
	}
	if len(state.Domains) != 1 || state.Generated || state.Target.ApplicationUUID != "app-1" {
		t.Fatalf("domains and target: %+v", state)
	}
	if state.Deployed {
		t.Fatalf("an application without history reads as deployed: %+v", state)
	}
	backend.history = []models.DeploymentRecord{{UUID: "deploy-1", Status: "finished"}}
	if err := os.Remove(filepath.Join(options.CWD, ".env")); err != nil {
		t.Fatal(err)
	}
	state, err = app.ProjectState(context.Background(), options)
	if err != nil || !state.Deployed || state.EnvFile != "" {
		t.Fatalf("deployed without .env: %+v %v", state, err)
	}
}

func TestContextsListsSavedContextsOnly(t *testing.T) {
	app := New(Dependencies{CredentialURL: "https://ci.example.com", CredentialToken: "t",
		ListInstances: func(options auth.Options) ([]auth.Instance, error) {
			if options.URL != "" || options.Token != "" || options.ConfigPath != "/tmp/config.json" {
				t.Fatalf("options = %+v", options)
			}
			return []auth.Instance{{Name: "home", URL: "https://coolify.example.com"}}, nil
		}})
	contexts, err := app.Contexts(Options{CoolifyConfig: "/tmp/config.json"})
	if err != nil || len(contexts) != 1 || contexts[0].Name != "home" {
		t.Fatalf("contexts = %+v, %v", contexts, err)
	}
	missing := New(Dependencies{ListInstances: func(auth.Options) ([]auth.Instance, error) {
		return nil, auth.MissingCredentials("/nowhere")
	}})
	if contexts, err := missing.Contexts(Options{}); err != nil || contexts != nil {
		t.Fatalf("missing file: %+v, %v", contexts, err)
	}
}
