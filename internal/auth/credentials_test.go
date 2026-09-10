package auth_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/internal/auth"
	"github.com/pelletier/go-toml/v2"
)

const syntheticToken = "synthetic-token-for-auth-tests"

func credentialsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(contents), 0400); err != nil {
		t.Fatal(err)
	}
	return path
}

const compatible = `{
  "instances": [
    {"name":"home","fqdn":"https://COOLIFY.example.com/","token":"synthetic-token-for-auth-tests","default":true},
    {"name":"work","fqdn":"http://localhost:8000","token":"synthetic-work-token"}
  ],
  "lastUpdateCheckTime":"2026-09-09T00:00:00Z",
  "futureMetadata":{"ignored":true}
}`

func TestCompatibleContextsAreReadOnly(t *testing.T) {
	path := credentialsFile(t, compatible)
	value, err := auth.Resolve(auth.Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if value.Name != "home" || value.URL != "https://coolify.example.com" || value.Token != syntheticToken {
		t.Fatalf("unexpected default: %v", value)
	}
	value, err = auth.Resolve(auth.Options{ConfigPath: path, Context: "work"})
	if err != nil || value.Name != "work" || value.Token != "synthetic-work-token" {
		t.Fatalf("context override failed: %v (%v)", value, err)
	}
	if _, err := auth.Resolve(auth.Options{ConfigPath: path, Context: "missing"}); !errors.Is(err, auth.ErrContextNotFound) {
		t.Fatalf("missing context must not fall back: %v", err)
	}
	instances, err := auth.List(auth.Options{ConfigPath: path})
	if err != nil || len(instances) != 2 || instances[0].Name != "home" || !instances[0].Default {
		t.Fatalf("list failed: %#v (%v)", instances, err)
	}
	encoded, err := json.Marshal(instances)
	if err != nil || strings.Contains(string(encoded), "token") {
		t.Fatalf("context list exposes credential fields: %s (%v)", encoded, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != compatible {
		t.Fatalf("credentials file was modified: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatalf("credentials permissions changed: %v (%v)", info, err)
	}
}

func TestInvalidCredentialsDoNotDiscloseSecrets(t *testing.T) {
	tests := map[string]string{
		"empty":             `{"instances":[]}`,
		"invalid json":      `{"instances": "` + syntheticToken,
		"wrong type":        `{"instances":[{"name":"home","fqdn":17,"token":"` + syntheticToken + `"}]}`,
		"missing name":      `{"instances":[{"fqdn":"https://example.com","token":"` + syntheticToken + `"}]}`,
		"missing token":     `{"instances":[{"name":"home","fqdn":"https://example.com"}]}`,
		"duplicate names":   strings.Replace(compatible, `"name":"work"`, `"name":"home"`, 1),
		"multiple defaults": strings.Replace(compatible, `"token":"synthetic-work-token"`, `"token":"synthetic-work-token","default":true`, 1),
		"url credentials":   strings.Replace(compatible, "https://COOLIFY.example.com/", "https://user:"+syntheticToken+"@example.com", 1),
		"url query":         strings.Replace(compatible, "https://COOLIFY.example.com/", "https://example.com/?token="+syntheticToken, 1),
		"url fragment":      strings.Replace(compatible, "https://COOLIFY.example.com/", "https://example.com/#"+syntheticToken, 1),
		"url scheme":        strings.Replace(compatible, "https://COOLIFY.example.com/", "ftp://example.com", 1),
		"url no host":       strings.Replace(compatible, "https://COOLIFY.example.com/", "https:///path", 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := credentialsFile(t, data)
			_, err := auth.Resolve(auth.Options{ConfigPath: path})
			if !errors.Is(err, auth.ErrInvalid) {
				t.Fatalf("expected credentials error: %v", err)
			}
			if strings.Contains(err.Error(), syntheticToken) {
				t.Fatal("error exposes credentials")
			}
		})
	}
}

func TestExplicitContextWorksWithoutDefault(t *testing.T) {
	path := credentialsFile(t, strings.Replace(compatible, `"default":true`, `"default":false`, 1))
	if _, err := auth.Resolve(auth.Options{ConfigPath: path}); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("missing default must require context: %v", err)
	}
	if _, err := auth.Resolve(auth.Options{ConfigPath: path, Context: "work"}); err != nil {
		t.Fatalf("explicit context should not need default: %v", err)
	}
	if _, err := auth.List(auth.Options{ConfigPath: path}); err != nil {
		t.Fatalf("interactive listing should not need default: %v", err)
	}
}

func TestExplicitPairIsIndependentAndCannotMixSources(t *testing.T) {
	value, err := auth.Resolve(auth.Options{URL: "https://example.com/base/", Token: syntheticToken})
	if err != nil || value.Name != "environment" || value.URL != "https://example.com/base" || value.Token != syntheticToken {
		t.Fatalf("explicit pair failed: %v (%v)", value, err)
	}
	for name, options := range map[string]auth.Options{
		"token alone":           {Token: syntheticToken},
		"url alone":             {URL: "https://example.com"},
		"context conflict":      {URL: "https://example.com", Token: syntheticToken, Context: "home"},
		"config conflict":       {URL: "https://example.com", Token: syntheticToken, ConfigPath: "not-read.json"},
		"url invalid":           {URL: "https://user:" + syntheticToken + "@example.com", Token: syntheticToken},
		"token newline":         {URL: "https://example.com", Token: syntheticToken + "\n"},
		"token whitespace only": {URL: "https://example.com", Token: " "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := auth.Resolve(options)
			if !errors.Is(err, auth.ErrInvalid) || strings.Contains(err.Error(), syntheticToken) {
				t.Fatalf("expected sanitized pair error: %v", err)
			}
			if _, err := auth.List(options); !errors.Is(err, auth.ErrInvalid) {
				t.Fatalf("listing must enforce pair conflicts: %v", err)
			}
		})
	}
}

func TestCredentialsSerializationAndFormattingRedactToken(t *testing.T) {
	value := auth.Credentials{Name: "home", URL: "https://example.com", Token: syntheticToken}
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	tomlBytes, err := toml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"JSON": string(jsonBytes), "TOML": string(tomlBytes),
		"fmt": fmt.Sprintf("%v %+v %#v", value, value, value),
	} {
		if strings.Contains(output, syntheticToken) {
			t.Fatalf("%s exposes token", name)
		}
	}
}

func TestDefaultPathMirrorsCoolifyCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix home and XDG precedence")
	}
	home, xdg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path, err := auth.DefaultPath()
	if err != nil || path != filepath.Join(home, ".config", "coolify", "config.json") {
		t.Fatalf("must use home even when XDG_CONFIG_HOME is set: %q (%v)", path, err)
	}
	t.Setenv("HOME", "")
	path, err = auth.DefaultPath()
	if err != nil || path != filepath.Join(xdg, "coolify", "config.json") {
		t.Fatalf("home failure must use XDG fallback: %q (%v)", path, err)
	}
}

func TestMissingCredentialsSayHowToGetThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.json")
	want := "No Coolify credentials at " + path + "; run coolship login, or set COOLSHIP_URL and COOLSHIP_TOKEN"
	if _, err := auth.Resolve(auth.Options{ConfigPath: path}); err == nil || err.Error() != want {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := auth.List(auth.Options{ConfigPath: path}); err == nil || err.Error() != want {
		t.Fatalf("list: %v", err)
	}
	if err := auth.MissingCredentials(path); err.Error() != want {
		t.Fatalf("MissingCredentials: %v", err)
	}
	// A file with no default names both ways out.
	path = credentialsFile(t, strings.Replace(compatible, `"default":true`, `"default":false`, 1))
	_, err := auth.Resolve(auth.Options{ConfigPath: path})
	if !errors.Is(err, auth.ErrInvalid) || !strings.HasSuffix(err.Error(), "no default instance; pass --context NAME or run coolship login --default") {
		t.Fatalf("no default: %v", err)
	}
	// Other read failures still name the file.
	unreadable := filepath.Join(t.TempDir(), "dir")
	if err := os.Mkdir(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Resolve(auth.Options{ConfigPath: unreadable}); err == nil || !strings.Contains(err.Error(), unreadable) || strings.Contains(err.Error(), "run coolship login") {
		t.Fatalf("directory as file: %v", err)
	}
}
