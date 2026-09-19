package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joaomnuno/coolship/internal/config"
)

func TestCompletionTargetsReadsNamedTargetsAndNeverFails(t *testing.T) {
	app, _, _ := testApp(newBackend())

	// A single [project] binding has no named target to complete.
	if targets := app.CompletionTargets(linkedOptions(t)); targets != nil {
		t.Fatalf("single binding -> %+v", targets)
	}

	dir := t.TempDir()
	data, err := config.Marshal(config.Config{Version: 1, Apps: map[string]config.Binding{
		"web": {Context: "home", Project: "Personal", Environment: "staging", Application: "web-frontend", Root: "web"},
		"api": {Context: "home", Project: "Personal", Environment: "production", Application: "api-service", Root: "api"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coolship.toml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	// Sorted by name, with the binding as the description a shell shows.
	want := []CompletionTarget{
		{Name: "api", Purpose: "Personal / production / api-service"},
		{Name: "web", Purpose: "Personal / staging / web-frontend"},
	}
	if got := app.CompletionTargets(Options{CWD: dir}); !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}

	// A shell has nowhere to show an error and no business failing the command
	// being typed, so every unreadable case is an empty list.
	if targets := app.CompletionTargets(Options{CWD: unlinkedDirectory(t)}); targets != nil {
		t.Fatalf("unlinked -> %+v", targets)
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "coolship.toml"), []byte("this is not toml = = ="), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(broken, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if targets := app.CompletionTargets(Options{CWD: broken}); targets != nil {
		t.Fatalf("unparseable -> %+v", targets)
	}
	if targets := app.CompletionTargets(Options{CWD: filepath.Join(dir, "does-not-exist")}); targets != nil {
		t.Fatalf("missing directory -> %+v", targets)
	}
}

func TestCompletionTargetsAsksForNoCredentialsAndNoBackend(t *testing.T) {
	// A completion must not build a client or read a token: a Tab cannot be
	// allowed to fail on an expired login or an unreachable instance.
	app := New(Dependencies{})
	dir := t.TempDir()
	data, err := config.Marshal(config.Config{Version: 1, Apps: map[string]config.Binding{
		"api": {Context: "home", Project: "Personal", Environment: "production", Application: "api-service", Root: "."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coolship.toml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	targets := app.CompletionTargets(Options{CWD: dir})
	if len(targets) != 1 || targets[0].Name != "api" {
		t.Fatalf("targets = %+v", targets)
	}
}
