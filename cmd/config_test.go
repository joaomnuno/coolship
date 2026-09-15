package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/preferences"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

func runConfig(t *testing.T, path string, args ...string) (string, string, error) {
	t.Helper()
	app := fakeApplication{config: func(context.Context, service.Options) (service.ConfigResult, error) {
		return service.ConfigResult{ConfigPath: "/p/coolship.toml", Target: "default", AppRoot: "/p", CredentialSource: "file"}, nil
	}}
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(app, ui.Streams{Out: &out, Err: &diagnostic}, "test", cmd.WithPreferences(preferences.Inspect(path)))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}

func TestConfigGetPrintsTheValueInEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	if err := os.WriteFile(path, []byte("verbosity = \"debug\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for args, want := range map[string]string{"verbosity": "debug\n", "hints": "true\n", "build_logs": "auto\n"} {
		out, diagnostic, err := runConfig(t, path, "config", "get", args)
		if err != nil || out != want || diagnostic != "" {
			t.Errorf("get %s: out=%q stderr=%q err=%v", args, out, diagnostic, err)
		}
	}
	out, _, err := runConfig(t, path, "config", "get", "verbosity", "--format", "json")
	var decoded ui.PreferenceResult
	if err != nil || json.Unmarshal([]byte(out), &decoded) != nil || decoded != (ui.PreferenceResult{Key: "verbosity", Value: "debug", Set: true, Path: path}) {
		t.Fatalf("get json: %q %v", out, err)
	}
	_, _, err = runConfig(t, path, "config", "get", "colour")
	if ui.ExitCode(err) != 2 || !strings.Contains(err.Error(), `unknown preference "colour" (did you mean "color"?)`) {
		t.Fatalf("unknown key: %v (exit %d)", err, ui.ExitCode(err))
	}
	if _, _, err := runConfig(t, path, "config", "get"); ui.ExitCode(err) != 2 {
		t.Fatalf("missing key: %v", err)
	}
}

func TestConfigSetWritesOneKeyQuietly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coolship", "preferences.toml")
	out, diagnostic, err := runConfig(t, path, "config", "set", "hints", "false")
	if err != nil || out != "" || diagnostic != "" {
		t.Fatalf("set: out=%q stderr=%q err=%v", out, diagnostic, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "hints = false\n" {
		t.Fatalf("file = %q", data)
	}
	out, _, err = runConfig(t, path, "config", "set", "color", "never", "--format", "json")
	if err != nil || !strings.Contains(out, `"key":"color","value":"never","set":true`) {
		t.Fatalf("set json: %q %v", out, err)
	}
	_, _, err = runConfig(t, path, "config", "set", "hints", "maybe")
	if ui.ExitCode(err) != 2 || !strings.Contains(err.Error(), `hints must be one of true, false, not "maybe"`) {
		t.Fatalf("bad value: %v", err)
	}
	if err := os.WriteFile(path, []byte("verbosty = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = runConfig(t, path, "config", "set", "hints", "true")
	if ui.ExitCode(err) != 2 || !errors.Is(err, preferences.ErrInvalidFile) || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("broken file: %v", err)
	}
}

func TestConfigWithoutATerminalPrintsTheShowView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	show, _, err := runConfig(t, path, "config", "show")
	if err != nil || !strings.Contains(show, "Configuration:    /p/coolship.toml\n") {
		t.Fatalf("show: %q %v", show, err)
	}
	bare, _, err := runConfig(t, path, "config")
	if err != nil || bare != show {
		t.Fatalf("bare config = %q; want the show view %q (%v)", bare, show, err)
	}
	if _, _, err := runConfig(t, path, "config", "shw"); ui.ExitCode(err) != 2 || !strings.Contains(err.Error(), `did you mean "show"`) {
		t.Fatalf("unknown subcommand: %v", err)
	}
	help, _, _ := runConfig(t, path, "config", "--help")
	if strings.Index(help, "  show") > strings.Index(help, "  get") || strings.Index(help, "  get") > strings.Index(help, "  set") {
		t.Fatalf("help lists subcommands out of order:\n%s", help)
	}
}
