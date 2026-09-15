// Package alias adds and removes a second name for the Coolship executable,
// such as cs, in the directory the running binary lives in. It touches only
// that one file and never follows PATH to write anywhere else.
package alias

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultName is the alias created when no name is given.
const DefaultName = "cs"

// modulePath identifies a Coolship binary by the main module Go records in it.
const modulePath = "github.com/joaomnuno/coolship"

// System is the process environment alias reads. Every field is optional;
// the zero value uses the running process, PATH, and the host OS.
type System struct {
	// Executable returns the running binary's path (os.Executable).
	Executable func() (string, error)
	// LookPath finds a command on PATH (exec.LookPath).
	LookPath func(string) (string, error)
	// IsCoolship reports whether the file at a path is a Coolship binary.
	// The default reads the Go build information without running the file.
	IsCoolship func(string) bool
	// GOOS decides between a symlink and a copy (runtime.GOOS).
	GOOS string
}

func (s System) normalized() System {
	if s.Executable == nil {
		s.Executable = os.Executable
	}
	if s.LookPath == nil {
		s.LookPath = exec.LookPath
	}
	if s.IsCoolship == nil {
		s.IsCoolship = isCoolshipBinary
	}
	if s.GOOS == "" {
		s.GOOS = runtime.GOOS
	}
	return s
}

// Status says what Create or Remove did.
type Status string

const (
	StatusCreated Status = "created"
	StatusExists  Status = "exists"
	StatusRemoved Status = "removed"
	StatusAbsent  Status = "absent"
)

// Result describes one alias operation.
type Result struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Target string `json:"target"`
	Status Status `json:"status"`
	// Warnings carry notes that did not stop the operation, such as the
	// directory not being on PATH.
	Warnings []string `json:"warnings,omitempty"`
}

// NameError rejects a name that cannot be a command in one directory.
type NameError struct {
	Name   string
	Reason string
}

func (e *NameError) Error() string { return fmt.Sprintf("invalid alias name %q: %s", e.Name, e.Reason) }

// ConflictError reports that the name already runs something other than
// Coolship.
type ConflictError struct {
	Name string
	Path string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s already runs %s, which is not Coolship; nothing was changed. Pick another name: coolship alias NAME", e.Name, e.Path)
}

// NotCoolshipError refuses to remove a file that is not a Coolship alias.
type NotCoolshipError struct {
	Name string
	Path string
}

func (e *NotCoolshipError) Error() string {
	return fmt.Sprintf("%s is not a link to or copy of Coolship; it was left in place", e.Path)
}

// SeparateBinaryError refuses to replace or remove a Coolship binary that is
// not an alias: a regular file rather than a link to one.
type SeparateBinaryError struct {
	Name string
	Path string
}

func (e *SeparateBinaryError) Error() string {
	return fmt.Sprintf("%s is a separate Coolship binary, not an alias, so it was left in place. Pick another name, or delete it yourself if you no longer need it", e.Path)
}

// DirError reports that the binary's directory cannot be written.
type DirError struct {
	Dir        string
	Executable string
	Args       string
	Err        error
}

func (e *DirError) Error() string {
	// The staging file's name means nothing to the reader; keep the cause.
	cause := e.Err
	var pathErr *os.PathError
	var linkErr *os.LinkError
	if errors.As(cause, &pathErr) {
		cause = pathErr.Err
	} else if errors.As(cause, &linkErr) {
		cause = linkErr.Err
	}
	return fmt.Sprintf("cannot write to %s, where coolship is installed (%v). Run it with sudo (sudo %s %s), or move coolship to a directory you own and run the command again",
		e.Dir, cause, e.Executable, e.Args)
}

func (e *DirError) Unwrap() error { return e.Err }

// Create adds name next to the running Coolship binary: a symlink, or a copy
// on Windows. It succeeds without changes when the alias already points to
// this binary, and refuses when name already runs something else.
func Create(system System, name string) (Result, error) {
	system = system.normalized()
	exe, path, err := locate(system, name)
	if err != nil {
		return Result{}, err
	}
	result := Result{Name: name, Path: path, Target: exe, Status: StatusCreated}

	// Something else answering to the name on PATH would shadow the alias or
	// be shadowed by it; either way the user should choose.
	if found, ok := lookPath(system, name); ok {
		if absolute, err := filepath.Abs(found); err == nil {
			found = absolute
		}
		if !samePath(found, path) {
			resolved, err := filepath.EvalSymlinks(found)
			if err != nil {
				resolved = found
			}
			if samePath(resolved, exe) {
				result.Path, result.Status = found, StatusExists
				return result, nil
			}
			if !system.IsCoolship(found) {
				return Result{}, &ConflictError{Name: name, Path: found}
			}
		}
	}

	if info, err := os.Lstat(path); err == nil {
		if current(system, path, exe) {
			result.Status = StatusExists
			return result, nil
		}
		// An alias of an older Coolship binary is refreshed below; a separate
		// Coolship binary and anything else stay.
		if err := keep(system, name, path, info); err != nil {
			return Result{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}

	if err := write(system, exe, path); err != nil {
		return Result{}, &DirError{Dir: filepath.Dir(path), Executable: exe, Args: "alias " + name, Err: err}
	}

	if found, ok := lookPath(system, name); !ok {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s is not on your PATH, so %s will not be found until it is", filepath.Dir(path), name))
	} else if resolved, err := filepath.EvalSymlinks(found); err != nil || !samePath(resolved, exe) && !samePath(found, path) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s runs %s first, because it comes earlier on your PATH", name, found))
	}
	return result, nil
}

// Remove deletes the alias name next to the running Coolship binary, only
// when it is a link to or copy of Coolship. A missing alias is not an error.
func Remove(system System, name string) (Result, error) {
	system = system.normalized()
	exe, path, err := locate(system, name)
	if err != nil {
		return Result{}, err
	}
	result := Result{Name: name, Path: path, Target: exe, Status: StatusRemoved}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		result.Status = StatusAbsent
		return result, nil
	} else if err != nil {
		return Result{}, err
	}
	if !current(system, path, exe) {
		if err := keep(system, name, path, info); err != nil {
			var conflict *ConflictError
			if errors.As(err, &conflict) {
				return Result{}, &NotCoolshipError{Name: name, Path: path}
			}
			return Result{}, err
		}
	}
	if err := os.Remove(path); err != nil {
		return Result{}, &DirError{Dir: filepath.Dir(path), Executable: exe, Args: "alias --remove " + name, Err: err}
	}
	return result, nil
}

