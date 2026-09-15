package alias

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fixture is a bin directory holding a fake coolship binary, plus a PATH of
// directories that the injected LookPath searches.
type fixture struct {
	bin    string
	exe    string
	path   []string
	system System
}

func newFixture(t *testing.T, goos string) *fixture {
	t.Helper()
	f := &fixture{bin: t.TempDir()}
	name := "coolship"
	if goos == "windows" {
		name += ".exe"
	}
	f.exe = filepath.Join(f.bin, name)
	writeFile(t, f.exe, "COOLSHIP-BINARY")
	f.path = []string{f.bin}
	f.system = System{
		Executable: func() (string, error) { return f.exe, nil },
		LookPath:   f.lookPath,
		IsCoolship: func(path string) bool {
			data, err := os.ReadFile(path)
			return err == nil && strings.HasPrefix(string(data), "COOLSHIP")
		},
		GOOS: goos,
	}
	return f
}

func (f *fixture) lookPath(name string) (string, error) {
	for _, dir := range f.path {
		candidates := []string{filepath.Join(dir, name)}
		if f.system.GOOS == "windows" {
			candidates = append([]string{filepath.Join(dir, name+".exe")}, candidates...)
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
}

func TestCreateLinksNextToTheBinary(t *testing.T) {
	symlinks(t)
	f := newFixture(t, "linux")
	result, err := Create(f.system, "cs")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(f.bin, "cs")
	if result.Status != StatusCreated || result.Path != want || result.Target != f.exe || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v", result)
	}
	target, err := os.Readlink(want)
	if err != nil || target != "coolship" {
		t.Fatalf("link target = %q, %v", target, err)
	}
	entries, _ := os.ReadDir(f.bin)
	if len(entries) != 2 {
		t.Fatalf("staging file left behind: %v", entries)
	}

	again, err := Create(f.system, "cs")
	if err != nil || again.Status != StatusExists {
		t.Fatalf("second create = %+v, %v", again, err)
	}
}

func TestCreateResolvesASymlinkedExecutable(t *testing.T) {
	symlinks(t)
	f := newFixture(t, "linux")
	shims := t.TempDir()
	shim := filepath.Join(shims, "coolship")
	if err := os.Symlink(f.exe, shim); err != nil {
		t.Fatal(err)
	}
	f.system.Executable = func() (string, error) { return shim, nil }
	f.path = []string{shims, f.bin}
	result, err := Create(f.system, "cs")
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != filepath.Join(f.bin, "cs") {
		t.Fatalf("alias written to %s, want the resolved binary's directory", result.Path)
	}
}

func TestCreateRefusesANameThatRunsSomethingElse(t *testing.T) {
	f := newFixture(t, "linux")
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "cs"), "#!/bin/sh\necho coursier\n")
	f.path = []string{other, f.bin}
	_, err := Create(f.system, "cs")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Path != filepath.Join(other, "cs") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(other, "cs")) {
		t.Fatalf("message does not name the other command: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.bin, "cs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("alias created despite the conflict")
	}
}

func TestCreateRefusesAnUnrelatedFileInTheDirectory(t *testing.T) {
	f := newFixture(t, "linux")
	f.path = nil
	writeFile(t, filepath.Join(f.bin, "cs"), "something else")
	_, err := Create(f.system, "cs")
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(f.bin, "cs")); string(data) != "something else" {
		t.Fatal("unrelated file was overwritten")
	}
}

