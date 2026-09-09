package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joaomnuno/coolship/internal/config"
)

// Discover resolves paths without changing the process working directory.
func Discover(paths Paths, allowMissing bool) (Project, error) {
	cwd := paths.CWD
	if cwd == "" {
		cwd = "."
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return Project{}, fmt.Errorf("resolve working directory: %w", err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return Project{}, fmt.Errorf("resolve working directory: %w", err)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return Project{}, fmt.Errorf("inspect working directory: %w", err)
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("%w: --cwd must be a directory", config.ErrInvalid)
	}
	gitRoot, err := findGitRoot(cwd)
	if err != nil {
		return Project{}, err
	}
	if paths.ConfigPath != "" {
		path := paths.ConfigPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		return load(path, gitRoot, cwd, allowMissing)
	}
	for directory := cwd; ; directory = filepath.Dir(directory) {
		path := filepath.Join(directory, "coolship.toml")
		if _, err := os.Lstat(path); err == nil {
			return load(path, gitRoot, cwd, false)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Project{}, fmt.Errorf("inspect project configuration: %w", err)
		}
		if directory == gitRoot || filepath.Dir(directory) == directory {
			break
		}
	}
	if !allowMissing {
		return Project{}, ErrNotLinked
	}
	root := gitRoot
	if root == "" {
		root = cwd
	}
	return load(filepath.Join(root, "coolship.toml"), gitRoot, cwd, true)
}

func findGitRoot(start string) (string, error) {
	for directory := start; ; directory = filepath.Dir(directory) {
		if _, err := os.Lstat(filepath.Join(directory, ".git")); err == nil {
			return directory, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect Git boundary: %w", err)
		}
		if filepath.Dir(directory) == directory {
			return "", nil
		}
	}
}

func load(path, gitRoot, cwd string, allowMissing bool) (Project, error) {
	path = filepath.Clean(path)
	actual, err := filepath.EvalSymlinks(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Project{}, fmt.Errorf("resolve project configuration: %w", err)
	}
	if err != nil {
		// Do not follow a broken file symlink when choosing a new destination.
		if _, statErr := os.Lstat(path); statErr == nil {
			return Project{}, fmt.Errorf("%w: configuration path is a broken symlink", config.ErrInvalid)
		}
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(path))
		if parentErr != nil {
			return Project{}, fmt.Errorf("resolve configuration directory: %w", parentErr)
		}
		actual = filepath.Join(parent, filepath.Base(path))
	}
	value := Project{ConfigRoot: filepath.Dir(actual), ConfigPath: actual, GitRoot: gitRoot, CWD: cwd}
	data, err := os.ReadFile(actual)
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		value.Config = config.Config{Version: 1, Project: config.Binding{Root: "."}}
		return value, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return Project{}, fmt.Errorf("%w: %s", ErrNotLinked, actual)
	}
	if err != nil {
		return Project{}, fmt.Errorf("read project configuration: %w", err)
	}
	value.Config, err = config.Parse(data)
	if err != nil {
		return Project{}, fmt.Errorf("load %s: %w", actual, err)
	}
	value.Exists = true
	value.Fingerprint = fingerprint(data)
	return value, nil
}

func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
