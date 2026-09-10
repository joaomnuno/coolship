package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Stored is one instance as written to the Coolify CLI configuration.
type Stored struct {
	Name  string
	URL   string
	Token string `json:"-"`
}

// NormalizeURL exposes the instance URL rules for callers that collect input.
func NormalizeURL(value string) (string, error) { return normalizeURL(value) }

// ValidateName accepts the context names Coolify CLI accepts on the command line.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: context name is required", ErrInvalid)
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: context name must not contain whitespace", ErrInvalid)
	}
	return nil
}

// Save adds or replaces one instance in the Coolify CLI configuration and
// returns the path written. Fields this package does not know, on the file
// and on other instances, are kept as they are so Coolify CLI's own data
// survives. The first instance, or an explicit request, becomes the default.
func Save(path string, instance Stored, makeDefault bool) (string, error) {
	if err := ValidateName(instance.Name); err != nil {
		return "", err
	}
	address, err := normalizeURL(instance.URL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(instance.Token) == "" || strings.ContainsAny(instance.Token, "\r\n") {
		return "", fmt.Errorf("%w: token must be nonempty and contain no line breaks", ErrInvalid)
	}
	path, document, instances, err := readDocument(path)
	if err != nil {
		return "", err
	}
	entry := map[string]any{"name": instance.Name, "fqdn": address, "token": instance.Token, "default": false}
	replaced := false
	hasDefault := false
	for i, existing := range instances {
		if name, _ := existing["name"].(string); name == instance.Name {
			for key, value := range entry {
				existing[key] = value // keep unknown keys of the same instance
			}
			instances[i] = existing
			entry = existing
			replaced = true
		}
	}
	if !replaced {
		instances = append(instances, entry)
	}
	for _, existing := range instances {
		if isDefault, _ := existing["default"].(bool); isDefault && existing["name"] != instance.Name {
			hasDefault = true
		}
	}
	if makeDefault || !hasDefault {
		for _, existing := range instances {
			existing["default"] = existing["name"] == instance.Name
		}
	}
	document["instances"] = instances
	return path, writeDocument(path, document)
}

// Remove deletes one instance by name and reports whether it was the default.
func Remove(path, name string) (string, bool, error) {
	path, document, instances, err := readDocument(path)
	if err != nil {
		return "", false, err
	}
	kept := instances[:0]
	found, wasDefault := false, false
	for _, existing := range instances {
		if existingName, _ := existing["name"].(string); existingName == name {
			found = true
			wasDefault, _ = existing["default"].(bool)
			continue
		}
		kept = append(kept, existing)
	}
	if !found {
		return path, false, fmt.Errorf("%w: %q", ErrContextNotFound, name)
	}
	document["instances"] = kept
	return path, wasDefault, writeDocument(path, document)
}

func readDocument(path string) (string, map[string]any, []map[string]any, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return "", nil, nil, err
		}
	}
	document := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, nil, fmt.Errorf("read Coolify CLI configuration: %w", err)
	}
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			return "", nil, nil, fmt.Errorf("%w: Coolify CLI configuration is not a JSON object; fix or remove %s", ErrInvalid, path)
		}
	}
	var instances []map[string]any
	if raw, ok := document["instances"].([]any); ok {
		for _, item := range raw {
			if entry, ok := item.(map[string]any); ok {
				instances = append(instances, entry)
			}
		}
	}
	return path, document, instances, nil
}

// writeDocument replaces the file atomically with private permissions, as
// Coolify CLI does (directory 0750, file 0600).
func writeDocument(path string, document map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Coolify CLI configuration: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("write Coolify CLI configuration: %w", err)
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write Coolify CLI configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("replace Coolify CLI configuration: %w", err)
	}
	return nil
}
