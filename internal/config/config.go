// Package config defines the versioned, credential-free project configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var ErrInvalid = errors.New("invalid project configuration")

type Config struct {
	Version int     `toml:"version" json:"version"`
	Project Binding `toml:"project" json:"project"`
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
}

// Parse rejects unknown fields, unsupported versions, and incomplete bindings.
func Parse(data []byte) (Config, error) {
	var result Config
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&result); err != nil {
		// Parser errors can contain source excerpts, including accidentally supplied secrets.
		return Config{}, fmt.Errorf("%w: TOML contains invalid syntax, types, or unknown fields", ErrInvalid)
	}
	if result.Project.Root == "" {
		result.Project.Root = "."
	}
	if err := Validate(result); err != nil {
		return Config{}, err
	}
	return result, nil
}

func Marshal(value Config) ([]byte, error) {
	if value.Project.Root == "" {
		value.Project.Root = "."
	}
	if err := Validate(value); err != nil {
		return nil, err
	}
	data, err := toml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode project configuration: %w", err)
	}
	return data, nil
}

func Validate(value Config) error {
	if value.Version != 1 {
		return fmt.Errorf("%w: version must be 1", ErrInvalid)
	}
	for _, field := range []struct{ name, selector, pin string }{
		{"project", value.Project.Project, value.Project.ProjectUUID},
		{"environment", value.Project.Environment, value.Project.EnvironmentUUID},
		{"application", value.Project.Application, value.Project.ApplicationUUID},
	} {
		if strings.TrimSpace(field.selector) == "" && strings.TrimSpace(field.pin) == "" {
			return fmt.Errorf("%w: project.%s or project.%s_uuid is required", ErrInvalid, field.name, field.name)
		}
		if field.pin != strings.TrimSpace(field.pin) {
			return fmt.Errorf("%w: project.%s_uuid must not contain surrounding whitespace", ErrInvalid, field.name)
		}
	}
	root := value.Project.Root
	if root == "" {
		root = "."
	}
	clean := filepath.Clean(root)
	if filepath.IsAbs(root) || filepath.VolumeName(root) != "" || strings.ContainsAny(root, "\\\x00") ||
		clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
		(len(root) >= 2 && root[1] == ':') {
		return fmt.Errorf("%w: project.root must stay inside the configuration directory", ErrInvalid)
	}
	return nil
}
