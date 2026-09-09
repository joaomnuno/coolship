// Package project discovers local configuration and stores reviewed bindings.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	CWD         string // effective working directory, absolute and symlink-resolved
	Config      config.Config
	Exists      bool
	Fingerprint string
}

// Target is one selected binding. Key is "default" for the single [project]
// form and the table name for [apps.<name>].
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

// Select picks one target. An explicit name wins; otherwise the single binding
// is used, or among named targets the one whose root most specifically
// contains the working directory. Equal roots or no containing root require
// an explicit name rather than a guess.
func Select(value Project, targetName, environmentOverride string) (Target, error) {
	if !value.Exists {
		return Target{}, ErrNotLinked
	}
	if err := config.Validate(value.Config); err != nil {
		return Target{}, err
	}
	key, binding, err := choose(value, targetName)
	if err != nil {
		return Target{}, err
	}
	if binding.Root == "" {
		binding.Root = "."
	}
	if environmentOverride != "" && environmentOverride != binding.Environment {
		if binding.EnvironmentUUID != "" || binding.ApplicationUUID != "" {
			return Target{}, fmt.Errorf("%w: --environment conflicts with the environment or application UUID pin; link the requested environment explicitly", config.ErrInvalid)
		}
		binding.Environment = environmentOverride
	}
	root, err := resolveRoot(value.ConfigRoot, binding.Root)
	if err != nil {
		return Target{}, err
	}
	return Target{Key: key, AppRoot: root, Binding: binding}, nil
}

func choose(value Project, targetName string) (string, config.Binding, error) {
	if !value.Config.Named() {
		if targetName != "" && targetName != "default" {
			return "", config.Binding{}, fmt.Errorf("%w: this configuration has a single [project] binding, not a target named %q", config.ErrInvalid, targetName)
		}
		return "default", value.Config.Project, nil
	}
	if targetName != "" {
		binding, ok := value.Config.Apps[targetName]
		if !ok {
			return "", config.Binding{}, fmt.Errorf("%w: no target named %q; available: %s", config.ErrInvalid, targetName, strings.Join(value.Config.TargetNames(), ", "))
		}
		return targetName, binding, nil
	}
	// Pick by working directory: deepest root that contains it.
	type candidate struct {
		name  string
		depth int
	}
	var candidates []candidate
	for _, name := range value.Config.TargetNames() {
		root, err := resolveRoot(value.ConfigRoot, value.Config.Apps[name].Root)
		if err != nil {
			continue // a broken root is reported when that target is selected explicitly
		}
		relative, err := filepath.Rel(root, value.CWD)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		candidates = append(candidates, candidate{name: name, depth: strings.Count(root, string(filepath.Separator))})
	}
	if len(candidates) == 0 {
		return "", config.Binding{}, fmt.Errorf("%w: no target's root contains this directory; select one of: %s", config.ErrInvalid, strings.Join(value.Config.TargetNames(), ", "))
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].depth > candidates[j].depth })
	if len(candidates) > 1 && candidates[0].depth == candidates[1].depth {
		var names []string
		for _, c := range candidates {
			if c.depth == candidates[0].depth {
				names = append(names, c.name)
			}
		}
		return "", config.Binding{}, fmt.Errorf("%w: targets %s share the same root; select one explicitly", config.ErrInvalid, strings.Join(names, ", "))
	}
	return candidates[0].name, value.Config.Apps[candidates[0].name], nil
}

func resolveRoot(configRoot, root string) (string, error) {
	if root == "" {
		root = "."
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(configRoot, root))
	if err != nil {
		return "", fmt.Errorf("resolve application root: %w", err)
	}
	relative, err := filepath.Rel(configRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: root resolves outside the configuration directory", config.ErrInvalid)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect application root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: root must be a directory", config.ErrInvalid)
	}
	return resolved, nil
}

// DefaultRoot is the root link proposes for a binding: the configuration
// directory for the single form, and the working directory for a named
// target, so linking from apps/web records apps/web.
func DefaultRoot(value Project, targetName string) string {
	if targetName == "" || targetName == "default" || value.CWD == "" {
		return "."
	}
	relative, err := filepath.Rel(value.ConfigRoot, value.CWD)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "."
	}
	return filepath.ToSlash(relative)
}
