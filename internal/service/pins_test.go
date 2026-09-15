package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joaomnuno/coolship/internal/config"
)

// A pinned application that no longer exists is found by its name; status
// warns once and reads the history of the application it found, not of the
// missing pin, whose history read was started first and must be discarded.
func TestStatusWithAMissingApplicationPinUsesTheNameAndWarnsOnce(t *testing.T) {
	f := newBackend()
	app, _, _ := testApp(f)
	options := linkedOptions(t)
	data, err := config.Marshal(config.Config{Version: 1, Project: config.Binding{Context: "home",
		Project: "Personal", ProjectUUID: "project-1", Environment: "production", EnvironmentUUID: "env-1",
		Application: "api", ApplicationUUID: "deleted-app", Root: "."}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(options.CWD, "coolship.toml"), data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := app.Status(context.Background(), options)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	want := []string{`coolship.toml pins application deleted-app that no longer exists; found "api" by name. Run coolship link to refresh.`}
	if result.Target.ApplicationUUID != "app-1" || !reflect.DeepEqual(result.Warnings, want) {
		t.Fatalf("status = %+v, want app-1 with warnings %q", result, want)
	}
	// Resolution never rewrites the file; link does.
	if unchanged, _ := os.ReadFile(filepath.Join(options.CWD, "coolship.toml")); string(unchanged) != string(data) {
		t.Fatal("status rewrote coolship.toml")
	}
}
