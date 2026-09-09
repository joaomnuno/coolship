package project

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/joaomnuno/coolship/internal/config"
)

// Plan is what writing a binding would do to the file.
type Plan struct {
	Config    config.Config // the complete configuration to write
	Key       string        // "default" or the target name
	Unchanged bool          // the file already says this; nothing is written
	Review    bool          // an existing binding changes or the form changes
}

// Propose composes the configuration that binds key. Adding a new named target
// keeps the others and needs no review; changing an existing binding, or
// converting between the single and named forms, does.
func Propose(value Project, key string, binding config.Binding) (Plan, error) {
	if binding.Root == "" {
		binding.Root = "."
	}
	if key == "" {
		key = "default"
	}
	plan := Plan{Key: key}
	if key == "default" {
		plan.Config = config.Config{Version: 1, Project: binding}
	} else {
		if err := config.ValidateTargetName(key); err != nil {
			return Plan{}, err
		}
		apps := make(map[string]config.Binding, len(value.Config.Apps)+1)
		if value.Config.Named() {
			maps.Copy(apps, value.Config.Apps)
		}
		apps[key] = binding
		plan.Config = config.Config{Version: 1, Apps: apps}
	}
	if err := config.Validate(plan.Config); err != nil {
		return Plan{}, err
	}
	if !value.Exists {
		return plan, nil
	}
	if config.Equal(value.Config, plan.Config) {
		plan.Unchanged = true
		return plan, nil
	}
	switch {
	case value.Config.Named() != plan.Config.Named():
		plan.Review = true // converting between forms drops the other form's bindings
	case key == "default":
		plan.Review = true
	default:
		existing, present := value.Config.Apps[key]
		plan.Review = present && existing != binding
	}
	return plan, nil
}

// WriteBinding publishes a complete file without clobbering an existing one.
// Reviewed changes require replace and the original file fingerprint.
func WriteBinding(value Project, key string, binding config.Binding, replace bool) error {
	plan, err := Propose(value, key, binding)
	if err != nil {
		return err
	}
	data, err := config.Marshal(plan.Config)
	if err != nil {
		return err
	}
	if value.ConfigPath == "" || value.ConfigRoot != filepath.Dir(value.ConfigPath) {
		return fmt.Errorf("%w: invalid configuration destination", config.ErrInvalid)
	}
	proposed := value
	proposed.Exists = true
	proposed.Config = plan.Config
	if _, err := Select(proposed, plan.Key, ""); err != nil {
		return err
	}
	lock, err := os.OpenFile(value.ConfigPath+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("lock project configuration: %w", err)
	}
	defer os.Remove(lock.Name())
	if err := lock.Close(); err != nil {
		return fmt.Errorf("close configuration lock: %w", err)
	}
	mode, err := checkOriginal(value)
	if err != nil {
		return err
	}
	if plan.Unchanged {
		return nil // Preserve original comments and formatting byte for byte.
	}
	if plan.Review && !replace {
		return ErrReplacementRequired
	}
	temporary, err := os.CreateTemp(value.ConfigRoot, ".coolship-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set configuration permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if _, err := checkOriginal(value); err != nil {
		return err
	}
	if value.Exists {
		if err := os.Rename(temporary.Name(), value.ConfigPath); err != nil {
			return fmt.Errorf("replace project configuration: %w", err)
		}
	} else {
		// A hard link publishes the complete file atomically and fails if the
		// destination appeared since discovery. Rename would silently clobber it.
		if err := os.Link(temporary.Name(), value.ConfigPath); errors.Is(err, os.ErrExist) {
			return ErrConflict
		} else if err != nil {
			return fmt.Errorf("publish project configuration: %w", err)
		}
	}
	return nil
}

func checkOriginal(value Project) (os.FileMode, error) {
	info, err := os.Lstat(value.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		if value.Exists {
			return 0, ErrConflict
		}
		return 0644, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect existing configuration: %w", err)
	}
	if !value.Exists || !info.Mode().IsRegular() {
		return 0, ErrConflict
	}
	data, err := os.ReadFile(value.ConfigPath)
	if err != nil {
		return 0, fmt.Errorf("read existing configuration: %w", err)
	}
	if value.Fingerprint == "" || fingerprint(data) != value.Fingerprint {
		return 0, ErrConflict
	}
	return info.Mode().Perm(), nil
}

// RemoveBinding deletes the configuration file that discovery loaded, and only
// that file: a change since discovery is a conflict, never an overwrite.
func RemoveBinding(value Project) error {
	if !value.Exists || value.ConfigPath == "" {
		return ErrNotLinked
	}
	lock, err := os.OpenFile(value.ConfigPath+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("lock project configuration: %w", err)
	}
	defer os.Remove(lock.Name())
	if err := lock.Close(); err != nil {
		return fmt.Errorf("close configuration lock: %w", err)
	}
	if _, err := checkOriginal(value); err != nil {
		return err
	}
	if err := os.Remove(value.ConfigPath); err != nil {
		return fmt.Errorf("remove project configuration: %w", err)
	}
	return nil
}
