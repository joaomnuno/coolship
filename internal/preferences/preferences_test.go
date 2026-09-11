package preferences

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestResolveFollowsXDGThenHomeThenAppData(t *testing.T) {
	home := func() (string, error) { return filepath.Join(string(filepath.Separator), "home", "dev"), nil }
	noHome := func() (string, error) { return "", errors.New("$HOME is not defined") }
	xdg := filepath.Join(string(filepath.Separator), "xdg")
	tests := []struct {
		name string
		goos string
		env  map[string]string
		home func() (string, error)
		want string
		fail bool
	}{
		{"absolute XDG_CONFIG_HOME", "linux", map[string]string{"XDG_CONFIG_HOME": xdg}, home, filepath.Join(xdg, "coolship", "preferences.toml"), false},
		{"relative XDG_CONFIG_HOME is ignored", "linux", map[string]string{"XDG_CONFIG_HOME": "cfg"}, home, filepath.Join(string(filepath.Separator), "home", "dev", ".config", "coolship", "preferences.toml"), false},
		{"no XDG_CONFIG_HOME", "darwin", nil, home, filepath.Join(string(filepath.Separator), "home", "dev", ".config", "coolship", "preferences.toml"), false},
		{"XDG_CONFIG_HOME without a home", "linux", map[string]string{"XDG_CONFIG_HOME": xdg}, noHome, filepath.Join(xdg, "coolship", "preferences.toml"), false},
		{"nothing to go on", "linux", nil, noHome, "", true},
		{"windows AppData", "windows", map[string]string{"APPDATA": `C:\Users\dev\AppData\Roaming`, "XDG_CONFIG_HOME": xdg}, home, filepath.Join(`C:\Users\dev\AppData\Roaming`, "coolship", "preferences.toml"), false},
		{"windows without AppData", "windows", nil, home, filepath.Join(string(filepath.Separator), "home", "dev", "AppData", "Roaming", "coolship", "preferences.toml"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolve(tt.goos, env(tt.env), tt.home)
			if tt.fail {
				if err == nil || !strings.Contains(err.Error(), "find preferences home") {
					t.Fatalf("resolve() = %q, %v; want an error naming the home lookup", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("resolve() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestLocateHonorsTheOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "prefs.toml")
	got, err := Locate(env(map[string]string{EnvPath: override, "XDG_CONFIG_HOME": t.TempDir()}))
	if err != nil || got != override {
		t.Fatalf("Locate() = %q, %v; want %q", got, err, override)
	}
	xdg := t.TempDir()
	if got, err := Locate(env(map[string]string{"XDG_CONFIG_HOME": xdg})); err != nil || got != filepath.Join(xdg, "coolship", "preferences.toml") {
		t.Fatalf("Locate() without the override = %q, %v", got, err)
	}
}

func TestDiscoverUsesTheOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	if err := os.WriteFile(path, []byte("color = \"never\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Discover(env(map[string]string{EnvPath: path}))
	if report.Err != nil || report.Path != path || !report.Present || report.Preferences.Color != ColorNever {
		t.Fatalf("Discover() = %+v", report)
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coolship", "preferences.toml")
	got, err := Load(path)
	if err != nil || got != (Preferences{}) {
		t.Fatalf("Load(missing) = %+v, %v; want the zero value and no error", got, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load created the file: %v", err)
	}
	report := Inspect(path)
	if report.Present || report.Err != nil || report.Path != path {
		t.Fatalf("Inspect(missing) = %+v", report)
	}
}

func TestLoadReadsEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences.toml")
	content := "verbosity = \"verbose\"\nbuild_logs = false\ncolor = \"always\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.Verbosity != VerbosityVerbose || got.Color != ColorAlways || got.BuildLogs == nil || *got.BuildLogs {
		t.Fatalf("Load() = %+v, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("verbosity = \"debug\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil || got.Verbosity != VerbosityDebug || got.BuildLogs != nil || got.Color != "" {
		t.Fatalf("Load() with build_logs absent = %+v, %v; want a nil BuildLogs", got, err)
	}
}

func TestParseRejectsUnknownKeysByName(t *testing.T) {
	_, err := Parse([]byte("verbosty = \"normal\"\n"))
	if err == nil || !strings.Contains(err.Error(), `unknown key "verbosty"`) {
		t.Fatalf("Parse(unknown key) = %v; want the key named", err)
	}
	_, err = Parse([]byte("verbosty = \"normal\"\n[ui]\ncolour = 1\n"))
	if err == nil || !strings.Contains(err.Error(), `"verbosty"`) || !strings.Contains(err.Error(), `"ui"`) {
		t.Fatalf("Parse(two unknown keys) = %v; want both named", err)
	}
	_, err = Parse([]byte("build_logs = \"yes\"\n"))
	if err == nil || !strings.Contains(err.Error(), `"build_logs"`) {
		t.Fatalf("Parse(wrong type) = %v; want the key named", err)
	}
	// A file that exists but is broken is a typed error naming the file.
	path := filepath.Join(t.TempDir(), "preferences.toml")
	if err := os.WriteFile(path, []byte("verbosity = \"loud\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(path)
	var typed *Error
	if !errors.As(err, &typed) || typed.Path != path || !strings.Contains(err.Error(), path) {
		t.Fatalf("Load(invalid) = %v; want a *Error naming %s", err, path)
	}
	var value *ValueError
	if !errors.As(err, &value) || value.Key != "verbosity" {
		t.Fatalf("Load(invalid) = %v; want a *ValueError for verbosity", err)
	}
}

func TestValidateNamesKeyAndAllowedValues(t *testing.T) {
	tests := []struct {
		name  string
		value Preferences
		want  string
	}{
		{"verbosity", Preferences{Verbosity: "loud"}, `verbosity must be one of normal, verbose, debug, not "loud"`},
		{"color", Preferences{Color: "sometimes"}, `color must be one of auto, always, never, not "sometimes"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.value)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() = %v; want %q", err, tt.want)
			}
		})
	}
	on := true
	for _, ok := range []Preferences{{}, {Verbosity: VerbosityNormal, Color: ColorAuto, BuildLogs: &on}, {Verbosity: VerbosityDebug, Color: ColorNever}} {
		if err := Validate(ok); err != nil {
			t.Fatalf("Validate(%+v) = %v", ok, err)
		}
	}
}

func TestInspectReportsAnUnreadableFile(t *testing.T) {
	path := t.TempDir() // a directory in place of the file cannot be read
	report := Inspect(path)
	var typed *Error
	if !report.Present || !errors.As(report.Err, &typed) || typed.Path != path {
		t.Fatalf("Inspect(directory) = %+v", report)
	}
}
