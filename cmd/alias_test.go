package cmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joaomnuno/coolship/cmd"
	"github.com/joaomnuno/coolship/internal/alias"
	"github.com/joaomnuno/coolship/internal/service"
	"github.com/joaomnuno/coolship/internal/ui"
)

func aliasSystem(t *testing.T, pathDirs ...string) (alias.System, string) {
	t.Helper()
	bin := t.TempDir()
	exe := filepath.Join(bin, "coolship")
	if err := os.WriteFile(exe, []byte("COOLSHIP"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := append([]string{}, pathDirs...)
	dirs = append(dirs, bin)
	return alias.System{
		Executable: func() (string, error) { return exe, nil },
		LookPath: func(name string) (string, error) {
			for _, dir := range dirs {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					return filepath.Join(dir, name), nil
				}
			}
			return "", exec.ErrNotFound
		},
		IsCoolship: func(path string) bool {
			data, err := os.ReadFile(path)
			return err == nil && string(data) == "COOLSHIP"
		},
		GOOS: "linux",
	}, bin
}

func executeAlias(t *testing.T, system alias.System, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	root := cmd.NewRootCommand(nil, ui.Streams{Out: &out, Err: &diagnostic}, "test-version", cmd.WithAliasSystem(system))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), diagnostic.String(), err
}

func TestAliasCommandAddsAndRemovesCs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	system, bin := aliasSystem(t)
	out, diagnostic, err := executeAlias(t, system, "alias")
	if err != nil || diagnostic != "" || out != "Added cs: "+filepath.Join(bin, "cs")+" runs coolship\n" {
		t.Fatalf("out=%q err=%q %v", out, diagnostic, err)
	}
	out, _, err = executeAlias(t, system, "alias", "--format", "json")
	var result alias.Result
	if err != nil || json.Unmarshal([]byte(out), &result) != nil || result.Status != alias.StatusExists || result.Name != "cs" {
		t.Fatalf("json=%q err=%v", out, err)
	}
	out, _, err = executeAlias(t, system, "alias", "--remove")
	if err != nil || !strings.HasPrefix(out, "Removed cs: ") {
		t.Fatalf("remove out=%q err=%v", out, err)
	}
}

func TestAliasCommandRefusesAnotherCommandAsInput(t *testing.T) {
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "cs"), []byte("coursier"), 0o755); err != nil {
		t.Fatal(err)
	}
	system, _ := aliasSystem(t, other)
	out, _, err := executeAlias(t, system, "alias")
	if !errors.Is(err, service.ErrInput) || ui.ExitCode(err) != 2 || out != "" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if !strings.Contains(err.Error(), filepath.Join(other, "cs")) {
		t.Fatalf("error does not name the other command: %v", err)
	}
	if _, _, err := executeAlias(t, system, "alias", "a", "b"); ui.ExitCode(err) != 2 {
		t.Fatalf("two names: %v", err)
	}
}
