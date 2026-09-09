// Package project discovers local configuration and stores reviewed bindings.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joaomnuno/coolship/internal/config"
	"github.com/joaomnuno/coolship/internal/models"
)

var (
	ErrNotLinked           = errors.New("project is not linked; run coolship link")
	ErrConflict            = errors.New("project configuration changed; review and retry linking")
	ErrReplacementRequired = errors.New("binding already exists; use --replace to authorize replacement")
)

type Paths struct {
	CWD        string
	ConfigPath string
}

type Project struct {
	ConfigRoot  string
	ConfigPath  string
	GitRoot     string
	Config      config.Config
	Exists      bool
	Fingerprint string
}

type Target struct {
	Key     string
	AppRoot string
	Binding config.Binding
}

// Context holds resolved identities and never contains credentials.
type Context struct {
	Project       Project
	Target        Target
	InstanceName  string
	InstanceURL   string
	RemoteProject models.Project
	Environment   models.Environment
	Application   models.Application
}

func Select(value Project, environmentOverride string) (Target, error) {
	if !value.Exists {
		return Target{}, ErrNotLinked
	}
	if err := config.Validate(value.Config); err != nil {
		return Target{}, err
	}
	binding := value.Config.Project
	if binding.Root == "" {
		binding.Root = "."
	}
	if environmentOverride != "" && environmentOverride != binding.Environment {
		if binding.EnvironmentUUID != "" || binding.ApplicationUUID != "" {
			return Target{}, fmt.Errorf("%w: --environment conflicts with the environment or application UUID pin; link the requested environment explicitly", config.ErrInvalid)
		}
		binding.Environment = environmentOverride
	}
	root, err := filepath.EvalSymlinks(filepath.Join(value.ConfigRoot, binding.Root))
	if err != nil {
		return Target{}, fmt.Errorf("resolve application root: %w", err)
	}
	relative, err := filepath.Rel(value.ConfigRoot, root)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Target{}, fmt.Errorf("%w: project.root resolves outside the configuration directory", config.ErrInvalid)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Target{}, fmt.Errorf("inspect application root: %w", err)
	}
	if !info.IsDir() {
		return Target{}, fmt.Errorf("%w: project.root must be a directory", config.ErrInvalid)
	}
	return Target{Key: "default", AppRoot: root, Binding: binding}, nil
}
