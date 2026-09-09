package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joaomnuno/coolship/internal/config"
)

// WriteBinding creates a complete file without clobbering an existing one.
// Replacements require an explicit review and the original file fingerprint.
func WriteBinding(value Project, binding config.Binding, replace bool) error {
	if binding.Root == "" {
		binding.Root = "."
	}
	data, err := config.Marshal(config.Config{Version: 1, Project: binding})
	if err != nil {
		return err
	}
	if value.ConfigPath == "" || value.ConfigRoot != filepath.Dir(value.ConfigPath) {
		return fmt.Errorf("%w: invalid configuration destination", config.ErrInvalid)
	}
	proposed := value
	proposed.Exists = true
	proposed.Config = config.Config{Version: 1, Project: binding}
	if _, err := Select(proposed, ""); err != nil {
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
	oldBinding := value.Config.Project
	if oldBinding.Root == "" {
		oldBinding.Root = "."
	}
	if value.Exists && oldBinding == binding {
		return nil // Preserve original comments and formatting byte for byte.
	}
	if value.Exists && !replace {
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
