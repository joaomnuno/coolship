// Package config defines the versioned, credential-free project configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var ErrInvalid = errors.New("invalid project configuration")

// Config holds either one binding under [project] or named bindings under
// [apps.<name>]. Mixing the two forms is invalid so a file has one meaning.
type Config struct {
	Version int                `json:"version"`
	Project Binding            `json:"project,omitempty"`
	Apps    map[string]Binding `json:"apps,omitempty"`
}

type Binding struct {
	Context         string `toml:"context,omitempty" json:"context,omitempty"`
	Project         string `toml:"project,omitempty" json:"project,omitempty"`
	Environment     string `toml:"environment,omitempty" json:"environment,omitempty"`
	Application     string `toml:"application,omitempty" json:"application,omitempty"`
	Root            string `toml:"root" json:"root"`
	ProjectUUID     string `toml:"project_uuid,omitempty" json:"project_uuid,omitempty"`
	EnvironmentUUID string `toml:"environment_uuid,omitempty" json:"environment_uuid,omitempty"`
	ApplicationUUID string `toml:"application_uuid,omitempty" json:"application_uuid,omitempty"`
	// Dev is the local command `coolship dev` runs, through the platform shell.
	Dev string `toml:"dev,omitempty" json:"dev,omitempty"`
}

// IsSet reports whether the binding names any remote resource.
func (b Binding) IsSet() bool {
	return b.Project != "" || b.Environment != "" || b.Application != "" ||
		b.ProjectUUID != "" || b.EnvironmentUUID != "" || b.ApplicationUUID != ""
}

// Named reports whether the configuration uses [apps.<name>] tables.
func (c Config) Named() bool { return len(c.Apps) > 0 }

// TargetNames lists named targets in a stable order.
func (c Config) TargetNames() []string {
	names := make([]string, 0, len(c.Apps))
	for name := range c.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Equal compares two configurations by value, with roots normalized.
func Equal(a, b Config) bool {
	return a.Version == b.Version && normalizeRoot(a.Project) == normalizeRoot(b.Project) &&
		maps.EqualFunc(a.Apps, b.Apps, func(x, y Binding) bool { return normalizeRoot(x) == normalizeRoot(y) })
}

func normalizeRoot(b Binding) Binding {
	if b.Root == "" {
		b.Root = "."
	}
	return b
}

// document is the on-disk shape; a pointer keeps [project] absent in the
// named form and lets a decoder reject unknown tables.
type document struct {
	Version int                `toml:"version"`
	Project *Binding           `toml:"project,omitempty"`
	Apps    map[string]Binding `toml:"apps,omitempty"`
}

// Parse rejects unknown fields, unsupported versions, mixed forms, and
// incomplete bindings.
func Parse(data []byte) (Config, error) {
	var doc document
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&doc); err != nil {
		// Parser errors can contain source excerpts, including accidentally supplied secrets.
		return Config{}, fmt.Errorf("%w: TOML contains invalid syntax, types, or unknown fields", ErrInvalid)
	}
	result := Config{Version: doc.Version, Apps: doc.Apps}
	if doc.Project != nil {
		result.Project = *doc.Project
	}
	if !result.Named() && result.Project.Root == "" {
		result.Project.Root = "."
	}
	for name, binding := range result.Apps {
		if binding.Root == "" {
			binding.Root = "."
			result.Apps[name] = binding
		}
	}
	if err := Validate(result); err != nil {
		return Config{}, err
	}
	return result, nil
}

func Marshal(value Config) ([]byte, error) {
	doc := document{Version: value.Version}
	if value.Named() {
		doc.Apps = make(map[string]Binding, len(value.Apps))
		for name, binding := range value.Apps {
			doc.Apps[name] = normalizeRoot(binding)
		}
	} else {
		project := normalizeRoot(value.Project)
		doc.Project = &project
		value.Project = project
	}
	if err := Validate(value); err != nil {
		return nil, err
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode project configuration: %w", err)
	}
	// The encoder emits an empty [apps] header before the first [apps.<name>]
	// table; the file reads better without it and parses identically.
	data = bytes.Replace(data, []byte("[apps]\n[apps."), []byte("[apps."), 1)
	return data, nil
}

var targetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ValidateTargetName accepts the identifiers usable as [apps.<name>] and on
// the command line.
func ValidateTargetName(name string) error {
	if name == "default" {
		return fmt.Errorf("%w: %q is reserved for the single [project] binding", ErrInvalid, name)
	}
	if !targetName.MatchString(name) {
		return fmt.Errorf("%w: target name %q must match [A-Za-z0-9][A-Za-z0-9_-]*", ErrInvalid, name)
	}
	return nil
}

func Validate(value Config) error {
	if value.Version != 1 {
		return fmt.Errorf("%w: version must be 1", ErrInvalid)
	}
	if value.Named() {
		if value.Project.IsSet() {
			return fmt.Errorf("%w: use either a [project] table or [apps.<name>] tables, not both", ErrInvalid)
		}
		for _, name := range value.TargetNames() {
			if err := ValidateTargetName(name); err != nil {
				return err
			}
			if err := validateBinding(value.Apps[name], "apps."+name); err != nil {
				return err
			}
		}
		return nil
	}
	return validateBinding(value.Project, "project")
}

func validateBinding(value Binding, table string) error {
	for _, field := range []struct{ name, selector, pin string }{
		{"project", value.Project, value.ProjectUUID},
		{"environment", value.Environment, value.EnvironmentUUID},
		{"application", value.Application, value.ApplicationUUID},
	} {
		if strings.TrimSpace(field.selector) == "" && strings.TrimSpace(field.pin) == "" {
			return fmt.Errorf("%w: %s.%s or %s.%s_uuid is required", ErrInvalid, table, field.name, table, field.name)
		}
		if field.pin != strings.TrimSpace(field.pin) {
			return fmt.Errorf("%w: %s.%s_uuid must not contain surrounding whitespace", ErrInvalid, table, field.name)
		}
	}
	root := value.Root
	if root == "" {
		root = "."
	}
	clean := filepath.Clean(root)
	if filepath.IsAbs(root) || filepath.VolumeName(root) != "" || strings.ContainsAny(root, "\\\x00") ||
		clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		(len(root) >= 2 && root[1] == ':') {
		return fmt.Errorf("%w: %s.root must stay inside the configuration directory", ErrInvalid, table)
	}
	return nil
}