func TestCreateReportsAnExistingAliasElsewhere(t *testing.T) {
	symlinks(t)
	f := newFixture(t, "linux")
	elsewhere := t.TempDir()
	if err := os.Symlink(f.exe, filepath.Join(elsewhere, "cs")); err != nil {
		t.Fatal(err)
	}
	f.path = []string{elsewhere, f.bin}
	result, err := Create(f.system, "cs")
	if err != nil || result.Status != StatusExists || result.Path != filepath.Join(elsewhere, "cs") {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestCreateRefreshesALinkToAnotherCoolship(t *testing.T) {
	symlinks(t)
	f := newFixture(t, "linux")
	writeFile(t, filepath.Join(f.bin, "coolship-old"), "COOLSHIP-OLD")
	if err := os.Symlink("coolship-old", filepath.Join(f.bin, "cs")); err != nil {
		t.Fatal(err)
	}
	result, err := Create(f.system, "cs")
	if err != nil || result.Status != StatusCreated {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if target, err := os.Readlink(filepath.Join(f.bin, "cs")); err != nil || target != "coolship" {
		t.Fatalf("stale link not replaced: %q %v", target, err)
	}
	if data, _ := os.ReadFile(filepath.Join(f.bin, "coolship-old")); string(data) != "COOLSHIP-OLD" {
		t.Fatal("the old binary the link pointed to was changed")
	}
}

func TestCreateRefreshesAStaleCopyOnWindows(t *testing.T) {
	f := newFixture(t, "windows")
	writeFile(t, filepath.Join(f.bin, "cs.exe"), "COOLSHIP-OLD")
	result, err := Create(f.system, "cs")
	if err != nil || result.Status != StatusCreated {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if data, _ := os.ReadFile(filepath.Join(f.bin, "cs.exe")); string(data) != "COOLSHIP-BINARY" {
		t.Fatalf("stale copy not refreshed: %q", data)
	}
}

// A regular Coolship binary beside the running one, such as the installed
// coolship next to a coolship-dev build, is not an alias: neither Create nor
// Remove touches it.
func TestSeparateCoolshipBinaryIsLeftInPlace(t *testing.T) {
	f := newFixture(t, "linux")
	f.path = nil
	other := filepath.Join(f.bin, "cs")
	writeFile(t, other, "COOLSHIP-INSTALLED")
	for name, operation := range map[string]func(System, string) (Result, error){"create": Create, "remove": Remove} {
		_, err := operation(f.system, "cs")
		var separate *SeparateBinaryError
		if !errors.As(err, &separate) || separate.Path != other {
			t.Fatalf("%s: err = %v", name, err)
		}
		if info, err := os.Lstat(other); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("%s: the separate binary was replaced or removed", name)
		}
		if data, _ := os.ReadFile(other); string(data) != "COOLSHIP-INSTALLED" {
			t.Fatalf("%s: the separate binary changed: %q", name, data)
		}
	}
}

func TestCreateWarnsWhenTheDirectoryIsNotOnPath(t *testing.T) {
	symlinks(t)
	f := newFixture(t, "linux")
	f.path = nil
	result, err := Create(f.system, "cs")
	if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "not on your PATH") {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestCreateCopiesOnWindows(t *testing.T) {
	f := newFixture(t, "windows")
	result, err := Create(f.system, "cs")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.bin, "cs.exe")
	if result.Path != path || result.Status != StatusCreated {
		t.Fatalf("result = %+v", result)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("copy is not a regular file: %v %v", info, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "COOLSHIP-BINARY" {
		t.Fatalf("copy content = %q", data)
	}
	again, err := Create(f.system, "cs")
	if err != nil || again.Status != StatusExists {
		t.Fatalf("identical copy not recognized: %+v %v", again, err)
	}
	removed, err := Remove(f.system, "cs")
	if err != nil || removed.Status != StatusRemoved {
		t.Fatalf("remove = %+v %v", removed, err)
	}
}

func TestCreateRefusesAnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission bits do not stop root or Windows")
	}
	f := newFixture(t, "linux")
	if err := os.Chmod(f.bin, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.bin, 0o755) })
	_, err := Create(f.system, "cs")
	var dir *DirError
	if !errors.As(err, &dir) || dir.Dir != f.bin {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"cannot write to " + f.bin, "sudo " + f.exe + " alias cs", "move coolship"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
}

func TestCreateRejectsBadNames(t *testing.T) {
	f := newFixture(t, "linux")
	for _, name := range []string{"", ".", "..", "-x", ".hidden", "a/b", `a\b`, "a b", "coolship"} {
		_, err := Create(f.system, name)
		var invalid *NameError
		if !errors.As(err, &invalid) {
			t.Errorf("Create(%q) err = %v, want a NameError", name, err)
		}
	}
}

func TestRejectsTheBinaryNameInAnotherCase(t *testing.T) {
	f := newFixture(t, "linux")
	for _, name := range []string{"COOLSHIP", "CoolShip"} {
		var invalid *NameError
		if _, err := Create(f.system, name); !errors.As(err, &invalid) {
			t.Errorf("Create(%q) err = %v, want a NameError", name, err)
		}
		if _, err := Remove(f.system, name); !errors.As(err, &invalid) {
			t.Errorf("Remove(%q) err = %v, want a NameError", name, err)
		}
	}
	w := newFixture(t, "windows")
	var invalid *NameError
	if _, err := Remove(w.system, "COOLSHIP.EXE"); !errors.As(err, &invalid) {
		t.Errorf("Remove(COOLSHIP.EXE) on windows err = %v, want a NameError", err)
	}
	if _, err := os.Stat(f.exe); err != nil {
		t.Fatal("binary removed")
	}
}

func TestCreateRefusesACommandFoundThroughARelativePathEntry(t *testing.T) {
	f := newFixture(t, "linux")
	other := t.TempDir()
	found := filepath.Join(other, "cs")
	writeFile(t, found, "#!/bin/sh\necho coursier\n")
	f.system.LookPath = func(string) (string, error) {
		return found, &exec.Error{Name: "cs", Err: exec.ErrDot}
	}
	_, err := Create(f.system, "cs")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Path != found {
		t.Fatalf("err = %v, want a conflict naming %s", err, found)
	}
}

func TestRemove(t *testing.T) {
	symlinks(t)
	f := newFixture(t, "linux")
	if _, err := Create(f.system, "cs"); err != nil {
		t.Fatal(err)
	}
	result, err := Remove(f.system, "cs")
	if err != nil || result.Status != StatusRemoved {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(f.bin, "cs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("alias still present")
	}
	if _, err := os.Stat(f.exe); err != nil {
		t.Fatal("binary removed with the alias")
	}
	result, err = Remove(f.system, "cs")
	if err != nil || result.Status != StatusAbsent {
		t.Fatalf("second remove = %+v, %v", result, err)
	}
}

func TestRemoveKeepsAFileThatIsNotCoolship(t *testing.T) {
	f := newFixture(t, "linux")
	writeFile(t, filepath.Join(f.bin, "cs"), "something else")
	_, err := Remove(f.system, "cs")
	var refused *NotCoolshipError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.bin, "cs")); err != nil {
		t.Fatal("unrelated file was removed")
	}
}

func TestDefaultIdentificationDoesNotTrustScripts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cs")
	writeFile(t, path, "#!/bin/sh\necho coolship\n")
	if isCoolshipBinary(path) {
		t.Fatal("a script was identified as Coolship")
	}
	if isCoolshipBinary(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("a missing file was identified as Coolship")
	}
}
