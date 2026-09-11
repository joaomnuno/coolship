package ui

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// charmPrefixes are the module paths of the Charm stack, in both the GitHub
// form the v1 modules use and the vanity form of the v2 modules.
var charmPrefixes = []string{"github.com/charmbracelet/", "charm.land/"}

// TestOnlyUIImportsTheCharmStack enforces ADR 0001: internal/ui is the only
// package that imports Bubble Tea, Huh, Lip Gloss, or their companions. Every
// Go file in the module is parsed for its imports, tests included.
func TestOnlyUIImportsTheCharmStack(t *testing.T) {
	root := moduleRoot(t)
	uiDir := filepath.Join(root, "internal", "ui")
	inUI := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported := strings.Trim(spec.Path.Value, `"`)
			if !importsCharm(imported) {
				continue
			}
			if filepath.Dir(path) == uiDir {
				inUI++
				continue
			}
			relative, _ := filepath.Rel(root, path)
			t.Errorf("%s imports %s; only internal/ui may import the Charm stack (ADR 0001)", relative, imported)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if inUI == 0 {
		t.Fatal("no file under internal/ui imports the Charm stack; the walk found nothing")
	}
}

func importsCharm(path string) bool {
	for _, prefix := range charmPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// moduleRoot finds the directory holding go.mod above the test's working
// directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