// locate validates name and returns the resolved binary and the alias path.
func locate(system System, name string) (exe, path string, err error) {
	if err := validate(name); err != nil {
		return "", "", err
	}
	exe, err = system.Executable()
	if err != nil {
		return "", "", fmt.Errorf("locate the coolship executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return "", "", fmt.Errorf("locate the coolship executable: %w", err)
	}
	file := name
	if system.GOOS == "windows" && !strings.EqualFold(filepath.Ext(name), ".exe") {
		file += ".exe"
	}
	path = filepath.Join(filepath.Dir(exe), file)
	// Compare case-insensitively: on macOS and Windows filesystems COOLSHIP
	// names the running binary itself, and removing it would delete Coolship.
	if strings.EqualFold(filepath.Clean(path), filepath.Clean(exe)) {
		return "", "", &NameError{Name: name, Reason: "that is the name of the coolship binary itself"}
	}
	return exe, path, nil
}

func validate(name string) error {
	switch {
	case name == "":
		return &NameError{Name: name, Reason: "it is empty"}
	case name == "." || name == "..":
		return &NameError{Name: name, Reason: "it names a directory"}
	case strings.HasPrefix(name, "-"), strings.HasPrefix(name, "."):
		return &NameError{Name: name, Reason: "it cannot start with - or ."}
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return &NameError{Name: name, Reason: "use only letters, digits, -, _, and ."}
		}
	}
	return nil
}

// keep returns why the file at path, which is not this binary, must stay, or
// nil when it is an alias of another Coolship binary that may be replaced or
// removed: a symlink to one, or on Windows, where aliases are copies, a copy
// of one. A regular Coolship binary elsewhere is a separate installation,
// such as the installed coolship next to a development build, and stays.
func keep(system System, name, path string, info os.FileInfo) error {
	coolship := system.IsCoolship(path)
	switch {
	case !coolship:
		return &ConflictError{Name: name, Path: path}
	case info.Mode()&os.ModeSymlink != 0, system.GOOS == "windows" && info.Mode().IsRegular():
		return nil
	}
	return &SeparateBinaryError{Name: name, Path: path}
}

// current reports whether path already is this exact binary: a symlink that
// resolves to it, a hard link, or (on Windows) an identical copy.
func current(system System, path, exe string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil && samePath(resolved, exe) {
		return true
	}
	a, errA := os.Stat(path)
	b, errB := os.Stat(exe)
	if errA != nil || errB != nil {
		return false
	}
	if os.SameFile(a, b) {
		return true
	}
	return system.GOOS == "windows" && a.Mode().IsRegular() && a.Size() == b.Size() && sameContent(path, exe)
}

// write puts the alias in place through a temporary name in the same
// directory, so an existing alias is replaced atomically.
func write(system System, exe, path string) error {
	dir := filepath.Dir(path)
	staged, err := os.CreateTemp(dir, ".coolship-alias-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if system.GOOS == "windows" {
		source, err := os.Open(exe)
		if err != nil {
			staged.Close()
			return err
		}
		_, err = io.Copy(staged, source)
		source.Close()
		if closeErr := staged.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err := os.Chmod(stagedPath, 0o755); err != nil {
			return err
		}
	} else {
		if err := staged.Close(); err != nil {
			return err
		}
		if err := os.Remove(stagedPath); err != nil {
			return err
		}
		// A relative target keeps working if the directory is moved.
		if err := os.Symlink(filepath.Base(exe), stagedPath); err != nil {
			return err
		}
	}
	return os.Rename(stagedPath, path)
}

// lookPath finds name on PATH. A match found through a relative PATH entry
// such as "." comes back with exec.ErrDot; it still runs when the user types
// the name, so it counts as found.
func lookPath(system System, name string) (string, bool) {
	found, err := system.LookPath(name)
	if found != "" && (err == nil || errors.Is(err, exec.ErrDot)) {
		return found, true
	}
	return "", false
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func sameContent(a, b string) bool {
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()
	bufA, bufB := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || string(bufA[:na]) != string(bufB[:nb]) {
			return false
		}
		if errA != nil || errB != nil {
			return errors.Is(errA, io.ErrUnexpectedEOF) || errors.Is(errA, io.EOF)
		}
	}
}

// isCoolshipBinary reads the Go build information of the file at path, which
// never runs it, and matches Coolship's module path.
func isCoolshipBinary(path string) bool {
	info, err := buildinfo.ReadFile(path)
	return err == nil && info.Main.Path == modulePath
}
