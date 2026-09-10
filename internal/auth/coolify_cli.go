package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type storedInstance struct {
	Name    string `json:"name"`
	FQDN    string `json:"fqdn"`
	Token   string `json:"token"`
	Default bool   `json:"default"`
}

// DefaultPath mirrors Coolify CLI, including its limited XDG fallback.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		if fallback := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(fallback) {
			return filepath.Join(fallback, "coolify", "config.json"), nil
		}
		return "", fmt.Errorf("find Coolify credentials home: %w", err)
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "coolify", "config.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "coolify", "config.json"), nil
	}
	return filepath.Join(home, ".config", "coolify", "config.json"), nil
}

// MissingCredentials is the failure every command reports when no Coolify CLI
// configuration exists: it names the file and both ways to get credentials.
func MissingCredentials(path string) error {
	return fmt.Errorf("No Coolify credentials at %s; run coolship login, or set COOLSHIP_URL and COOLSHIP_TOKEN", path)
}

func load(path string) ([]storedInstance, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, MissingCredentials(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read Coolify CLI configuration %s (use --coolify-config or COOLSHIP_URL/COOLSHIP_TOKEN): %w", path, err)
	}
	var document struct {
		Instances []storedInstance `json:"instances"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%w: Coolify CLI configuration is not valid JSON with an instances array", ErrInvalid)
	}
	if len(document.Instances) == 0 {
		return nil, fmt.Errorf("%w: no instances configured", ErrInvalid)
	}
	names := make(map[string]bool, len(document.Instances))
	defaults := 0
	for index := range document.Instances {
		instance := &document.Instances[index]
		if strings.TrimSpace(instance.Name) == "" || strings.TrimSpace(instance.Token) == "" {
			return nil, fmt.Errorf("%w: instance %d needs a nonempty name and token", ErrInvalid, index+1)
		}
		if strings.ContainsAny(instance.Token, "\r\n") {
			return nil, fmt.Errorf("%w: instance %d token must not contain line breaks", ErrInvalid, index+1)
		}
		if names[instance.Name] {
			return nil, fmt.Errorf("%w: duplicate context name %q", ErrInvalid, instance.Name)
		}
		names[instance.Name] = true
		instance.FQDN, err = normalizeURL(instance.FQDN)
		if err != nil {
			return nil, fmt.Errorf("instance %d: %w", index+1, err)
		}
		if instance.Default {
			defaults++
		}
	}
	if defaults > 1 {
		return nil, fmt.Errorf("%w: multiple default instances configured", ErrInvalid)
	}
	return document.Instances, nil
}
